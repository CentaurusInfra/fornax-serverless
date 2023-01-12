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
	"reflect"
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
	DefaultSessionCloseTimeoutDuration   = 60 * time.Second
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
	if v := pool.getSession(util.Name(session)); v != nil {
		am.onApplicationSessionUpdateEvent(v.session, v)
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

	applicationKey := getSessionApplicationKey(newCopy)
	pool := am.getOrCreateApplicationPool(applicationKey)
	if v := pool.getSession(util.Name(newCopy)); v != nil {
		if newCopy.DeletionTimestamp == nil && reflect.DeepEqual(newCopy.Status, v.session.Status) && reflect.DeepEqual(newCopy.Spec, v.session.Spec) {
			// no meaningful change, skip
			return
		}
		updateSessionPool(pool, newCopy)
	} else {
		if !util.SessionInTerminalState(newCopy) {
			updateSessionPool(pool, newCopy)
		}
	}
	klog.InfoS("Application session updated", "session", util.Name(newCopy), "old status", oldCopy.Status.SessionStatus, "new status", newCopy.Status.SessionStatus, "deleting", newCopy.DeletionTimestamp != nil, "pod", newCopy.Annotations[fornaxv1.AnnotationFornaxCorePod])
	am.enqueueApplication(applicationKey)
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
	if oldCopy := pool.getSession(util.Name(session)); oldCopy != nil {
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

// deployApplicationSessions group session into pending, timeout, deleting states, and
// 1, assign pending session to idle pods and call OpenSession on choosen pod.
// session status change in memory to SessionStatusStarting, session is store in node and report back,
// if fornax core restart and lost these memory state, it rely on pod to report back.
// 2, It cleanup timeout session which stuck in pending or starting session for more than a timeout duration.
// and call node to close session if session is in starting state which was sent to a pod before.
// session is changed to SessionStatusTimeout, session client need to create a new session.
// 3, if a session is being deleted by client(aka, close session), it call node to close session session,
// timedout and closed session are removed from application's session pool
func (am *ApplicationManager) deployApplicationSessions(pool *ApplicationPool, application *fornaxv1.Application) error {
	pendingSessions, deletingSessions, timeoutSessions := pool.getNonRunningSessions()

	// 1/ assign pending sessions to idle pod
	// get 5 more in case some pods assigment failed
	idlePods := pool.getSomeIdlePods(len(pendingSessions) + 5)
	klog.V(5).InfoS("Syncing application pending session", "application", pool.appName, "#pending", len(pendingSessions), "#deleting", len(deletingSessions), "#timeout", len(timeoutSessions))

	// sort by creation timestamp to make sure FIFO
	sort.Sort(PendingSessions(pendingSessions))
	sessionErrors := []error{}
	si := 0
	for _, ap := range idlePods {
		if si == len(pendingSessions) {
			// has assigned all pending sesion to pod
			break
		}
		pod := am.podManager.FindPod(ap.podName)
		if pod != nil {
			as := pendingSessions[si]
			klog.V(5).InfoS("Assign session to pod", "application", pool.appName, "pod", util.Name(pod), "session", util.Name(as.session))
			err := am.assignSessionToPod(pool, pod, as.session)
			if err != nil {
				// move to next pod, it could fail to accept other session also
				klog.ErrorS(err, "Failed to open session on pod", "app", pool.appName, "session", as.session.Name, "pod", util.Name(pod))
				sessionErrors = append(sessionErrors, err)
				continue
			} else {
				si += 1
			}
		} else {
			// TODO
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
// if session is not assigned or pending, just delete since it's already in a terminal state
func (am *ApplicationManager) deleteApplicationSession(pool *ApplicationPool, s *ApplicationSession) error {
	if s.session.DeletionTimestamp == nil {
		s.session.DeletionTimestamp = util.NewCurrentMetaTime()
	}

	if util.SessionIsClosing(s.session) {
		// has requested to deleted, check if close timeout
		if s.session.DeletionTimestamp.Time.After(time.Now().Add(-1 * DefaultSessionCloseTimeoutDuration)) {
			return nil
		}
	}

	klog.V(5).InfoS("Cleanup a session", "session", util.Name(s.session), "state", s.session.Status.SessionStatus)
	if s.state == SessionStatePending {
		if err := am.changeSessionStatus(s.session, fornaxv1.SessionStatusTimeout); err != nil {
			return err
		}
		pool.deleteSession(s.session)
		return nil
	}

	if podName, found := s.session.Annotations[fornaxv1.AnnotationFornaxCorePod]; found {
		pod := am.podManager.FindPod(podName)
		if pod != nil {
			if !util.SessionIsClosing(s.session) {
				am.changeSessionStatus(s.session, fornaxv1.SessionStatusClosing)
			}
			err := am.sessionManager.CloseSession(pod, s.session)
			if err != nil {
				return err
			}
		} else {
			// should not happen for session not in pending state
			am.changeSessionStatus(s.session, fornaxv1.SessionStatusClosed)
		}
	} else {
		if err := am.changeSessionStatus(s.session, fornaxv1.SessionStatusClosed); err != nil {
			return err
		}
		pool.deleteSession(s.session)
		return nil
	}

	return nil
}

// call session manager to open session, change sessions status to starting and set access point, update pod to allocated state
func (am *ApplicationManager) assignSessionToPod(pool *ApplicationPool, pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	newSession := session.DeepCopy()
	newSession.Status.SessionStatus = fornaxv1.SessionStatusStarting
	for _, cont := range pod.Spec.Containers {
		for _, port := range cont.Ports {
			newSession.Status.AccessEndPoints = append(session.Status.AccessEndPoints, fornaxv1.AccessEndPoint{
				Protocol:  port.Protocol,
				IPAddress: port.HostIP,
				Port:      port.HostPort,
			})
		}
	}

	if newSession.Annotations != nil {
		newSession.Annotations[fornaxv1.AnnotationFornaxCorePod] = util.Name(pod)
	} else {
		newSession.Annotations = map[string]string{fornaxv1.AnnotationFornaxCorePod: util.Name(pod)}
	}
	if err := am.sessionManager.OpenSession(pod, newSession); err != nil {
		return err
	} else {
		updateSessionPool(pool, newSession)
		return nil
	}
}

// cleanupSessionOnDeletedPod handle pod is terminated unexpectedly, e.g. node crash
// in normal cases,session should be closed before pod is terminated and deleted.
// It update open session to closed and pending session to timedout,
// and does not try to call node to close session, as session does not exist at all on node when pod terminated on node
func (am *ApplicationManager) cleanupSessionOnDeletedPod(pool *ApplicationPool, podName string) {
	podSessions := pool.getPodSessions(podName)
	for _, sess := range podSessions {
		klog.Infof("Delete session %s on deleted pod %s", util.Name(sess.session), podName)
		am.deleteApplicationSession(pool, sess)
	}
}

// cleanupSessionOfApplication if a application is being deleted, it
// call node to close all sessions which are still open, when session reported as closed from node, then session can be eventually deleted
// if session is still pending assigned to pod, delete sessions from application sessions pool directly
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
