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
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/session"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

func BuildFornaxcoreGrpcPodStateForTerminatedPod(nodeRevision int64, pod *fornaxtypes.FornaxPod) *grpc.FornaxCoreMessage {
	state := grpc.PodState_Terminated
	s := grpc.PodState{
		NodeRevision: nodeRevision,
		State:        state,
		Pod:          pod.Pod.DeepCopy(),
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
	sessionStates := []*grpc.SessionState{}
	for _, v := range pod.Sessions {
		s := session.BuildFornaxcoreGrpcSessionState(nodeRevision, v)
		sessionStates = append(sessionStates, s.GetSessionState())
	}
	s := grpc.PodState{
		NodeRevision: nodeRevision,
		State:        PodStateToFornaxState(pod),
		Pod:          pod.Pod.DeepCopy(),
		// TODO
		Resource:      &grpc.PodResource{},
		SessionStates: sessionStates,
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
	case types.PodStateTerminating:
		grpcState = grpc.PodState_Terminating
	case types.PodStateTerminated:
		grpcState = grpc.PodState_Terminated
	case types.PodStateFailed:
		// check container runtime state
		allContainerTerminated := true
		for _, v := range pod.Containers {
			allContainerTerminated = allContainerTerminated && v.ContainerStatus != nil && v.ContainerStatus.RuntimeStatus != nil && v.ContainerStatus.RuntimeStatus.FinishedAt != 0 && v.ContainerStatus.RuntimeStatus.State == criv1.ContainerState_CONTAINER_EXITED
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
