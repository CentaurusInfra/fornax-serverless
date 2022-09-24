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
	"fmt"
	"reflect"
	"sort"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	DefaultSessionPendingTimeoutDuration = 5 * time.Second
	DefaultSessionOpenTimeoutDuration    = 10 * time.Second
	HouseKeepingDuration                 = 1 * time.Minute
)

// treat node as authority for session status, session status from node could be Starting, Available, Closed,
// use status from node always, then session will be closed if session is already deleted
func (am *ApplicationManager) onSessionEventFromNode(se *ie.SessionEvent) error {
	pod := se.Pod
	session := se.Session

	applicationKey := am.getSessionApplicationKey(session)
	pool := am.getOrCreateApplicationPool(applicationKey)

	// termiante pod after sesion is closed, do this here before update session status to avoid race condition that
	// session removed from pod and pod is set idle and be picked up by other session
	if session.Spec.KillInstanceWhenSessionClosed && session.Status.SessionStatus == fornaxv1.SessionStatusClosed && util.PodNotTerminated(pod) {
		klog.InfoS("Terminate a pod as KillInstanceWhenSessionClosed is true", "pod", util.Name(pod), "session", util.Name(session))
		am.deleteApplicationPod(pool, pod, false)
	}

	oldCopy := pool.getSession(string(session.GetUID()))
	if oldCopy == nil {
		if util.SessionIsClosed(session) {
			am.onApplicationSessionDeleteEvent(session)
		} else {
			am.onApplicationSessionAddEvent(session)
		}
	} else {
		if util.SessionIsOpen(session) && oldCopy.DeletionTimestamp != nil {
			// session was requested to delete by application, save old copy's deletiontimestamp and close session
			session.DeletionTimestamp = oldCopy.DeletionTimestamp
			am.sessionManager.CloseSession(pod, session)
		}
		// set available and close time when received in fornax core, for perf benchmark
		if session.Status.SessionStatus == fornaxv1.SessionStatusAvailable {
			session.Status.AvailableTime = util.NewCurrentMetaTimeNormallized()
			session.Status.AvailableTimeMicro = time.Now().UnixMicro()
		}

		if session.Status.SessionStatus == fornaxv1.SessionStatusClosed {
			session.Status.CloseTime = util.NewCurrentMetaTimeNormallized()
		}

		am.onApplicationSessionUpdateEvent(oldCopy, session)
		// update session in go routine, this is persist status change to return by api server
		am.sessionManager.UpdateSessionStatus(oldCopy.DeepCopy(), session.Status.DeepCopy())
	}

	return nil
}

func (am *ApplicationManager) changeSessionStatus(v *fornaxv1.ApplicationSession, status fornaxv1.SessionStatus) error {
	session := v.DeepCopy()
	newStatus := v.Status.DeepCopy()
	newStatus.SessionStatus = status
	if status == fornaxv1.SessionStatusClosed || status == fornaxv1.SessionStatusTimeout {
		// reset client session to let it can be hard deleted
		newStatus.ClientSessions = []v1.LocalObjectReference{}
	}
	return am.sessionManager.UpdateSessionStatus(session, newStatus)
}

func (am *ApplicationManager) getSessionApplicationKey(session *fornaxv1.ApplicationSession) string {
	applicationName := session.Spec.ApplicationName
	namespace := session.Namespace
	return fmt.Sprintf("%s/%s", namespace, applicationName)
}

// add active session into application's session pool and delete terminal session from pool
// add session/delete session will update pod state according pod's session usage
func (am *ApplicationManager) updateSessionPool(pool *ApplicationPool, session *fornaxv1.ApplicationSession) {
	sessionId := string(session.GetUID())
	if util.SessionInTerminalState(session) {
		pool.deleteSession(session)
	} else {
		// a trick to make sure pending session are sorted using micro second, api server truncate creation timestamp to second
		session.CreationTimestamp = *util.NewCurrentMetaTime()
		pool.addSession(sessionId, session)
	}
}

// callback from Application informer when ApplicationSession is created
// if session in terminal state, remove this session from pool(should not happen for a new session, but for weird case)
// else add new copy into pool
func (am *ApplicationManager) onApplicationSessionAddEvent(obj interface{}) {
	session := obj.(*fornaxv1.ApplicationSession)
	if session.DeletionTimestamp != nil {
		am.onApplicationSessionDeleteEvent(obj)
		return
	}
	applicationKey := am.getSessionApplicationKey(session)
	pool := am.getOrCreateApplicationPool(applicationKey)

	if !util.SessionInTerminalState(session) {
		klog.InfoS("Application session created", "session", util.Name(session))
		am.updateSessionPool(pool, session)
		am.enqueueApplication(applicationKey)
	}
}

// callback from Application informer when ApplicationSession is updated
// or session status is reported back from node
// if session in terminal state, remove this session from pool,
// else add new copy into pool
// do not need to sync application unless session is deleting or status changed
func (am *ApplicationManager) onApplicationSessionUpdateEvent(old, cur interface{}) {
	oldCopy := old.(*fornaxv1.ApplicationSession)
	newCopy := cur.(*fornaxv1.ApplicationSession)

	applicationKey := am.getSessionApplicationKey(newCopy)
	pool := am.getOrCreateApplicationPool(applicationKey)
	if v := pool.getSession(string(newCopy.GetUID())); v != nil {
		// use cached memory copy if there is,
		// fornaxcore update memory cache firstly then asynchronously update etcd when received session status on node.
		// if cached copy is already closed, do not use new status, data could be old since etcd is asynchronously updated.
		oldCopy = v.DeepCopy()
		if util.SessionInTerminalState(oldCopy) {
			am.updateSessionPool(pool, oldCopy)
		} else if reflect.DeepEqual(oldCopy.Status, newCopy.Status) {
			// ignore no status change
		} else if util.SessionIsOpen(oldCopy) && (newCopy.Status.SessionStatus == fornaxv1.SessionStatusUnspecified || newCopy.Status.SessionStatus == fornaxv1.SessionStatusPending) {
			// ignore weird etcd late update, a open session can not change back
		} else {
			am.updateSessionPool(pool, newCopy)
		}
	} else {
		// do not need to add a new session if it's terminated
		if util.SessionInTerminalState(newCopy) {
			return
		}
		am.updateSessionPool(pool, newCopy)
	}

	if (newCopy.DeletionTimestamp != nil && oldCopy.DeletionTimestamp == nil) || !reflect.DeepEqual(oldCopy.Status, newCopy.Status) {
		klog.InfoS("Application session updated", "session", util.Name(newCopy), "old status", oldCopy.Status.SessionStatus, "new status", newCopy.Status.SessionStatus, "deleting", newCopy.DeletionTimestamp != nil)
		am.enqueueApplication(applicationKey)
	}
}

// callback from Application informer when ApplicationSession is physically deleted
func (am *ApplicationManager) onApplicationSessionDeleteEvent(obj interface{}) {
	session, ok := obj.(*fornaxv1.ApplicationSession)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("couldn't get object from tombstone %#v", obj)
			return
		}
		session, ok = tombstone.Obj.(*fornaxv1.ApplicationSession)
		if !ok {
			klog.Errorf("tombstone contained object that is not a Application session %#v", obj)
			return
		}
	}
	if session.DeletionTimestamp == nil {
		session.DeletionTimestamp = util.NewCurrentMetaTime()
	}

	// as fornaxcore update cache memory before update etcd, if there is no a cached copy,
	// do not need to sync a deleted session, as fornaxcore update cache memory before update etcd
	applicationKey := am.getSessionApplicationKey(session)
	pool := am.getApplicationPool(applicationKey)
	if pool == nil {
		return
	}

	oldCopy := pool.getSession(string(session.GetUID()))
	if oldCopy != nil {
		if util.SessionInTerminalState(oldCopy) {
			am.updateSessionPool(pool, oldCopy)
		} else {
			am.updateSessionPool(pool, session)
		}

		// delete a closed or timeout session does not impact application at all, no need to sync
		klog.InfoS("Application session deleted", "session", util.Name(session), "status", session.Status, "finalizer", session.Finalizers)
		am.enqueueApplication(applicationKey)
	}
}

type PendingSessions []*fornaxv1.ApplicationSession

func (ps PendingSessions) Len() int {
	return len(ps)
}

//so, sort latency from smaller to lager value
func (ps PendingSessions) Less(i, j int) bool {
	return ps[i].CreationTimestamp.Before(&ps[j].CreationTimestamp)
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
	pendingSessions, deletingSessions, _, timeoutSessions, runningSessions := pool.groupSessionsByState()
	// get 5 more in case some pods assigment failed
	idlePods := pool.getSomeIdlePods(len(pendingSessions))
	klog.InfoS("Syncing application pending session", "application", pool.appName, "#running", len(runningSessions), "#pending", len(pendingSessions), "#deleting", len(deletingSessions), "#timeout", len(timeoutSessions))

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
			// update session status and set access point of session
			session := pendingSessions[si]
			klog.InfoS("Assign session to pod", "application", pool.appName, "pod", util.Name(pod), "session", util.Name(session))
			err := am.bindSessionToPod(pod, session)
			if err != nil {
				// move to next pod, it could fail to accept other session also
				klog.ErrorS(err, "Failed to open session on pod", "app", pool.appName, "session", session.Name, "pod", util.Name(pod))
				sessionErrors = append(sessionErrors, err)
				continue
			} else {
				pool.addOrUpdatePod(ap.podName, PodStateAllocated, []string{string(session.GetUID())})
				si += 1
			}
		}
	}

	// 2, cleanup timeout session, set session status to timeout and delete it from list
	for _, v := range timeoutSessions {
		if err := am.changeSessionStatus(v, fornaxv1.SessionStatusTimeout); err != nil {
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
func (am *ApplicationManager) deleteApplicationSession(pool *ApplicationPool, session *fornaxv1.ApplicationSession) error {
	if util.SessionIsClosing(session) {
		// already request to close, wait for report back
		// TODO, if pod never report, session should be closed
		return nil
	} else if util.SessionIsOpen(session) {
		if session.DeletionTimestamp == nil {
			session.DeletionTimestamp = util.NewCurrentMetaTime()
		}
		err := am.closeApplicationSession(session)
		if err != nil {
			return err
		} else {
			// do not persist closing state, it's transit and kept in memory
			session.Status.SessionStatus = fornaxv1.SessionStatusClosing
		}
	} else {
		// in terminal or pending state, just delete it from pool
		if util.SessionIsPending(session) {
			if err := am.changeSessionStatus(session, fornaxv1.SessionStatusTimeout); err != nil {
				return err
			}
		}

		pool.deleteSession(session)
	}
	return nil
}

// change sessions status to starting and set access point
func (am *ApplicationManager) bindSessionToPod(pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
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
		// set status back in memory to try rebind
		session.Status = *oldStatus
		return err
	} else {
		return nil
	}
}

func (am *ApplicationManager) closeApplicationSession(session *fornaxv1.ApplicationSession) error {
	klog.Infof("Close applciation sessions %s", util.Name(session))
	if util.SessionIsOpen(session) {
		if session.Status.PodReference != nil {
			podName := session.Status.PodReference.Name
			pod := am.podManager.FindPod(podName)
			if pod != nil {
				// ideally this state should report back from node, set it here to avoid calling node to close session multiple times
				// if node report back different status, then app will call close session again
				err := am.sessionManager.CloseSession(pod, session)
				if err != nil {
					return err
				}
			} else {
				// how to handle it, this case could happen when FornaxCore restart
				// it have not get all pods reported by node, and client want to close a session
				am.changeSessionStatus(session, fornaxv1.SessionStatusClosed)
				return nil
			}
		}
	} else {
		// no op
	}

	return nil
}

// cleanupSessionOnDeletedPod handle pod is terminated unexpectedly, e.g. node crash
// in normal cases,session should be closed before pod is terminated and deleted.
// It update open session to closed and pending session to timedout,
// and does not try to call node to close session, as session does not exist at all on node when pod deleted
func (am *ApplicationManager) cleanupSessionOnDeletedPod(pool *ApplicationPool, podName string) {
	podSessions := pool.getPodSessions(podName)
	for _, session := range podSessions {
		klog.Infof("Delete session %s on deleted pod %s", util.Name(session), podName)
		pool.deleteSession(session)
	}
	// use go routine to update session status, as session has been remove from pool,
	// update dead session status later will not impact sync application result
	go func() {
		for _, v := range podSessions {
			if util.SessionIsOpen(v) {
				am.changeSessionStatus(v, fornaxv1.SessionStatusClosed)
			} else if util.SessionIsPending(v) {
				am.changeSessionStatus(v, fornaxv1.SessionStatusTimeout)
			}
		}
	}()
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
