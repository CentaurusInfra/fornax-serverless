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

package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxstore "centaurusinfra.io/fornax-serverless/pkg/store"
	storefactory "centaurusinfra.io/fornax-serverless/pkg/store/factory"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var _ ie.SessionManagerInterface = &sessionManager{}

type SessionStatusChangeMap struct {
	changes map[string]*fornaxv1.ApplicationSessionStatus
	mu      sync.Mutex
}

type sessionManager struct {
	ctx             context.Context
	nodeAgentClient nodeagent.NodeAgentClient
	watchers        []chan<- *ie.SessionEvent
	statusUpdateCh  chan string
	statusChanges   *SessionStatusChangeMap
	sessionStore    fornaxstore.ApiStorageInterface
}

func (sscm *SessionStatusChangeMap) addSessionStatusChange(name string, status *fornaxv1.ApplicationSessionStatus, replace bool) {
	sscm.mu.Lock()
	defer sscm.mu.Unlock()
	if _, found := sscm.changes[name]; found {
		if replace {
			sscm.changes[name] = status
		}
	} else {
		sscm.changes[name] = status
	}
}

func (sscm *SessionStatusChangeMap) getAndRemoveStatusChangeSnapshot() map[string]*fornaxv1.ApplicationSessionStatus {
	sscm.mu.Lock()
	defer sscm.mu.Unlock()
	updatedSessions := map[string]*fornaxv1.ApplicationSessionStatus{}
	for name, v := range sscm.changes {
		updatedSessions[name] = v
	}

	for name := range updatedSessions {
		delete(sscm.changes, name)
	}
	return updatedSessions
}

func NewSessionManager(ctx context.Context, nodeAgentProxy nodeagent.NodeAgentClient, sessionStore fornaxstore.ApiStorageInterface) *sessionManager {
	mgr := &sessionManager{
		ctx:             ctx,
		nodeAgentClient: nodeAgentProxy,
		statusUpdateCh:  make(chan string, 500),
		statusChanges: &SessionStatusChangeMap{
			changes: map[string]*fornaxv1.ApplicationSessionStatus{},
			mu:      sync.Mutex{},
		},
		sessionStore: sessionStore,
	}
	return mgr
}

// AsyncSessionStatusUpdateRun receive session status signal from channel and grab a snapshot of session status change from session statua map, and update api server,
// snap shot is removed from session status change map, if any session status update failed, put it back if there is no new status
func (sm *sessionManager) AsyncSessionStatusUpdateRun(ctx context.Context) {
	klog.Info("Starting fornaxv1 session status manager")

	go func() {
		defer klog.Info("Shutting down fornaxv1 session status manager")

		failedSessions := map[string]*fornaxv1.ApplicationSessionStatus{}
		batchUpdateSession := func(sessionBatch map[string]*fornaxv1.ApplicationSessionStatus) {
			wg := sync.WaitGroup{}
			for name, status := range sessionBatch {
				wg.Add(1)
				go func(name string, status *fornaxv1.ApplicationSessionStatus) {
					err := sm._updateSessionStatus(name, status)
					if err != nil {
						failedSessions[name] = status
						klog.ErrorS(err, "Failed to update session status", "session", name)
					}
					wg.Done()
				}(name, status)
			}
			wg.Wait()
		}

		FornaxCore_SessionStatusManager_Retry := "FornaxCore_SessionStatusManager_StatusUpdate_Retry"
		for {
			select {
			case <-ctx.Done():
				break
			case <-sm.statusUpdateCh:
				// consume all signal in current channel
				remainingLen := len(sm.statusUpdateCh)
				for i := 0; i < remainingLen; i++ {
					<-sm.statusUpdateCh
				}
				sessionStatuses := sm.statusChanges.getAndRemoveStatusChangeSnapshot()

				sessions := map[string]*fornaxv1.ApplicationSessionStatus{}
				for n, v := range sessionStatuses {
					sessions[n] = v
					if len(sessions) == 10 {
						batchUpdateSession(sessions)
						sessions = map[string]*fornaxv1.ApplicationSessionStatus{}
					}
				}
				batchUpdateSession(sessions)

				// a trick to retry, all failed status update are still in map, send a fake update to retry,
				// it's bit risky, if some guy put a lot of event into channel before we can put a retry signal,
				// it will stuck checking channel current length must be zero could mitigate a bit
				for name, status := range failedSessions {
					// use false replace flag, when there is new status in map, do not replace it
					sm.statusChanges.addSessionStatusChange(name, status, false)
					if len(sm.statusUpdateCh) == 0 {
						sm.statusUpdateCh <- FornaxCore_SessionStatusManager_Retry
					}
				}
				if len(failedSessions) > 0 {
					time.Sleep(10 * time.Millisecond)
				}
				failedSessions = map[string]*fornaxv1.ApplicationSessionStatus{}
			}
		}
	}()
}

func (sm *sessionManager) Watch(receiver chan<- *ie.SessionEvent) {
	sm.watchers = append(sm.watchers, receiver)
}

// forward to who are interested in session event
func (sm *sessionManager) NotifySessionStatusFromNode(nodeId string, pod *v1.Pod, sessions []*fornaxv1.ApplicationSession) {
	for _, session := range sessions {
		for _, watcher := range sm.watchers {
			watcher <- &ie.SessionEvent{
				NodeId:  nodeId,
				Pod:     pod,
				Session: session,
				Type:    ie.SessionEventTypeUpdate,
			}
		}
	}
}

func (sm *sessionManager) CloseSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	if nodeName, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreNode]; found {
		return sm.nodeAgentClient.CloseSession(nodeName, pod, session)
	} else {
		return fmt.Errorf("Can not find which node this pod is on, %s", util.Name(pod))
	}
}

func (sm *sessionManager) OpenSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	if nodeName, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreNode]; found {
		return sm.nodeAgentClient.OpenSession(nodeName, pod, session)
	} else {
		return fmt.Errorf("Can not find which node session is on, %s", util.Name(session))
	}
}

// UpdateApplicationSessionStatus put updated status into a map send singal into a channel to asynchronously update session status
func (sm *sessionManager) AsyncUpdateSessionStatus(session *fornaxv1.ApplicationSession, newStatus *fornaxv1.ApplicationSessionStatus) error {
	sm.statusChanges.addSessionStatusChange(util.Name(session), newStatus, true)
	if len(sm.statusUpdateCh) == 0 {
		sm.statusUpdateCh <- util.Name(session)
	}
	return nil
}

// UpdateApplicationSessionStatus put updated status into a map send singal into a channel to asynchronously update session status
func (sm *sessionManager) UpdateSessionStatus(session *fornaxv1.ApplicationSession, newStatus *fornaxv1.ApplicationSessionStatus) error {
	st := time.Now().UnixMicro()
	e := sm._updateSessionStatus(util.Name(session), newStatus)
	et := time.Now().UnixMicro()
	klog.Infof("GWJ update session status took %d micro seconds for %s", et-st, util.Name(session))
	return e
}

// attempts to update the Status of the given Application Session name
func (sm *sessionManager) _updateSessionStatus(sessionName string, newStatus *fornaxv1.ApplicationSessionStatus) error {
	var updateErr error
	for i := 0; i <= 3; i++ {
		session, err := storefactory.GetApplicationSessionCache(sm.sessionStore, sessionName)
		if err != nil {
			return err
		}
		if session == nil {
			return nil
		}

		updatedSession := session.DeepCopy()
		updatedSession.Status = *newStatus
		if util.SessionIsOpen(updatedSession) {
			util.AddFinalizer(&updatedSession.ObjectMeta, fornaxv1.FinalizerOpenSession)
		} else {
			util.RemoveFinalizer(&updatedSession.ObjectMeta, fornaxv1.FinalizerOpenSession)
		}

		_, updateErr = storefactory.UpdateApplicationSession(sm.ctx, sm.sessionStore, updatedSession)
		if updateErr == nil {
			break
		}
	}
	return updateErr
}
