/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package application

import (
	"context"
	"fmt"
	"sort"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxstore "centaurusinfra.io/fornax-serverless/pkg/store"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
)

const (
	DefaultSessionPendingTimeoutDuration = 5 * time.Second
	DefaultSessionOpenTimeoutDuration    = 10 * time.Second
	HouseKeepingDuration                 = 1 * time.Minute
)

type ApplicationSessionState uint8

const (
	SessionStatePending  ApplicationSessionState = 0 // session is pending assing to pod
	SessionStateStarting ApplicationSessionState = 1 // session is assgined, waiting for pod start it and reply
	SessionStateRunning  ApplicationSessionState = 2 // session is assigned and running
	SessionStateDeleting ApplicationSessionState = 3 // session is required to delete, if it's open need to closed firstly
)

type ApplicationSession struct {
	session *fornaxv1.ApplicationSession
	state   ApplicationSessionState
}

func (am *ApplicationManager) initApplicationSessionInformer(ctx context.Context) error {
	channel, err := am.sessionManager.Watch(ctx)
	if err != nil {
		return err
	}
	am.sessionUpdateChannel = channel
	return nil
}

func (am *ApplicationManager) onSessionEventFromStorage(we fornaxstore.WatchEventWithOldObj) {
	switch we.Type {
	case watch.Added:
		am.onApplicationSessionAddEvent(we.Object)
	case watch.Modified:
		am.onApplicationSessionUpdateEvent(we.OldObject, we.Object)
	case watch.Deleted:
		am.onApplicationSessionDeleteEvent(we.Object)
	}
}

func (am *ApplicationManager) changeSessionStatus(session *fornaxv1.ApplicationSession, status fornaxv1.SessionStatus) error {
	newStatus := session.Status.DeepCopy()
	newStatus.SessionStatus = status
	if status == fornaxv1.SessionStatusClosed || status == fornaxv1.SessionStatusTimeout {
		newStatus.ClientSessions = []v1.LocalObjectReference{}
	}
	// set local copy status then update store
	session.Status = *newStatus
	return am.sessionManager.UpdateSessionStatus(session, newStatus)
}

// callback from Application informer when ApplicationSession is created
// if there is a cached copy in application pool, do session update
// if session is not in pool and not terminal state, add new session into pool, and sync application
func (am *ApplicationManager) onApplicationSessionAddEvent(obj interface{}) {
	session := obj.(*fornaxv1.ApplicationSession)
	if session.DeletionTimestamp != nil {
		am.onApplicationSessionDeleteEvent(obj)
		return
	}
	applicationKey := getSessionApplicationKey(session)
	pool := am.getOrCreateApplicationPool(applicationKey)

	klog.InfoS("Application session created", "session", util.Name(session))
	if v := pool.getSession(string(session.GetUID())); v != nil {
		am.onApplicationSessionUpdateEvent(v.session, session)
		return
	} else {
		if !util.SessionInTerminalState(session) {
			updateSessionPool(pool, session)
		}
	}
	am.enqueueApplication(applicationKey)
}

// callback from Application informer when ApplicationSession is updated
// if session already in application pool, update it and trigger application sync
// else add new copy into pool and do not need to add new session if it's terminated if it's not in app pool, just forget it
func (am *ApplicationManager) onApplicationSessionUpdateEvent(old, cur interface{}) {
	oldCopy := old.(*fornaxv1.ApplicationSession)
	newCopy := cur.(*fornaxv1.ApplicationSession)
	klog.InfoS("Application session updated", "session", util.Name(newCopy), "old status", oldCopy.Status.SessionStatus, "new status", newCopy.Status.SessionStatus, "deleting", newCopy.DeletionTimestamp != nil)

	applicationKey := getSessionApplicationKey(newCopy)
	pool := am.getOrCreateApplicationPool(applicationKey)

	if v := pool.getSession(string(newCopy.GetUID())); v != nil {
		updateSessionPool(pool, newCopy)
		am.enqueueApplication(applicationKey)
	} else {
		if !util.SessionInTerminalState(newCopy) {
			updateSessionPool(pool, newCopy)
		}
		am.enqueueApplication(applicationKey)
	}
}

// callback from Application informer when ApplicationSession is physically deleted
// if it's in pool, update session status and resync application
// if a delete session is not application pool, no need to add, it does not impact application at all
func (am *ApplicationManager) onApplicationSessionDeleteEvent(obj interface{}) {
	session, ok := obj.(*fornaxv1.ApplicationSession)
	if !ok {
		klog.Errorf("Received a unknown runtime object", obj)
	}

	klog.InfoS("Application session deleted", "session", util.Name(session), "status", session.Status)
	applicationKey := getSessionApplicationKey(session)
	pool := am.getApplicationPool(applicationKey)
	if pool == nil {
		return
	}
	oldCopy := pool.getSession(string(session.GetUID()))
	if oldCopy != nil {
		if oldCopy.session.DeletionTimestamp == nil {
			oldCopy.session.DeletionTimestamp = util.NewCurrentMetaTime()
		}
		updateSessionPool(pool, oldCopy.session)
	}
	am.enqueueApplication(applicationKey)
}

type PendingSessions []*ApplicationSession

func (ps PendingSessions) Len() int {
	return len(ps)
}

//so, sort latency from smaller to lager value
func (ps PendingSessions) Less(i, j int) bool {
	return ps[i].session.CreationTimestamp.Before(&ps[j].session.CreationTimestamp)
}

func (ps PendingSessions) Swap(i, j int) {
	ps[i], ps[j] = ps[j], ps[i]
}

// deployApplicationSessions grab a list of pending session and try to allocate them to pods and call OpenSession on choosen pod.
// session status change in memory to SessionStatusStarting, but do not update etcd to avoid unnecessary resync.
// session status will be changed in etcd until pod report back, if fornax core restart and lost these memory state, it rely on pod to report back.
// It also cleanup session when a session is in Starting or Pending state for more than a timeout duration.
// session is changed to SessionStatusTimeout, session client need to create a new session.
// It also cleanup session in deletingSessions when a session is in Starting or Pending state for more than a timeout duration.
// session is changed to SessionStatusClosed, session client need to create a new session.
// session timedout and closed are removed from application pool's session list, so, syncApplicationPods do not need to consider these sessions anymore
func (am *ApplicationManager) deployApplicationSessions(pool *ApplicationPool, application *fornaxv1.Application) error {
	pendingSessions, deletingSessions, timeoutSessions := pool.getNonRunningSessions()
	// get 5 more in case some pods assigment failed
	idlePods := pool.getSomeIdlePods(len(pendingSessions))
	klog.InfoS("Syncing application pending session", "application", pool.appName, "#pending", len(pendingSessions), "#deleting", len(deletingSessions), "#timeout", len(timeoutSessions))

	sort.Sort(PendingSessions(pendingSessions))
	sessionErrors := []error{}
	// 1/ assign pending sessions to idle pod
	si := 0
	for _, ap := range idlePods {
		if si == len(pendingSessions) {
			// has assigned all pending sesion to pod
			break
		}
		pod := am.podManager.FindPod(ap.podName)
		if pod != nil {
			// update as status and set access point of as
			as := pendingSessions[si]
			klog.InfoS("Assign session to pod", "application", pool.appName, "pod", util.Name(pod), "session", util.Name(as.session))
			err := am.bindSessionToPod(pool, pod, as.session)
			if err != nil {
				// move to next pod, it could fail to accept other session also
				klog.ErrorS(err, "Failed to open session on pod", "app", pool.appName, "session", as.session.Name, "pod", util.Name(pod))
				sessionErrors = append(sessionErrors, err)
				continue
			} else {
				pool.addOrUpdatePod(ap.podName, PodStateAllocated, []string{string(as.session.GetUID())})
				si += 1
			}
		} else {
			klog.InfoS("A idle Pod does not exist in Pod manager at all, should be deleted", "application", pool.appName, "pod", util.Name(ap.podName))
		}
	}

	// 2, cleanup timeout session,
	for _, v := range timeoutSessions {
		if err := am.deleteApplicationSession(pool, v); err != nil {
			klog.ErrorS(err, "Failed to cleanup timeout session")
			sessionErrors = append(sessionErrors, err)
		}
	}

	// 3, cleanup deleting session,
	for _, v := range deletingSessions {
		err := am.deleteApplicationSession(pool, v)
		if err != nil {
			klog.ErrorS(err, "Failed to delete deleting session")
			sessionErrors = append(sessionErrors, err)
		}
	}

	if len(sessionErrors) > 0 {
		return fmt.Errorf("Some sessions failed to be sync, errors=%v", sessionErrors)
	}

	return nil
}

// if session is open, close it and wait for node report back
// if session is still in pending, change status to timeout
// if session is not open or pending, just delete since it's already in a terminal state
func (am *ApplicationManager) deleteApplicationSession(pool *ApplicationPool, s *ApplicationSession) error {
	if s.session.DeletionTimestamp != nil {
		s.session.DeletionTimestamp = util.NewCurrentMetaTime()
	}

	if util.SessionIsClosing(s.session) {
		// klog.InfoS("Cleanup a session is in closing state", "session", util.Name(s.session))
		return nil
	} else if util.SessionIsOpen(s.session) {
		if s.session.Status.PodReference != nil {
			podName := s.session.Status.PodReference.Name
			pod := am.podManager.FindPod(podName)
			if pod != nil {
				// ideally this state should report back from node, set it here to avoid calling node to close session multiple times
				// if node report back different status, then app will call close session again
				am.changeSessionStatus(s.session, fornaxv1.SessionStatusClosing)
				err := am.sessionManager.CloseSession(pod, s.session)
				if err != nil {
					return err
				}
			} else {
				am.changeSessionStatus(s.session, fornaxv1.SessionStatusClosed)
			}
		}
	} else {
		if util.SessionIsPending(s.session) {
			if err := am.changeSessionStatus(s.session, fornaxv1.SessionStatusTimeout); err != nil {
				return err
			}
		}

		klog.InfoS("Cleanup a session is in pending or terminated state", "session", util.Name(s.session))
		pool.deleteSession(s.session)
	}
	return nil
}

// change sessions status to starting and set access point
func (am *ApplicationManager) bindSessionToPod(pool *ApplicationPool, pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	newStatus := session.Status.DeepCopy()
	newStatus.SessionStatus = fornaxv1.SessionStatusStarting
	for _, cont := range pod.Spec.Containers {
		for _, port := range cont.Ports {
			newStatus.AccessEndPoints = append(session.Status.AccessEndPoints, fornaxv1.AccessEndPoint{
				Protocol:  port.Protocol,
				IPAddress: port.HostIP,
				Port:      port.HostPort,
			})
		}
	}
	newStatus.PodReference = &v1.LocalObjectReference{
		Name: util.Name(pod),
	}
	oldStatus := session.Status.DeepCopy()
	session.Status = *newStatus
	if err := am.sessionManager.OpenSession(pod, session); err != nil {
		session.Status = *oldStatus
		return err
	} else {
		// just change pool directly, no need to update storage for a transient state, and triger unnecessary sync
		updateSessionPool(pool, session)
		return nil
	}
}

// cleanupSessionOnDeletedPod handle pod is terminated unexpectedly, e.g. node crash
// in normal cases,session should be closed before pod is terminated and deleted.
// It update open session to closed and pending session to timedout,
// and does not try to call node to close session, as session does not exist at all on node when pod deleted
func (am *ApplicationManager) cleanupSessionOnDeletedPod(pool *ApplicationPool, podName string) {
	podSessions := pool.getPodSessions(podName)
	for _, sess := range podSessions {
		klog.Infof("Delete session %s on deleted pod %s", util.Name(sess.session), podName)
		am.deleteApplicationSession(pool, sess)
	}
}

// cleanupSessionOfApplication if a application is being deleted,
// close all sessions which are still alive and delete sessions from application sessions pool if they are still pending
// when alive session reported as closed by Node Agent, then session can be eventually deleted
func (am *ApplicationManager) cleanupSessionOfApplication(pool *ApplicationPool) error {
	deleteErrors := []error{}

	sessions := pool.sessionList()
	for _, v := range sessions {
		err := am.deleteApplicationSession(pool, v)
		if err != nil {
			deleteErrors = append(deleteErrors, err)
		}
	}

	if len(deleteErrors) != 0 {
		return fmt.Errorf("Some sessions failed to be deleted, errors=%v", deleteErrors)
	}

	return nil
}
