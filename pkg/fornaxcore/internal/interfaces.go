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

package internal

import (
	"context"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/collection"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	fornaxstore "centaurusinfra.io/fornax-serverless/pkg/store"

	v1 "k8s.io/api/core/v1"
)

type PodManagerInterface interface {
	AddPod(nodeId string, pod *v1.Pod) (*v1.Pod, error)
	DeletePod(nodeId string, pod *v1.Pod) (*v1.Pod, error)
	TerminatePod(podName string) error
	HibernatePod(podName string) error
	FindPod(podName string) *v1.Pod
	Watch(watcher chan<- *PodEvent)
}

type NodeWorkingState string

const (
	NodeWorkingStateRegistering  NodeWorkingState = "registering"
	NodeWorkingStateDisconnected NodeWorkingState = "disconnected"
	NodeWorkingStateRunning      NodeWorkingState = "running"
)

type FornaxNodeWithState struct {
	NodeId     string
	Node       *v1.Node
	State      NodeWorkingState
	Pods       *collection.ConcurrentStringSet
	DaemonPods map[string]*v1.Pod
	LastSeen   time.Time
}

type NodeManagerInterface interface {
	NodeInfoProviderInterface
	UpdateSessionState(nodeId string, session *fornaxv1.ApplicationSession) error
	UpdatePodState(nodeId string, pod *v1.Pod, sessions []*fornaxv1.ApplicationSession) error
	SyncNodePodStates(nodeId string, podStates []*grpc.PodState)
	DisconnectNode(nodeId string) error
	FindNode(name string) *FornaxNodeWithState
	CreateNode(nodeId string, node *v1.Node) (*FornaxNodeWithState, error)
	UpdateNode(nodeId string, node *v1.Node) (*FornaxNodeWithState, error)
	SetupNode(nodeId string, node *v1.Node) (*FornaxNodeWithState, error)
}

// SessionManagerInterface work as a bridge between node agent and fornax core, it call nodeagent to open/close a session
// and update session status using session state reported back from node agent
type SessionManagerInterface interface {
	UpdateSessionStatus(session *fornaxv1.ApplicationSession, newStatus *fornaxv1.ApplicationSessionStatus) error
	OnSessionStatusFromNode(nodeId string, pod *v1.Pod, session *fornaxv1.ApplicationSession) error
	OpenSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error
	CloseSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error
	Watch(ctx context.Context) (<-chan fornaxstore.WatchEventWithOldObj, error)
}

// NodeInfoProviderInterface provide method to watch and list NodeEvent
type NodeInfoProviderInterface interface {
	List() []*NodeEvent
	Watch(watcher chan<- *NodeEvent)
}

// PodInfoProviderInterface provide method to watch and list PodEvent
type PodInfoProviderInterface interface {
	Watch(watcher chan<- *PodEvent)
}

// NodeMonitorInterface handle message sent by node agent
type NodeMonitorInterface interface {
	OnNodeConnect(nodeId string) error
	OnNodeDisconnect(nodeId string) error
	OnRegistry(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnNodeReady(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnNodeStateUpdate(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnPodStateUpdate(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnSessionUpdate(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
}
