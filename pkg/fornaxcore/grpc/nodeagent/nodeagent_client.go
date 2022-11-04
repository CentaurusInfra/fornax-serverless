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

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"

	v1 "k8s.io/api/core/v1"
)

var (
	NodeRevisionOutOfOrderError        = errors.New("revision out of order between fornaxcore and node")
	NodeNotFoundError                  = errors.New("node not found")
	NodeAlreadyExistError              = errors.New("node already exist")
	ObjectMissingNodeLabelError        = errors.New("does not have fornax node label")
	ObjectMissingPodLabelError         = errors.New("does not have fornax pod label")
	ObjectMissingApplicationLabelError = errors.New("does not have fornax application label")
)

type MessageDispatcher interface {
	DispatchNodeMessage(nodeId string, message *grpc.FornaxCoreMessage) error
}
type NodeAgentClient interface {
	MessageDispatcher
	FullSyncNode(nodeId string) error
	CreatePod(nodeId string, pod *v1.Pod) error
	TerminatePod(nodeId string, pod *v1.Pod) error
	HibernatePod(nodeId string, pod *v1.Pod) error
	OpenSession(nodeId string, pod *v1.Pod, session *fornaxv1.ApplicationSession) error
	CloseSession(nodeId string, pod *v1.Pod, session *fornaxv1.ApplicationSession) error
}
