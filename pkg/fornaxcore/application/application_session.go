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
	"errors"
	"fmt"
	"reflect"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

var (
	SessionIsAlreadyDeleted  = errors.New("Session is already deleted")
	ApplicationNotFound      = errors.New("Application not found")
	PodNotFound              = errors.New("Pod not found")
	NoAvailablePodForSession = errors.New("Cannot find a pod for session")
)

const (
	DefaultSessionPendingTimeoutDuration = 5 * time.Second
	DefaultSessionOpenTimeoutDuration    = 10 * time.Second
)

type ApplicationSessionSummary struct {
	pendingCount  int32
	startingCount int32
	idleCount     int32
	inUseCount    int32
	timeoutCount  int32
	closedCount   int32
	deletingCount int32
}

func (pool *ApplicationPool) sessionLength() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return len(pool.sessions)
}

func (pool *ApplicationPool) sessionList() []*fornaxv1.ApplicationSession {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	keys := []*fornaxv1.ApplicationSession{}
	for _, v := range pool.sessions {
		keys = append(keys, v)
	}
	return keys
}

func (pool *ApplicationPool) summarySessions() ApplicationSessionSummary {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	summary := ApplicationSessionSummary{}
	for _, v := range pool.sessionList() {
		if v.DeletionTimestamp == nil {
			if v.Status.SessionStatus == fornaxv1.SessionStatusUnspecified || v.Status.SessionStatus == fornaxv1.SessionStatusPending {
				summary.pendingCount += 1
			} else if v.Status.SessionStatus == fornaxv1.SessionStatusAvailable {
				summary.idleCount += 1
			} else if v.Status.SessionStatus == fornaxv1.SessionStatusStarting {
				summary.startingCount += 1
			} else if v.Status.SessionStatus == fornaxv1.SessionStatusOccupied {
				summary.inUseCount += 1
			} else if v.Status.SessionStatus == fornaxv1.SessionStatusClosed {
				summary.closedCount += 1
			} else if v.Status.SessionStatus == fornaxv1.SessionStatusTimeout {
				summary.timeoutCount += 1
			} else {
				summary.pendingCount += 1
			}
		} else {
			summary.deletingCount += 1
		}
	}
	return summary
}

func (pool *ApplicationPool) getSession(key string) *fornaxv1.ApplicationSession {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	if v, found := pool.sessions[key]; found {
		return v
	}

	return nil
}

func (pool *ApplicationPool) addSession(key string, session *fornaxv1.ApplicationSession) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.sessions[key] = session
}

func (pool *ApplicationPool) deleteSession(key string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.sessions, key)
}

// groupSessionsByState return a list of session of different states,
// pending, not assigned to pod yet
// deleting, delete requested
// timeout, session timedout to get a pod, or session assigned to node, but timeout to get session state from node
// active, session assigned to pod, waiting for started by pod or being used or waiting for connection
func (pool *ApplicationPool) groupSessionsByState() (pendingSessions, deletingSessions, timeoutSessions, activeSessions []*fornaxv1.ApplicationSession) {
	for _, v := range pool.sessionList() {
		timeoutDuration := DefaultSessionOpenTimeoutDuration
		if v.Spec.OpenTimeoutSeconds > 0 {
			timeoutDuration = time.Duration(v.Spec.OpenTimeoutSeconds) * time.Second
		}
		pendingTimeoutTimeStamp := time.Now().Add(-1 * timeoutDuration)
		if v.DeletionTimestamp != nil {
			deletingSessions = append(deletingSessions, v)
		} else {
			if util.SessionIsPending(v) {
				if v.CreationTimestamp.Time.Before(pendingTimeoutTimeStamp) {
					timeoutSessions = append(timeoutSessions, v)
				} else {
					pendingSessions = append(pendingSessions, v)
				}
			} else if v.Status.SessionStatus == fornaxv1.SessionStatusStarting {
				if v.CreationTimestamp.Time.Before(pendingTimeoutTimeStamp) {
					timeoutSessions = append(timeoutSessions, v)
				} else {
					activeSessions = append(activeSessions, v)
				}
			} else if util.SessionIsOpen(v) {
				activeSessions = append(activeSessions, v)
			}
		}
	}
	return pendingSessions, deletingSessions, timeoutSessions, activeSessions
}

func (appc *ApplicationManager) changeSessionStatus(v *fornaxv1.ApplicationSession, status fornaxv1.SessionStatus) error {
	newStatus := v.Status.DeepCopy()
	newStatus.SessionStatus = status
	if status == fornaxv1.SessionStatusClosed || status == fornaxv1.SessionStatusTimeout {
		// reset client session to let it can be hard deleted
		newStatus.ClientSessions = []v1.LocalObjectReference{}
	}
	_, err := appc.sessionManager.UpdateSessionStatus(v, newStatus)
	if err != nil {
		return err
	}
	return nil
}

func (appc *ApplicationManager) getSessionApplicationKey(session *fornaxv1.ApplicationSession) (string, error) {
	applicationLabel := session.Spec.ApplicationName
	namespace, name, err := cache.SplitMetaNamespaceKey(applicationLabel)
	if err == nil {
		application, err := appc.applicationLister.Applications(namespace).Get(name)
		if err != nil {
			return "", err
		}

		applicationKey := util.ResourceName(application)
		return applicationKey, nil
	} else {
		return "", fmt.Errorf("Session application label:%s is not valid meta namespace key", applicationLabel)
	}
}

// add active session into application's session pool and delete terminal session from pool
func (appc *ApplicationManager) updateSessionPool(applicationKey, sessionKey string, session *fornaxv1.ApplicationSession) {
	pool := appc.getOrCreateApplicationPool(applicationKey)
	if util.SessionInTerminalState(session) {
		if session.Status.PodReference != nil {
			pod := pool.getPod(session.Status.PodReference.Name)
			if pod != nil {
				pod.sessions.Delete(sessionKey)
			}
		}
		pool.deleteSession(sessionKey)
	} else {
		if session.Status.PodReference != nil {
			podName := session.Status.PodReference.Name
			pod := pool.addPod(podName, NewApplicationPod(podName))
			pod.sessions.Add(sessionKey)
		}
		pool.addSession(sessionKey, session)
	}
}

// callback from Application informer when ApplicationSession is created
// if session in terminal state, remove this session from pool(should not happen for a new session, but for weird case)
// else add new copy into pool
func (appc *ApplicationManager) onApplicationSessionAddEvent(obj interface{}) {
	session := obj.(*fornaxv1.ApplicationSession)
	if session.DeletionTimestamp != nil {
		appc.onApplicationSessionDeleteEvent(obj)
		return
	}
	sessionKey := util.ResourceName(session)
	applicationKey, err := appc.getSessionApplicationKey(session)
	if err != nil {
		klog.ErrorS(err, "Can not get application key", "session", session)
		if apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Get a session of deleted application, close it", "session", session)
			appc.closeApplicationSession(session)
		}
		return
	}
	klog.InfoS("Adding application session", "sessio", sessionKey)
	appc.updateSessionPool(applicationKey, sessionKey, session)
	appc.enqueueApplication(applicationKey)
}

// callback from Application informer when ApplicationSession is updated
// if session in terminal state, remove this session from pool,
// else add new copy into pool
func (appc *ApplicationManager) onApplicationSessionUpdateEvent(old, cur interface{}) {
	oldCopy := old.(*fornaxv1.ApplicationSession)
	newCopy := cur.(*fornaxv1.ApplicationSession)
	if reflect.DeepEqual(oldCopy, newCopy) {
		return
	}

	sessionKey := util.ResourceName(newCopy)
	applicationKey, err := appc.getSessionApplicationKey(newCopy)
	if err != nil {
		klog.ErrorS(err, "Can not get application key", "session", newCopy)
		if apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Get a session of deleted application, close it", "session", newCopy)
			appc.closeApplicationSession(newCopy)
		}
		return
	}

	appc.updateSessionPool(applicationKey, sessionKey, newCopy)
	// do not need to sync application session unless deleting or status change
	if (newCopy.DeletionTimestamp != nil && oldCopy.DeletionTimestamp == nil) || !reflect.DeepEqual(oldCopy.Status, newCopy.Status) {
		klog.InfoS("Updating application session", "session", sessionKey, "status", newCopy.Status, "deleting", newCopy.DeletionTimestamp != nil)
		appc.enqueueApplication(applicationKey)
	}
}

// callback from Application informer when ApplicationSession is physically deleted
func (appc *ApplicationManager) onApplicationSessionDeleteEvent(obj interface{}) {
	session := obj.(*fornaxv1.ApplicationSession)
	if session.DeletionTimestamp != nil {
		session.DeletionTimestamp = util.NewCurrentMetaTime()
	}

	sessionKey := util.ResourceName(session)
	applicationKey, err := appc.getSessionApplicationKey(session)
	if err != nil {
		klog.ErrorS(err, "Can not get application key", "session", session)
		if apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Get a deleted session of deleted application, ignore it", "session", session)
		}
		return
	}

	klog.InfoS("Deleting application session", "session", sessionKey, "status", session.Status)
	appc.updateSessionPool(applicationKey, sessionKey, session)
	appc.enqueueApplication(applicationKey)
}

// return sum of of all in use, idle, pending session, and all pending sessions
func (appc *ApplicationManager) getTotalAndPendingSessionNum(applicationKey string) (int, int) {
	if pool := appc.getApplicationPool(applicationKey); pool != nil {
		summary := pool.summarySessions()
		return int(summary.idleCount + summary.inUseCount + summary.pendingCount + summary.startingCount), int(summary.pendingCount)
	}
	return 0, 0
}

// syncApplicationSessions grab a list of pending session and try to allocate them to pods and call OpenSession on choosen pod.
// session status change in memory to SessionStatusStarting, but do not update etcd to avoid unnecessary resync.
// session status will be changed in etcd until pod report back, if fornax core restart and lost these memory state, it rely on pod to report back.
// It also cleanup session when a session is in Starting or Pending state for more than a timeout duration.
// session is changed to SessionStatusTimeout, session client need to create a new session.
// It also cleanup session in deletingSessions when a session is in Starting or Pending state for more than a timeout duration.
// session is changed to SessionStatusClosed, session client need to create a new session.
// session timedout and closed are removed from application pool's session list, so, syncApplicationPods do not need to consider these sessions anymore
func (appc *ApplicationManager) syncApplicationSessions(application *fornaxv1.Application, applicationKey string) error {
	pool := appc.getApplicationPool(applicationKey)
	if pool == nil {
		return nil
	}
	_, _, idleRunningPods := appc.groupApplicationPods(applicationKey)
	pendingSessions, deletingSessions, timeoutSessions, _ := pool.groupSessionsByState()
	klog.InfoS("Syncing application session", "application", applicationKey, "#pending", len(pendingSessions), "#deleting", len(deletingSessions), "#timeout", len(timeoutSessions))

	sessionErrors := []error{}
	// 1/ assign pending sessions to idle pod
	si := 0
	for _, rp := range idleRunningPods {
		pod := appc.podManager.FindPod(rp.podName)
		if pod != nil {
			// allow only one sssion for one pod for now
			if rp.sessions.Len() == 0 {
				// have assigned all pending sessions, break
				if si == len(pendingSessions) {
					break
				}

				// update session status and set access point of session
				session := pendingSessions[si]
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
					Name: util.ResourceName(pod),
				}
				session.Status = *newStatus
				err := appc.sessionManager.OpenSession(pod, session)
				if err != nil {
					// move to next pod, it could fail to accept other session also
					klog.ErrorS(err, "Failed to open session", "app", applicationKey, "session", session.Name, "pod", util.ResourceName(pod))
					sessionErrors = append(sessionErrors, err)
					continue
				} else {
					rp.sessions.Add(util.ResourceName(session))
					si += 1
					// appc.sessionManager.UpdateSessionStatus(session, newStatus)
				}
			}
		}
	}

	// 2, cleanup timeout session, set session status to timeout and delete it from list
	for _, v := range timeoutSessions {
		if err := appc.changeSessionStatus(v, fornaxv1.SessionStatusTimeout); err == nil {
			pool.deleteSession(util.ResourceName(v))
		} else {
			sessionErrors = append(sessionErrors, err)
		}
	}

	// 3, cleanup deleting session,
	// if session is open, close it and wait for node report back
	// if session is still in pending, change status to closed and delete it from session list
	// if session is not open or pending, just delete since it's already in a terminal state
	for _, v := range deletingSessions {
		if util.SessionIsOpen(v) {
			err := appc.closeApplicationSession(v)
			if err != nil {
				sessionErrors = append(sessionErrors, err)
			}
		} else if util.SessionIsPending(v) {
			if err := appc.changeSessionStatus(v, fornaxv1.SessionStatusClosed); err == nil {
				pool.deleteSession(util.ResourceName(v))
			}
		} else {
			pool.deleteSession(util.ResourceName(v))
		}
	}

	if len(sessionErrors) > 0 {
		return errors.New("Some sessions failed to sync")
	}

	return nil
}

func (appc *ApplicationManager) closeApplicationSession(v *fornaxv1.ApplicationSession) error {
	klog.Infof("Close applciation sessions %s", util.ResourceName(v))
	if v.Status.PodReference != nil {
		podName := v.Status.PodReference.Name
		pod := appc.podManager.FindPod(podName)
		if pod != nil {
			return appc.sessionManager.CloseSession(pod, v)
		} else {
			// TODO, how to handle it, this case could happen when FornaxCore restart,
			// it have not get all pods reported by node, and client want to close a session
			return PodNotFound
		}
	}

	return nil
}

func (appc *ApplicationManager) sessionHouseKeeping() error {
	apps := appc.applicationList()
	klog.Info("cleanup timeout session")
	for _, v := range apps {
		_, _, timeoutSessions, _ := v.groupSessionsByState()
		for _, v := range timeoutSessions {
			appc.changeSessionStatus(v, fornaxv1.SessionStatusTimeout)
		}
	}

	return nil
}

// cleanupSessionOnDeletedPod handle pod is terminated unexpectedly, e.g. node crash
// in normal cases,session should be closed before pod is terminated and deleted.
// It update open session to closed and pending session to timedout,
// and does not try to call node to close session, as session does not exist at all on node when pod deleted
func (appc *ApplicationManager) cleanupSessionOnDeletedPod(pool *ApplicationPool, podName string) error {
	klog.Infof("Delete all sessions of deleted pod %s", podName)
	sessions := pool.sessionList()
	podSessions := []*fornaxv1.ApplicationSession{}
	for _, v := range sessions {
		if v.Status.PodReference != nil && v.Status.PodReference.Name == podName {
			klog.Infof("Delete sessions %s", util.ResourceName(v))
			pool.deleteSession(util.ResourceName(v))
			podSessions = append(podSessions, v)
		}
	}
	// use go routine to update session status, as session has been remove from pool,
	// update dead session status later will not impact sync application result
	go func() {
		for _, v := range podSessions {
			if util.SessionIsOpen(v) {
				appc.changeSessionStatus(v, fornaxv1.SessionStatusClosed)
			} else if util.SessionIsPending(v) {
				appc.changeSessionStatus(v, fornaxv1.SessionStatusTimeout)
			}
		}
	}()

	return nil
}

// cleanupSessionOfApplication if a application is being deleted,
// terminate all pods which are still alive and delete pods from application pod pool if it does not exist anymore in Pod Manager
// when alive pods reported as terminated by Node Agent, then application can be eventually deleted
func (appc *ApplicationManager) cleanupSessionOfApplication(applicationKey string) error {
	klog.Infof("Delete all sessions of application %s", applicationKey)
	deleteErrors := []error{}

	if pool := appc.getApplicationPool(applicationKey); pool != nil {
		// for being deleted session, no need to touch, status will be changed when pod are terminated
		sessions := pool.sessionList()
		for _, v := range sessions {
			var err error
			switch {
			case util.SessionIsOpen(v):
				err = appc.closeApplicationSession(v)
				if err != nil {
					deleteErrors = append(deleteErrors, err)
				}
			case util.SessionIsPending(v):
				err = appc.changeSessionStatus(v, fornaxv1.SessionStatusTimeout)
				if err == nil {
					pool.deleteSession(util.ResourceName(v))
				} else {
					deleteErrors = append(deleteErrors, err)
				}
			default:
				// do nothing
			}

		}

		if len(deleteErrors) != 0 {
			return fmt.Errorf("Some sessions failed to be deleted, num=%d", len(deleteErrors))
		}
	}

	return nil
}
