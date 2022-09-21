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
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/util"
)

type ApplicationPodSummary struct {
	totalCount    int32
	pendingCount  int32
	deletingCount int32
	idleCount     int32
	occupiedCount int32
}

func (pool *ApplicationPool) summaryPod(podManager ie.PodManagerInterface) ApplicationPodSummary {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	summary := ApplicationPodSummary{}
	summary.pendingCount = int32(len(pool.pods[PodStatePending]))
	summary.deletingCount = int32(len(pool.pods[PodStateDeleting]))
	summary.occupiedCount = int32(len(pool.pods[PodStateAllocated]))
	summary.idleCount = int32(len(pool.pods[PodStateIdle]))
	summary.totalCount = summary.pendingCount + summary.deletingCount + summary.idleCount + summary.occupiedCount

	return summary
}

func (pool *ApplicationPool) getPodSessions(podName string) []*fornaxv1.ApplicationSession {
	sessions := []*fornaxv1.ApplicationSession{}
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pod := pool._getPodNoLock(podName)
	if pod == nil {
		return sessions
	}
	for k := range pod.sessions {
		if sess := pool._getSessionNoLock(k); sess != nil {
			sessions = append(sessions, sess)
		}
	}
	return sessions
}

func (pool *ApplicationPool) getPod(podName string) *ApplicationPod {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return pool._getPodNoLock(podName)
}

func (pool *ApplicationPool) _getPodNoLock(podName string) *ApplicationPod {
	for _, pods := range pool.pods {
		if p, found := pods[podName]; found {
			return p
		}
	}
	return nil
}

// find pod in a state map, move it to different state map and add session bundle on it
func (pool *ApplicationPool) addOrUpdatePod(podName string, podState ApplicationPodState, sessionIds []string) *ApplicationPod {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return pool._addOrUpdatePodNoLock(podName, podState, sessionIds)
}

func (pool *ApplicationPool) _addOrUpdatePodNoLock(podName string, podState ApplicationPodState, sessionIds []string) *ApplicationPod {
	for _, pods := range pool.pods {
		if p, f := pods[podName]; f {
			for _, v := range sessionIds {
				p.sessions[v] = true
			}
			if p.state == podState {
				return p
			} else {
				p.state = podState
				pool.pods[podState][podName] = p
				delete(pods, podName)
				return p
			}
		}
	}

	// not found, add it
	p := NewApplicationPod(podName, podState)
	for _, v := range sessionIds {
		p.sessions[v] = true
	}
	pool.pods[podState][podName] = p
	return p
}

func (pool *ApplicationPool) deletePod(podName string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for _, v := range pool.pods {
		delete(v, podName)
	}
}

func (pool *ApplicationPool) getSomeIdlePods(num int) []*ApplicationPod {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pods := []*ApplicationPod{}
	for _, v := range pool.pods[PodStateIdle] {
		if len(pods) == num {
			break
		}
		pods = append(pods, v)
	}
	return pods
}

func (pool *ApplicationPool) activeApplicationPodLength() (occupiedPods, pendingPods, idlePods int) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pendingPods = len(pool.pods[PodStatePending])
	idlePods = len(pool.pods[PodStateIdle])
	occupiedPods = len(pool.pods[PodStateAllocated])
	return occupiedPods, pendingPods, idlePods
}

func (pool *ApplicationPool) podLength() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	length := 0
	for _, v := range pool.pods {
		length += len(v)
	}
	return length
}

func (pool *ApplicationPool) podListOfState(state ApplicationPodState) []*ApplicationPod {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pods := []*ApplicationPod{}
	for _, p := range pool.pods[state] {
		pods = append(pods, p)
	}
	return pods
}

func (pool *ApplicationPool) podList() []*ApplicationPod {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pods := []*ApplicationPod{}
	for _, v := range pool.pods {
		for _, p := range v {
			pods = append(pods, p)
		}
	}
	return pods
}

type ApplicationSessionSummary struct {
	pendingCount  int32
	startingCount int32
	idleCount     int32
	inUseCount    int32
	timeoutCount  int32
	closedCount   int32
	deletingCount int32
}

// return sum of of all in use, idle, pending session, and all pending sessions
func (pool *ApplicationPool) getTotalAndPendingSessionNum() (int, int) {
	summary := pool.summarySession()
	return int(summary.idleCount + summary.inUseCount + summary.pendingCount + summary.startingCount + summary.deletingCount), int(summary.pendingCount)
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

func (pool *ApplicationPool) summarySession() ApplicationSessionSummary {
	summary := ApplicationSessionSummary{}
	sessions := pool.sessionList()
	for _, v := range sessions {
		if v.DeletionTimestamp == nil {
			if v.Status.SessionStatus == fornaxv1.SessionStatusUnspecified || v.Status.SessionStatus == fornaxv1.SessionStatusPending {
				summary.pendingCount += 1
			} else if v.Status.SessionStatus == fornaxv1.SessionStatusAvailable {
				summary.idleCount += 1
			} else if v.Status.SessionStatus == fornaxv1.SessionStatusStarting {
				summary.startingCount += 1
			} else if v.Status.SessionStatus == fornaxv1.SessionStatusInUse {
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
	return pool._getSessionNoLock(key)
}

func (pool *ApplicationPool) _getSessionNoLock(key string) *fornaxv1.ApplicationSession {
	if v, found := pool.sessions[key]; found {
		return v
	}

	return nil
}

func (pool *ApplicationPool) addSession(key string, session *fornaxv1.ApplicationSession) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if session.Status.PodReference != nil {
		podName := session.Status.PodReference.Name
		pool._addOrUpdatePodNoLock(podName, PodStateAllocated, []string{string(session.GetUID())})
	}
	pool.sessions[key] = session
}

func (pool *ApplicationPool) deleteSession(session *fornaxv1.ApplicationSession) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	sessionId := string(session.GetUID())
	if session.Status.PodReference != nil {
		podName := session.Status.PodReference.Name
		for _, podsOfState := range pool.pods {
			if pod, found := podsOfState[podName]; found {
				delete(pod.sessions, sessionId)
				if len(pod.sessions) == 0 && pod.state == PodStateAllocated {
					// only allow from allocated => idle when delete a session from this pod, pod is in pending/deleting state should keep its state
					delete(podsOfState, podName)
					pod.state = PodStateIdle
					pool.pods[PodStateIdle][podName] = pod
				}
				break
			}
		}
	}
	delete(pool.sessions, sessionId)
}

// groupSessionsByState return a list of session of different states,
// pending, not assigned to pod yet
// deleting, delete requested
// timeout, session timedout to get a pod, or session assigned to node, but timeout to get session state from node
// active, session assigned to pod, waiting for started by pod or being used or waiting for connection
func (pool *ApplicationPool) groupSessionsByState() (pendingSessions, deletingSessions, closingSessions, timeoutSessions, activeSessions []*fornaxv1.ApplicationSession) {
	sessions := pool.sessionList()
	for _, v := range sessions {
		timeoutDuration := DefaultSessionOpenTimeoutDuration
		if v.Spec.OpenTimeoutSeconds > 0 {
			timeoutDuration = time.Duration(v.Spec.OpenTimeoutSeconds) * time.Second
		}
		pendingTimeoutTimeStamp := time.Now().Add(-1 * timeoutDuration)
		if util.SessionIsClosing(v) {
			closingSessions = append(closingSessions, v)
		} else if v.DeletionTimestamp != nil {
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
	return pendingSessions, deletingSessions, closingSessions, timeoutSessions, activeSessions
}
