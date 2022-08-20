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

package sessionservice

import (
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
)

var _ SessionService = &FakeSessionService{}

type FakeSessionService struct {
	stateCallbackFuncs map[string]func(internal.SessionState)
}

// CloseSession implements SessionService
func (f *FakeSessionService) CloseSession(sessionId string) error {
	if c, f := f.stateCallbackFuncs[sessionId]; f {
		c(internal.SessionState{
			SessionId:      sessionId,
			SessionState:   types.SessionStateClosed,
			ClientSessions: []types.ClientSession{},
		})
	} else {
		return SessionNotFoundError
	}
	return nil
}

// OpenSession implements SessionService
func (f *FakeSessionService) OpenSession(podId string, sessionId string, sessionData string, stateCallbackFunc func(internal.SessionState)) error {
	f.stateCallbackFuncs[sessionId] = stateCallbackFunc
	stateCallbackFunc(internal.SessionState{
		SessionId:      sessionId,
		SessionState:   types.SessionStateReady,
		ClientSessions: []types.ClientSession{},
	})
	return nil
}

// Ping implements SessionService
func (f *FakeSessionService) Ping(sessionId string) (internal.SessionState, error) {
	return internal.SessionState{
		SessionId:      sessionId,
		SessionState:   types.SessionStateReady,
		ClientSessions: []types.ClientSession{},
	}, nil
}

func NewFakeSessionService() *FakeSessionService {
	return &FakeSessionService{
		stateCallbackFuncs: map[string]func(internal.SessionState){},
	}
}
