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
	v1 "k8s.io/api/core/v1"
)

type PodManagerInterface interface {
	AddPod(nodeId string, pod *v1.Pod) (*v1.Pod, error)
	DeletePod(nodeId string, pod *v1.Pod) (*v1.Pod, error)
	TerminatePod(pod *v1.Pod) error
	FindPod(identifier string) *v1.Pod
	Watch(watcher chan<- interface{})
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
	NodeInfoProvider
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
	UpdateSessionFinalizer(session *fornaxv1.ApplicationSession) error
	UpdateSessionStatusFromNode(nodeId string, pod *v1.Pod, sessions []*fornaxv1.ApplicationSession)
	OpenSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error
	CloseSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error
	Watch(watcher chan<- interface{})
}

// NodeInfoProvider provide method to watch and list NodeEvent
type NodeInfoProvider interface {
	List() []*NodeEvent
	Watch(watcher chan<- interface{})
}

// PodInfoProvider provide method to watch and list PodEvent
type PodInfoProvider interface {
	Watch(watcher chan<- interface{})
}

// NodeMonitorInterface handle message sent by node agent
type NodeMonitorInterface interface {
	OnNodeConnect(nodeId string) error
	OnNodeDisconnect(nodeId string) error
	OnRegistry(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnNodeReady(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnNodeStateUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnPodStateUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnSessionUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
}
