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
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/message"
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/sessionservice"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/util"
)

type SessionActor struct {
	stop           bool
	pod            *types.FornaxPod
	session        *types.FornaxSession
	sessionService sessionservice.SessionService
	supervisor     message.ActorRef
}

const (
	DefaultCloseSessionGraceSeconds = uint32(120)
)

func NewSessionActor(pod *types.FornaxPod, session *types.FornaxSession, sessionService sessionservice.SessionService, supervisor message.ActorRef) *SessionActor {
	actor := &SessionActor{
		pod:            pod,
		session:        session,
		sessionService: sessionService,
		supervisor:     supervisor,
	}
	return actor
}

// try to open a session with session service, if it failed, send a session closed message
func (a *SessionActor) OpenSession() error {
	err := util.BackoffExec(1*time.Millisecond, 10*time.Millisecond, 2*time.Second, 2, func() error {
		return a.sessionService.OpenSession(a.pod, a.session, a.notifySessionState)
	})

	if err != nil {
		if err == sessionservice.SessionAlreadyExist {
			// TODO handle it
		} else {
			a.notifySessionState(internal.SessionState{
				SessionId:      a.session.Identifier,
				SessionState:   types.SessionStateClosed,
				ClientSessions: []types.ClientSession{},
			})
			return err
		}
	}
	return nil
}

// try to close a session with session service, if session already closed, send a session closed message again
// if session service do not have this session, send closed message
func (a *SessionActor) CloseSession() (err error) {
	graceSeconds := DefaultCloseSessionGraceSeconds
	if a.session.Session.Spec.CloseGracePeriodSeconds != nil {
		graceSeconds = *a.session.Session.Spec.CloseGracePeriodSeconds
	}
	err = a.sessionService.CloseSession(a.pod, a.session, graceSeconds)
	if err != nil && err == sessionservice.SessionNotFound {
		a.notifySessionState(internal.SessionState{
			SessionId:      a.session.Identifier,
			SessionState:   types.SessionStateClosed,
			ClientSessions: []types.ClientSession{},
		})
	}
	return err
}

func (a *SessionActor) PingSession() error {
	return a.sessionService.PingSession(a.pod, a.session, a.notifySessionState)
}

// session actor forward session state to pod to handle
func (a *SessionActor) notifySessionState(state internal.SessionState) {
	message.Send(nil, a.supervisor, state)
}
