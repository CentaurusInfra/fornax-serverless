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

package pod

import (
	"strings"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/session"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
)

func BuildFornaxcoreGrpcPodStateForFailedPod(nodeRevision int64, pod *v1.Pod) *grpc.FornaxCoreMessage {
	state := grpc.PodState_Terminated
	s := grpc.PodState{
		NodeRevision: nodeRevision,
		State:        state,
		Pod:          pod.DeepCopy(),
		Resource:     &grpc.PodResource{},
	}
	messageType := grpc.MessageType_POD_STATE
	return &grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &grpc.FornaxCoreMessage_PodState{
			PodState: &s,
		},
	}
}

func BuildFornaxcoreGrpcPodState(nodeRevision int64, pod *fornaxtypes.FornaxPod) *grpc.FornaxCoreMessage {
	sessionLables := []string{}
	sessionStates := []*grpc.SessionState{}
	// pod only report sessions currently bundle on it, session termninated state is supposed by reported already
	// fornax core use session states in pod state to delete sessions which is not found
	for _, sess := range pod.Sessions {
		if !util.SessionInTerminalState(sess.Session) {
			s := session.BuildFornaxcoreGrpcSessionState(nodeRevision, sess)
			sessionStates = append(sessionStates, s.GetSessionState())
			sessionLables = append(sessionLables, util.Name(sess.Session))
		}
	}

	podWithSession := pod.Pod.DeepCopy()
	labels := podWithSession.GetLabels()
	if len(sessionLables) > 0 {
		labels[fornaxv1.LabelFornaxCoreApplicationSession] = strings.Join(sessionLables, ",")
	} else {
		delete(labels, fornaxv1.LabelFornaxCoreApplicationSession)
	}
	podWithSession.Labels = labels

	s := grpc.PodState{
		NodeRevision:  nodeRevision,
		State:         PodStateToFornaxState(pod),
		Pod:           podWithSession,
		SessionStates: sessionStates,
		// TODO
		Resource: &grpc.PodResource{},
	}
	messageType := grpc.MessageType_POD_STATE
	return &grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &grpc.FornaxCoreMessage_PodState{
			PodState: &s,
		},
	}
}

func PodStateToFornaxState(pod *fornaxtypes.FornaxPod) grpc.PodState_State {
	var grpcState grpc.PodState_State
	switch pod.FornaxPodState {
	case types.PodStateCreating:
		grpcState = grpc.PodState_Creating
	case types.PodStateCreated:
		grpcState = grpc.PodState_Standby
	case types.PodStateRunning:
		grpcState = grpc.PodState_Running
	case types.PodStateHibernated:
		grpcState = grpc.PodState_Running
	case types.PodStateTerminating:
		grpcState = grpc.PodState_Terminating
	case types.PodStateTerminated:
		grpcState = grpc.PodState_Terminated
	case types.PodStateCleanup:
		grpcState = grpc.PodState_Terminated
	case types.PodStateFailed:
		// check container runtime state
		allContainerTerminated := true
		for _, v := range pod.Containers {
			allContainerTerminated = allContainerTerminated && (v.ContainerStatus == nil || runtime.ContainerExit(v.ContainerStatus))
		}
		if allContainerTerminated {
			grpcState = grpc.PodState_Terminated
		} else {
			grpcState = grpc.PodState_Terminating
		}
	default:
		grpcState = grpc.PodState_Creating
	}

	return grpcState
}
