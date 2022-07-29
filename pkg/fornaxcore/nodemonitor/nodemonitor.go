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
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/server"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/node"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	podutil "centaurusinfra.io/fornax-serverless/pkg/util"
	k8spodutil "k8s.io/kubernetes/pkg/api/v1/pod"

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
	klog.InfoS("A node is registering", "node", util.UniqueNodeName(v1node), "revision", message.GetNodeRegistry().NodeRevision)

	fornaxnode, err := nm.nodeManager.SetupNode(v1node, revision)
	if err != nil {
		klog.ErrorS(err, "Failed to setup node", "node", v1node)
		return nil, err
	}

	daemons := []*v1.Pod{}
	for _, v := range fornaxnode.DaemonPods {
		daemons = append(daemons, v.DeepCopy())
	}
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

	klog.InfoS("Setup node", "node", util.UniqueNodeName(fornaxnode.Node), "configuration", m)
	return m, nil
}

// OnNodeReady update node state, make node ready for schedule pod
func (nm *nodeMonitor) OnNodeReady(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	klog.InfoS("A node is ready",
		"node", util.UniqueNodeName(message.GetNodeReady().Node),
		"revision", message.GetNodeReady().NodeRevision,
		"pods", len(message.GetNodeReady().PodStates))

	_, err := nm.updateOrCreateNode(
		message.GetNodeReady().GetNode(),
		message.GetNodeReady().GetNodeRevision(),
		message.GetNodeReady().GetPodStates())
	if err != nil {
		klog.ErrorS(err, "Failed to sync a node", "node", util.UniqueNodeName(message.GetNodeReady().Node))
		if err == nodeagent.NodeRevisionOutOfOrderError {
			return server.NewFullSyncRequest(), nil
		}
		return nil, err
	}
	return nil, nil
}

// OnNodeStateUpdate sync node pods state
func (nm *nodeMonitor) OnNodeStateUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	klog.InfoS("Received a node state",
		"node", util.UniqueNodeName(message.GetNodeState().Node),
		"revision", message.GetNodeState().GetNodeRevision(),
		"pods", len(message.GetNodeState().PodStates))
	_, err := nm.updateOrCreateNode(
		message.GetNodeState().GetNode(),
		message.GetNodeState().GetNodeRevision(),
		message.GetNodeState().GetPodStates())
	if err != nil {
		klog.ErrorS(err, "Failed to sync a node", "node", util.UniqueNodeName(message.GetNodeState().Node))
		return nil, err
	}

	return nil, nil
}

// OnPodStateUpdate update single pod state
func (nm *nodeMonitor) OnPodStateUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	podState := message.GetPodState()
	klog.InfoS("Received a pod state",
		"pod", podutil.UniquePodName(podState.GetPod()),
		"state", podState.GetState(),
		"condition", k8spodutil.IsPodReady(podState.GetPod()),
		"node revision", podState.GetNodeRevision())

	err := nm.nodeManager.UpdatePodState(podState)
	if err != nil {
		klog.ErrorS(err, "Failed to update pod state", "pod", podState)
		return nil, err
	}
	return nil, nil
}

func (nm *nodeMonitor) updateOrCreateNode(v1node *v1.Node, revision int64, podStates []*grpc.PodState) (newNode *node.FornaxNodeWithState, err error) {
	if existingNode := nm.nodeManager.FindNode(v1node.GetName()); existingNode == nil {
		// somehow node already register with other fornax cores
		newNode, err = nm.nodeManager.CreateNode(v1node, revision)
		if err != nil {
			klog.ErrorS(err, "Failed to create a node", "node", v1node)
			return nil, err
		}
		nm.nodeManager.SyncNodePodStates(newNode.Node, podStates)
	} else {
		if existingNode.Revision > revision {
			klog.Warningf("Received a older revision of node, fornaxcore revision: %d, nodeagent revision: %d", existingNode.Revision, revision)
			return nil, nodeagent.NodeRevisionOutOfOrderError
		} else if existingNode.Revision == revision {
			// klog.Infof("Received a same revision from node agent, revision: %d, skip", revision)
			return existingNode, nil
		}

		newNode, err = nm.nodeManager.UpdateNode(v1node, revision)
		if err != nil {
			klog.ErrorS(err, "Failed to update a node", "node", v1node)
			return nil, err
		}
		nm.nodeManager.SyncNodePodStates(newNode.Node, podStates)
	}
	klog.InfoS("Node state and its pod states synced", "node", util.UniqueNodeName(newNode.Node))

	return newNode, nil
}

func NewNodeMonitor(nodeManager node.NodeManager) *nodeMonitor {
	nm := &nodeMonitor{
		chQuit:      make(chan interface{}),
		nodeManager: nodeManager,
	}

	return nm
}
