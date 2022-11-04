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

package server

import (
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/sessionservice"
	session_grpc "centaurusinfra.io/fornax-serverless/pkg/nodeagent/sessionservice/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
)

var _ sessionservice.SessionService = &sessionServer{}

type sessionServer struct {
	nullService *sessionservice.NullSessionService
	grpcService *session_grpc.GrpcSessionService
}

// CloseSession implements sessionservice.SessionService
func (*sessionServer) CloseSession(pod *types.FornaxPod, session *types.FornaxSession, graceSeconds uint16) error {
	panic("unimplemented")
}

// OpenSession implements sessionservice.SessionService
func (*sessionServer) OpenSession(pod *types.FornaxPod, session *types.FornaxSession, stateCallbackFunc func(internal.SessionState)) error {
	panic("unimplemented")
}

// PingSession implements sessionservice.SessionService
func (*sessionServer) PingSession(pod *types.FornaxPod, session *types.FornaxSession, stateCallbackFunc func(internal.SessionState)) error {
	panic("unimplemented")
}

func NewSessionService() *sessionServer {
	return &sessionServer{
		nullService: &sessionservice.NullSessionService{},
		grpcService: session_grpc.NewSessionService(),
	}
}
