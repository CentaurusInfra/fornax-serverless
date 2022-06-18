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

func BuildFornaxGrpcNodeState(node FornaxNode) grpc.NodeState {
	podStates := []*grpc.PodState{}
	// sessionStates := []*grpc.SessionState{}
	for _, v := range node.Pods {
		s := pod.BuildFornaxcoreGrpcPodState(v)
		podStates = append(podStates, s)
	}
	return grpc.NodeState{
		NodeIp:    &node.NodeConfig.NodeIP,
		Node:      node.V1Node,
		PodStates: podStates,
		// TODO, add sessionStates
		// SessionStates: sessionStates,
	}
}
