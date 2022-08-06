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

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
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
	klog.InfoS("A node is registering", "node", util.UniqueNodeName(v1node), "revision", message.GetNodeRegistry().GetNodeRevision())

	// currRevision := oldcopy.Revision
	// if revision < currRevision {
	//  klog.Warningf("Node is registering with a older revision, fornax core revision: %d, received revision: %d", currRevision, revision)
	// }
	//

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
	nodeReady := message.GetNodeReady()
	klog.InfoS("A node is ready",
		"node", util.UniqueNodeName(nodeReady.Node),
		"revision", nodeReady.GetNodeRevision(),
		"pods", len(nodeReady.PodStates))

	_, err := nm.updateOrCreateNode(nodeReady.GetNode(), nodeReady.GetNodeRevision(), nodeReady.GetPodStates())
	if err != nil {
		klog.ErrorS(err, "Failed to sync a node", "node", util.UniqueNodeName(nodeReady.Node))
		if err == nodeagent.NodeRevisionOutOfOrderError {
			return server.NewFullSyncRequest(), nil
		}
		return nil, err
	}
	return nil, nil
}

// OnNodeStateUpdate sync node pods state
func (nm *nodeMonitor) OnNodeStateUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	nodeState := message.GetNodeState()
	klog.InfoS("Received a node state",
		"node", util.UniqueNodeName(nodeState.GetNode()),
		"revision", nodeState.GetNodeRevision(),
		"pods", len(nodeState.GetPodStates()))
	_, err := nm.updateOrCreateNode(nodeState.GetNode(), nodeState.GetNodeRevision(), nodeState.GetPodStates())
	if err != nil {
		klog.ErrorS(err, "Failed to sync a node", "node", util.UniqueNodeName(message.GetNodeState().Node))
		return nil, err
	}

	return nil, nil
}

// OnPodStateUpdate update single pod state
func (nm *nodeMonitor) OnPodStateUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	podState := message.GetPodState()
	revision := podState.GetNodeRevision()
	klog.InfoS("Received a pod state",
		"pod", podutil.UniquePodName(podState.GetPod()),
		"fornax state", podState.GetState(),
		"pod phase", podState.GetPod().Status.Phase,
		"condition", k8spodutil.IsPodReady(podState.GetPod()),
		"node revision", revision)

	nodeIdentifier, found := podState.GetPod().Labels[fornaxv1.LabelFornaxCoreNode]
	if !found {
		klog.Errorf("Pod miss node label: %s", fornaxv1.LabelFornaxCoreNode)
		return nil, nodeagent.PodMissingNodeLabelError
	}
	nodeWS := nm.nodeManager.FindNode(nodeIdentifier)
	if nodeWS == nil {
		return nil, nodeagent.NodeRevisionOutOfOrderError
	}

	currRevision := nodeWS.Revision
	if revision == currRevision+1 {
		// in incremental order
		err := nm.nodeManager.UpdatePodState(podState.GetPod().DeepCopy(), revision)
		if err != nil {
			klog.ErrorS(err, "Failed to update pod state", "pod", podState)
			return nil, err
		}
	} else if revision == currRevision {
		// klog.Infof("Received a same revision from node agent, revision: %d, skip", revision)
	} else {
		klog.Warningf("Received a disordred revision from node agent, fornax core revision: %d, received revision: %d", currRevision, revision)
		return nil, nodeagent.NodeRevisionOutOfOrderError
	}
	return nil, nil
}

func (nm *nodeMonitor) updateOrCreateNode(v1node *v1.Node, revision int64, podStates []*grpc.PodState) (newNode *node.FornaxNodeWithState, err error) {
	if nodeWS := nm.nodeManager.FindNode(util.UniqueNodeName(v1node)); nodeWS == nil {
		// somehow node already register with other fornax cores
		newNode, err = nm.nodeManager.CreateNode(v1node, revision)
		if err != nil {
			klog.ErrorS(err, "Failed to create a node", "node", v1node)
			return nil, err
		}
		nm.nodeManager.SyncNodePodStates(newNode.Node, podStates)
	} else {
		if nodeWS.Revision > revision {
			klog.Warningf("Received a older revision of node, fornaxcore revision: %d, node revision: %d", nodeWS.Revision, revision)
			return nil, nodeagent.NodeRevisionOutOfOrderError
		} else if nodeWS.Revision == revision {
			// klog.Infof("Received a same revision from node agent, revision: %d, skip", revision)
			return nodeWS, nil
		}

		newNode, err = nm.nodeManager.UpdateNode(v1node, revision)
		if err != nil {
			klog.ErrorS(err, "Failed to update a node", "node", v1node)
			return nil, err
		}
		nm.nodeManager.SyncNodePodStates(newNode.Node, podStates)
	}
	klog.InfoS("Node state and its pod states synced", "node", util.UniqueNodeName(newNode.Node), "revision", revision)

	return newNode, nil
}

func NewNodeMonitor(nodeManager node.NodeManager) *nodeMonitor {
	nm := &nodeMonitor{
		chQuit:      make(chan interface{}),
		nodeManager: nodeManager,
	}

	return nm
}
