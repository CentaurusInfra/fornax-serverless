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
	"encoding/json"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"k8s.io/klog/v2"
)

type SessionStatusChange struct{}
type SessionCreated struct{}
type SessionTerminated struct{}
type SessionStarted struct{}
type SessionLive struct{}
type SessionStandby struct{}
type ClientSessionJoin struct{}
type ClientSessionExit struct{}

func BuildFornaxcoreGrpcSessionState(revision int64, session *types.FornaxSession) *grpc.FornaxCoreMessage {
	sessionData, err := json.Marshal(session.Session)
	if err != nil {
		// not supposed to happen, mostly is program OOM, session data should be good, ignore
		klog.ErrorS(err, "Failed to marshar session object", "session", session.Identifier)
		return nil
	}

	clientSessionData := [][]byte{}
	for _, v := range session.ClientSessions {
		data, err := json.Marshal(v)
		if err != nil {
			// not supposed to happen, mostly is program OOM, session data should be good, ignore
			klog.ErrorS(err, "Failed to marshar session object", "session", session.Identifier)
			return nil
		}
		clientSessionData = append(clientSessionData, data)
	}

	ns := grpc.SessionState{
		NodeRevision:      revision,
		SessionData:       sessionData,
		ClientSessionData: clientSessionData,
	}

	messageType := grpc.MessageType_SESSION_STATE
	return &grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &grpc.FornaxCoreMessage_SessionState{
			SessionState: &ns,
		},
	}
}
