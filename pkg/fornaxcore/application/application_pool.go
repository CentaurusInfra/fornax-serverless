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
	summary.pendingCount = int32(len(pool.podsByState[PodStatePending]))
	summary.deletingCount = int32(len(pool.podsByState[PodStateDeleting]))
	summary.occupiedCount = int32(len(pool.podsByState[PodStateAllocated]))
	summary.idleCount = int32(len(pool.podsByState[PodStateIdle]))
	summary.totalCount = summary.pendingCount + summary.deletingCount + summary.idleCount + summary.occupiedCount

	return summary
}

func (pool *ApplicationPool) getPodSessions(podName string) []*ApplicationSession {
	sessions := []*ApplicationSession{}
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pod := pool._getPodNoLock(podName)
	if pod == nil {
		return sessions
	}
	for k := range pod.sessions {
		if s := pool._getSessionNoLock(k); s != nil {
			sessions = append(sessions, s)
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
	for _, pods := range pool.podsByState {
		if p, found := pods[podName]; found {
			return p
		}
	}
	return nil
}

func (pool *ApplicationPool) podStateTransitionAllowed(oldState, newState ApplicationPodState) bool {
	if oldState == newState {
		return true
	} else if oldState == PodStatePending {
		return true
	} else if oldState == PodStateIdle && newState != PodStatePending {
		return true
	} else if oldState == PodStateAllocated && newState != PodStatePending {
		return true
	} else if oldState == PodStateDeleting && newState == PodStateDeleting {
		return true
	}
	return false
}

// find pod in a state map, move it to different state map and add session bundle on it
func (pool *ApplicationPool) addOrUpdatePod(podName string, podState ApplicationPodState, sessionNames []string) *ApplicationPod {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if p := pool._getPodNoLock(podName); p != nil {
		if !pool.podStateTransitionAllowed(p.state, podState) {
			return p
		}
	}
	return pool._addOrUpdatePodNoLock(podName, podState, sessionNames)
}

// move pod from a state bucket to new state bucket and update its session map
func (pool *ApplicationPool) _addOrUpdatePodNoLock(podName string, podNewState ApplicationPodState, sessionNames []string) *ApplicationPod {
	for _, pods := range pool.podsByState {
		if p, f := pods[podName]; f {
			for _, v := range sessionNames {
				p.sessions[v] = true
			}
			if p.state == podNewState {
				return p
			} else {
				p.state = podNewState
				pool.podsByState[podNewState][podName] = p
				delete(pods, podName)
				return p
			}
		}
	}

	// not found, add it
	p := NewApplicationPod(podName, podNewState)
	for _, v := range sessionNames {
		p.sessions[v] = true
	}
	pool.podsByState[podNewState][podName] = p
	return p
}

func (pool *ApplicationPool) deletePod(podName string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for _, v := range pool.podsByState {
		delete(v, podName)
	}
}

func (pool *ApplicationPool) getSomeIdlePods(num int) []*ApplicationPod {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pods := []*ApplicationPod{}
	for _, v := range pool.podsByState[PodStateIdle] {
		if len(pods) == num {
			break
		}
		pods = append(pods, v)
	}
	return pods
}

func (pool *ApplicationPool) activePodNums() (occupiedPods, pendingPods, idlePods int) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pendingPods = len(pool.podsByState[PodStatePending])
	idlePods = len(pool.podsByState[PodStateIdle])
	occupiedPods = len(pool.podsByState[PodStateAllocated])
	return occupiedPods, pendingPods, idlePods
}

func (pool *ApplicationPool) podLength() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	length := 0
	for _, v := range pool.podsByState {
		length += len(v)
	}
	return length
}

func (pool *ApplicationPool) podListOfState(state ApplicationPodState) []*ApplicationPod {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pods := []*ApplicationPod{}
	for _, p := range pool.podsByState[state] {
		pods = append(pods, p)
	}
	return pods
}

func (pool *ApplicationPool) podList() []*ApplicationPod {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pods := []*ApplicationPod{}
	for _, v := range pool.podsByState {
		for _, p := range v {
			pods = append(pods, p)
		}
	}
	return pods
}

type ApplicationSessionSummary struct {
	pendingCount  int
	startingCount int
	runningCount  int
	timeoutCount  int
	deletingCount int
}

func (pool *ApplicationPool) sessionLength() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	num := 0
	for _, v := range pool.sessions {
		num += len(v)
	}
	return num
}

func (pool *ApplicationPool) sessionList() []*ApplicationSession {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	sessions := []*ApplicationSession{}
	for _, v := range pool.sessions {
		for _, s := range v {
			sessions = append(sessions, s)
		}
	}
	return sessions
}

func (pool *ApplicationPool) summarySession() ApplicationSessionSummary {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	summary := ApplicationSessionSummary{}
	summary.deletingCount = len(pool.sessions[SessionStateDeleting])
	summary.runningCount = len(pool.sessions[SessionStateRunning])
	summary.startingCount = len(pool.sessions[SessionStateStarting])
	summary.pendingCount = len(pool.sessions[SessionStatePending])

	return summary
}

func (pool *ApplicationPool) getSession(key string) *ApplicationSession {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return pool._getSessionNoLock(key)
}

func (pool *ApplicationPool) _getSessionNoLock(key string) *ApplicationSession {
	for _, v := range pool.sessions {
		if s, found := v[key]; found {
			return s
		}
	}

	return nil
}

func (pool *ApplicationPool) addSession(sessionName string, session *fornaxv1.ApplicationSession) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	newState := SessionStatePending
	if session.DeletionTimestamp != nil || util.SessionIsClosing(session) {
		newState = SessionStateDeleting
	} else if util.SessionIsStarting(session) {
		newState = SessionStateStarting
	} else if util.SessionIsPending(session) {
		newState = SessionStatePending
	} else if util.SessionIsOpen(session) {
		newState = SessionStateRunning
	} else {
		pool._deleteSessionNoLock(session)
		return
	}

	s := pool._getSessionNoLock(sessionName)
	if s != nil {
		if pool.sessionStateTransitionAllowed(s.state, newState) {
			delete(pool.sessions[s.state], sessionName)
		} else {
			return
		}
	}

	// update pool with new state
	pool.sessions[newState][sessionName] = &ApplicationSession{
		session: session,
		state:   newState,
	}
	if session.Status.PodReference != nil {
		podName := session.Status.PodReference.Name
		pool._addOrUpdatePodNoLock(podName, PodStateAllocated, []string{sessionName})
	}
}

func (pool *ApplicationPool) sessionStateTransitionAllowed(oldState, newState ApplicationSessionState) bool {
	if oldState == newState {
		return true
	} else if oldState == SessionStatePending {
		return true
	} else if oldState == SessionStateStarting && newState != SessionStatePending {
		return true
	} else if oldState == SessionStateRunning && newState != SessionStatePending && newState != SessionStateStarting {
		return true
	} else if oldState == SessionStateDeleting && newState != SessionStatePending && newState != SessionStateStarting && newState != SessionStateRunning {
		return true
	}
	return false
}

func (pool *ApplicationPool) deleteSession(session *fornaxv1.ApplicationSession) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool._deleteSessionNoLock(session)
}

// delete a session from application pool, and delete it from referenced pod's session map, and change pod state back to idle state,
// only allow from allocated => idle when delete a session from this pod, pod is in pending/deleting state should keep its state
func (pool *ApplicationPool) _deleteSessionNoLock(session *fornaxv1.ApplicationSession) {
	sessionName := util.Name(session)
	if session.Status.PodReference != nil {
		podName := session.Status.PodReference.Name
		for _, podsOfState := range pool.podsByState {
			if pod, found := podsOfState[podName]; found {
				delete(pod.sessions, sessionName)
				if len(pod.sessions) == 0 && pod.state == PodStateAllocated {
					delete(podsOfState, podName)
					pod.state = PodStateIdle
					pool.podsByState[PodStateIdle][podName] = pod
				}
				break
			}
		}
	}
	for _, v := range pool.sessions {
		delete(v, sessionName)
	}
}

// getNonRunningSessions return a list of session of different states,
// pending, not assigned to pod yet
// deleting, delete requested
// timeout, session timedout to get a pod, or session assigned to node, but timeout to get session state from node
func (pool *ApplicationPool) getNonRunningSessions() (pendingSessions, deletingSessions, timeoutSessions []*ApplicationSession) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	for _, s := range pool.sessions[SessionStatePending] {
		timeoutDuration := DefaultSessionOpenTimeoutDuration
		if s.session.Spec.OpenTimeoutSeconds > 0 {
			timeoutDuration = time.Duration(s.session.Spec.OpenTimeoutSeconds) * time.Second
		}
		pendingTimeoutTimeStamp := time.Now().Add(-1 * timeoutDuration)
		if s.session.CreationTimestamp.Time.Before(pendingTimeoutTimeStamp) {
			timeoutSessions = append(timeoutSessions, s)
		} else {
			pendingSessions = append(pendingSessions, s)
		}
	}

	for _, s := range pool.sessions[SessionStateStarting] {
		timeoutDuration := DefaultSessionOpenTimeoutDuration
		if s.session.Spec.OpenTimeoutSeconds > 0 {
			timeoutDuration = time.Duration(s.session.Spec.OpenTimeoutSeconds) * time.Second
		}
		pendingTimeoutTimeStamp := time.Now().Add(-1 * timeoutDuration)
		if s.session.CreationTimestamp.Time.Before(pendingTimeoutTimeStamp) {
			timeoutSessions = append(timeoutSessions, s)
		} else {
			pendingSessions = append(pendingSessions, s)
		}
	}

	for _, s := range pool.sessions[SessionStateDeleting] {
		deletingSessions = append(deletingSessions, s)
	}

	return pendingSessions, deletingSessions, timeoutSessions
}

// add active session into application's session pool and delete terminal session from pool
// add session/delete session will update pod state according pod's session usage
func updateSessionPool(pool *ApplicationPool, session *fornaxv1.ApplicationSession) {
	sessionName := util.Name(session)
	if util.SessionInTerminalState(session) {
		pool.deleteSession(session)
	} else {
		// a trick to make sure pending session are sorted using micro second, api server truncate creation timestamp to second
		session.CreationTimestamp = *util.NewCurrentMetaTime()
		pool.addSession(sessionName, session)
	}
}

func getSessionApplicationKey(session *fornaxv1.ApplicationSession) string {
	applicationName := session.Spec.ApplicationName
	namespace := session.Namespace
	return fmt.Sprintf("%s/%s", namespace, applicationName)
}
