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

package nodemonitor

import (
	"context"

	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/server"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/node"
	podutil "centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var _ server.NodeMonitor = &nodeMonitor{}

type nodeMonitor struct {
	chQuit      chan interface{}
	nodeManager node.NodeManager
}

// OnRegistry setup a new node, send a a node configruation back to node for initialization,
// node will send back node ready message after node configruation finished
func (nm *nodeMonitor) OnRegistry(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	v1node := message.GetNodeRegistry().GetNode().DeepCopy()
	revision := message.GetNodeRegistry().GetNodeRevision()
	klog.InfoS("A node is registering", "node", v1node)

	fornaxnode, err := nm.nodeManager.SetupNode(v1node, revision)
	daemons := []*v1.Pod{}
	for _, v := range fornaxnode.DaemonPods {
		daemons = append(daemons, v.DeepCopy())
	}
	if err == nil {
		// update node, send node configuration
		domain := default_config.DefaultDomainName
		nodeConig := grpc.FornaxCoreMessage_NodeConfiguration{
			NodeConfiguration: &grpc.NodeConfiguration{
				ClusterDomain: &domain,
				Node:          fornaxnode.Node.DeepCopy(),
				DaemonPods:    daemons,
			},
		}
		messageType := grpc.MessageType_NODE_CONFIGURATION
		m := &grpc.FornaxCoreMessage{
			MessageType: &messageType,
			MessageBody: &nodeConig,
		}

		klog.InfoS("Setup node", "node configuration", m)
		return m, nil
	} else {
		klog.ErrorS(err, "Failed to setup node", "node", v1node)
	}

	return nil, nil
}

// OnReady create a new node or update node, and put node in ready state, scheduler begin to schedule pod in ready node
func (nm *nodeMonitor) OnReady(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	v1node := message.GetNodeReady().GetNode()
	revision := message.GetNodeReady().GetNodeRevision()
	klog.InfoS("A node is ready for taking pods", "node", v1node)

	_, err := nm.updateOrCreateNode(revision, v1node)
	if err != nil {
		klog.ErrorS(err, "Failed to update a node", "node", v1node)
		if err == node.NodeRevisionOutofOrderError {
			return newFullSyncRequest(), nil
		}
		return nil, err
	}

	// ValidateNodeReadyState(newNode)
	return nil, nil
}

// OnNodeUpdate update node state using message from node agent
func (nm *nodeMonitor) OnNodeUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	// update v1node resource info
	v1node := message.GetNodeState().GetNode()
	revision := message.GetNodeState().GetNodeRevision()
	fornaxnode, err := nm.updateOrCreateNode(revision, v1node)
	if err != nil {
		klog.ErrorS(err, "Failed to sync a node", "node", v1node)
		if err == node.NodeRevisionOutofOrderError {
			return newFullSyncRequest(), nil
		}
		return nil, err
	}

	err = nm.nodeManager.SyncNodePodStates(fornaxnode, revision, message.GetNodeState().GetPodStates())
	if err != nil {
		klog.ErrorS(err, "Failed to sync node pods state", "node", v1node)
		return nil, err
	}
	return nil, nil
}

// OnPodUpdate update single pod state
func (nm *nodeMonitor) OnPodUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	podState := message.GetPodState()
	klog.InfoS("Received a pod state", "pod", podutil.UniquePodName(podState.GetPod()), "state", podState.GetState())

	err := nm.nodeManager.UpdatePodState(podState)
	if err != nil {
		klog.ErrorS(err, "Failed to update pod state", "pod", podState)
		return nil, err
	}
	return nil, nil
}

func (nm *nodeMonitor) updateOrCreateNode(revision int64, v1node *v1.Node) (newNode *node.FornaxNodeWithState, err error) {
	if existingNode := nm.nodeManager.FindNode(v1node.GetName()); existingNode == nil {
		// somehow node already register with other fornax cores
		newNode, err = nm.nodeManager.CreateNode(v1node, revision)
		if err != nil {
			klog.ErrorS(err, "Failed to create a node", "node", v1node)
			return nil, err
		}
	} else {
		newNode, err = nm.nodeManager.UpdateNode(v1node, revision)
		if err != nil {
			return nil, err
		}
	}

	return newNode, nil
}

// nodeManager: node.NewNodeManager(node.DefaultStaleNodeTimeout, dispatcher*server.MessageDispatcher),
func NewNodeMonitor(nodeManager node.NodeManager) *nodeMonitor {
	nm := &nodeMonitor{
		chQuit:      make(chan interface{}),
		nodeManager: nodeManager,
	}

	return nm
}

func newFullSyncRequest() *grpc.FornaxCoreMessage {
	msg := grpc.FornaxCoreMessage_NodeFullSync{
		NodeFullSync: &grpc.NodeFullSync{},
	}
	messageType := grpc.MessageType_NODE_FULL_SYNC
	return &grpc.FornaxCoreMessage{
		MessageType: &messageType,
		MessageBody: &msg,
	}
}
