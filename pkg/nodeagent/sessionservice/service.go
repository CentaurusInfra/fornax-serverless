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
)

var (
	SessionNotFoundError = errors.New("Session not found")
	SessionAlreadyExist  = errors.New("Session is already open")
)

type SessionService interface {
	OpenSession(podId, sessionId string, sessionData string, stateCallbackFunc func(internal.SessionState)) error
	CloseSession(sessionId string) error
	Ping(sessionId string) (internal.SessionState, error)
}
