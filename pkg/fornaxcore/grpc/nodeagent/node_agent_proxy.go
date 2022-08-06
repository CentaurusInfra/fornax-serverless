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

package nodeagent

import (
	"errors"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	v1 "k8s.io/api/core/v1"
)

var (
	NodeRevisionOutOfOrderError = errors.New("revision out of order between fornaxcore and node")
	NodeNotFoundError           = errors.New("node not found")
	NodeAlreadyExistError       = errors.New("node already exist")
	PodMissingNodeLabelError    = errors.New("pod does not have fornax node label")
)

type MessageDispatcher interface {
	DispatchMessage(node string, message *grpc.FornaxCoreMessage) error
}
type NodeAgentProxy interface {
	MessageDispatcher
	CreatePod(nodeIdentifier string, pod *v1.Pod) error
	TerminatePod(nodeIdentifier string, pod *v1.Pod) error
	FullSyncNode(nodeIdentifier string) error
}
