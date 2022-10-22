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

package node

import (
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod"
)

func BuildFornaxGrpcNodeState(node *FornaxNode, revision int64) *grpc.FornaxCoreMessage {
	podStates := []*grpc.PodState{}
	for _, v := range node.Pods.List() {
		s := pod.BuildFornaxcoreGrpcPodState(revision, v)
		podStates = append(podStates, s.GetPodState())
	}

	ns := grpc.NodeState{
		NodeRevision: revision,
		Node:         node.V1Node,
		PodStates:    podStates,
	}

	messageType := grpc.MessageType_NODE_STATE
	return &grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &grpc.FornaxCoreMessage_NodeState{
			NodeState: &ns,
		},
	}
}

func BuildFornaxGrpcNodeReady(node *FornaxNode, revision int64) *grpc.FornaxCoreMessage {
	podStates := []*grpc.PodState{}
	for _, v := range node.Pods.List() {
		s := pod.BuildFornaxcoreGrpcPodState(revision, v)
		podStates = append(podStates, s.GetPodState())
	}
	ns := grpc.NodeReady{
		NodeRevision:  revision,
		Node:          node.V1Node,
		PodStates:     podStates,
		SessionStates: []*grpc.SessionState{},
	}

	messageType := grpc.MessageType_NODE_READY
	return &grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &grpc.FornaxCoreMessage_NodeReady{
			NodeReady: &ns,
		},
	}
}
