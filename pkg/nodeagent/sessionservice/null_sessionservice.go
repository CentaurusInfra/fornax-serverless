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

var _ SessionService = &NullSessionService{}

type NullSessionService struct {
	stateCallbackFuncs map[string]func(internal.SessionState)
}

// CloseSession implements SessionService
func (f *NullSessionService) CloseSession(pod *types.FornaxPod, session *types.FornaxSession, graceseconds uint16) error {
	if c, found := f.stateCallbackFuncs[session.Identifier]; found {
		c(internal.SessionState{
			SessionId:      session.Identifier,
			SessionState:   types.SessionStateClosed,
			ClientSessions: []types.ClientSession{},
		})
		delete(f.stateCallbackFuncs, session.Identifier)
	} else {
		return SessionNotFound
	}
	return nil
}

// OpenSession implements SessionService
func (f *NullSessionService) OpenSession(pod *types.FornaxPod, session *types.FornaxSession, stateCallbackFunc func(internal.SessionState)) error {
	f.stateCallbackFuncs[session.Identifier] = stateCallbackFunc
	stateCallbackFunc(internal.SessionState{
		SessionId:      session.Identifier,
		SessionState:   types.SessionStateReady,
		ClientSessions: []types.ClientSession{},
	})
	return nil
}

// Ping implements SessionService
func (f *NullSessionService) PingSession(pod *types.FornaxPod, session *types.FornaxSession, stateCallbackFunc func(internal.SessionState)) error {
	if _, found := f.stateCallbackFuncs[session.Identifier]; found {
		return nil
	} else {
		return SessionNotFound
	}
}

// NullSessionService used when pod do not use session service to open/close session, have a NullSessionService just make the pod actor handle sessions in same way for all pods no matter they use session service or not.
// it does not check session status, it just return a dumb message to fool pod actor
func NewNullSessionService() *NullSessionService {
	return &NullSessionService{
		stateCallbackFuncs: map[string]func(internal.SessionState){},
	}
}
