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
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/collection"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"

	v1 "k8s.io/api/core/v1"
)

type PodManagerInterface interface {
	AddOrUpdatePod(pod *v1.Pod) (*v1.Pod, error)
	DeletePod(pod *v1.Pod) (*v1.Pod, error)
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
	Revision   string
	Node       *v1.Node
	State      NodeWorkingState
	Pods       *collection.ConcurrentStringSet
	DaemonPods map[string]*v1.Pod
	LastSeen   time.Time
}

type NodeManagerInterface interface {
	NodeInfoLWInterface
	UpdateNodeState(nodeId string, node *v1.Node) (*FornaxNodeWithState, error)
	UpdatePodState(nodeId string, pod *v1.Pod) error
	SyncPodStates(nodeId string, podStates []*grpc.PodState)
	DisconnectNode(nodeId string) error
}

// NodeInfoLWInterface provide method to watch and list NodeEvent
type NodeInfoLWInterface interface {
	List() []*NodeEvent
	Watch(watcher chan<- *NodeEvent)
}

// PodInfoLWInterface provide method to watch and list PodEvent
type PodInfoLWInterface interface {
	Watch(watcher chan<- *PodEvent)
}

// NodeMonitorInterface handle message sent by node agent
type NodeMonitorInterface interface {
	OnNodeConnect(nodeId string) error
	OnNodeDisconnect(nodeId string) error
	OnNodeRegistry(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnNodeReady(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnNodeStateUpdate(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnPodStateUpdate(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
}
