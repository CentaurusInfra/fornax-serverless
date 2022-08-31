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
	"fmt"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/message"
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/sessionservice"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/util"
)

type SessionActor struct {
	stop           bool
	session        *types.FornaxSession
	sessionService sessionservice.SessionService
	supervisor     message.ActorRef
}

const (
	DefaultCloseSessionGraceSeconds = uint16(120)
)

func NewSessionActor(session *types.FornaxSession, sessionService sessionservice.SessionService, supervisor message.ActorRef) *SessionActor {
	actor := &SessionActor{
		session:        session,
		sessionService: sessionService,
		supervisor:     supervisor,
	}
	return actor
}

// try to open a session with session service, if it failed, send a session closed message
func (a *SessionActor) OpenSession() error {
	if util.SessionIsClosed(a.session.Session) && len(a.session.ClientSessions) == 0 {
		return fmt.Errorf("Session %s already closed", a.session.Identifier)
	}
	podId := a.session.PodIdentifier
	err := a.sessionService.OpenSession(podId, a.session.Identifier, a.session.Session.Spec.SessionData, a.receiveSessionState)
	if err != nil {
		if err == sessionservice.SessionAlreadyExist {
			// TODO handle it
		} else {
			a.receiveSessionState(internal.SessionState{
				SessionId:      a.session.Identifier,
				SessionState:   types.SessionStateClosed,
				ClientSessions: []types.ClientSession{},
			})
			return err
		}
	}
	return nil

}

// try to open a session with session service, if it failed, send a session closed message
func (a *SessionActor) CloseSession() (err error) {
	graceSeconds := DefaultCloseSessionGraceSeconds
	if a.session.Session.Spec.CloseGracePeriodSeconds != nil {
		graceSeconds = *a.session.Session.Spec.CloseGracePeriodSeconds
	}
	if util.SessionIsOpen(a.session.Session) {
		// save this state to report back to fornaxcore
		a.session.Session.Status.SessionStatus = fornaxv1.SessionStatusClosing
		err = a.sessionService.CloseSession(a.session.PodIdentifier, a.session.Identifier, graceSeconds)
		if err != nil && err == sessionservice.SessionNotFound {
			// send session closed state event
			a.receiveSessionState(internal.SessionState{
				SessionId:      a.session.Identifier,
				SessionState:   types.SessionStateClosed,
				ClientSessions: []types.ClientSession{},
			})
		}
	}
	return err
}

func (a *SessionActor) PingSession() error {
	return a.sessionService.PingSession(a.session.PodIdentifier, a.session.Identifier, a.receiveSessionState)
}

// session actor forward session state to pod to handle
func (a *SessionActor) receiveSessionState(state internal.SessionState) {
	message.Send(nil, a.supervisor, state)
}
