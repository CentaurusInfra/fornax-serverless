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
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	"k8s.io/klog/v2"
)

type SessionStatusChangeMap struct {
	changes map[string]*fornaxv1.ApplicationSessionStatus
	mu      sync.Mutex
}

type sessionStatusUpdater struct {
	statusUpdateCh chan string
	statusChanges  *SessionStatusChangeMap
	sessionManager *sessionManager
}

// UpdateApplicationSessionStatus put updated status into a map send singal into a channel to asynchronously update session status
func (sm *sessionStatusUpdater) AsyncUpdateSessionStatus(session *fornaxv1.ApplicationSession, newStatus *fornaxv1.ApplicationSessionStatus) error {
	sm.statusChanges.addSessionStatusChange(util.Name(session), newStatus, true)
	if len(sm.statusUpdateCh) == 0 {
		sm.statusUpdateCh <- util.Name(session)
	}
	return nil
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

func NewSessionStatusUpdater(sessionManager *sessionManager) *sessionStatusUpdater {
	return &sessionStatusUpdater{
		statusUpdateCh: make(chan string, 500),
	}
}

// AsyncSessionStatusUpdateRun receive session status signal from channel and grab a snapshot of session status change from session statua map, and update api server,
// snap shot is removed from session status change map, if any session status update failed, put it back if there is no new status
func (sm *sessionStatusUpdater) AsyncSessionStatusUpdateRun(ctx context.Context) {
	klog.Info("Starting fornaxv1 session status manager")

	go func() {
		defer klog.Info("Shutting down fornaxv1 session status manager")

		failedSessions := map[string]*fornaxv1.ApplicationSessionStatus{}
		batchUpdateSession := func(sessionBatch map[string]*fornaxv1.ApplicationSessionStatus) {
			wg := sync.WaitGroup{}
			for name, status := range sessionBatch {
				wg.Add(1)
				go func(name string, status *fornaxv1.ApplicationSessionStatus) {
					err := sm.sessionManager._updateSessionStatus(name, status)
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
