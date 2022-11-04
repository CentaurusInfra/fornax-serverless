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
	"errors"

	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
)

var (
	SessionNotFound                 = errors.New("Session not found")
	SessionAlreadyExist             = errors.New("Session is already open")
	SessionStreamDisconnected       = errors.New("Session stream not connected")
	SessionStreamAlreadyEstablished = errors.New("only one stream connection is allowed from one instance")
)

type SessionService interface {
	OpenSession(pod *types.FornaxPod, session *types.FornaxSession, stateCallbackFunc func(internal.SessionState)) error
	CloseSession(pod *types.FornaxPod, session *types.FornaxSession, graceSeconds uint16) error
	PingSession(pod *types.FornaxPod, session *types.FornaxSession, stateCallbackFunc func(internal.SessionState)) error
}
