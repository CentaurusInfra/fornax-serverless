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
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/message"
)

// +enum
type SessionActorState string

const (
	Initialiazing        SessionActorState = "Initialiazing"
	Ready                SessionActorState = "Ready"
	InsufficientResource SessionActorState = "InsufficientResource"
)

type ClientSession struct {
}

type SessionActor struct {
	message.LocalChannelActor
	State         SessionActorState
	Receiver      chan grpc.FornaxCoreMessage
	ClientSession map[string]ClientSession
}

func (n *SessionActor) Start() {
	// new a proto.actor and start to listen to message
}

type SessionActorConfiguration struct {
}

func NewSessionActor(state SessionActorState, config SessionActorConfiguration) *SessionActor {
	actor := &SessionActor{}
	return actor
}

func OnSessionStart(msg *grpc.SessionStart) error {
	// find pod actor and send a message to it
	// if pod actor does not exist, create one
	return nil
}

func OnSessionClose(msg *grpc.SessionClose) error {
	// find pod actor and send a message to it
	// if pod actor does not exist, create one
	return nil
}
