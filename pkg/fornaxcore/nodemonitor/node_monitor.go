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
	"encoding/json"
	"sync"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	podutil "centaurusinfra.io/fornax-serverless/pkg/util"
	k8spodutil "k8s.io/kubernetes/pkg/api/v1/pod"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var _ ie.NodeMonitorInterface = &nodeMonitor{}

type NodeWithRevision struct {
	NodeIdentifier string
	Revision       int64
}

type NodeRevisionPool struct {
	mu    sync.RWMutex
	nodes map[string]*NodeWithRevision
}

func (pool *NodeRevisionPool) add(name string, node *NodeWithRevision) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.nodes[name] = node
}

func (pool *NodeRevisionPool) delete(name string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.nodes, name)
}

func (pool *NodeRevisionPool) get(name string) *NodeWithRevision {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if n, found := pool.nodes[name]; found {
		return n
	}

	return nil
}

type nodeMonitor struct {
	chQuit      chan interface{}
	nodeManager ie.NodeManagerInterface
	nodes       NodeRevisionPool
	staleNodes  NodeRevisionPool
}

// OnSessionUpdate implements server.NodeMonitor
func (nm *nodeMonitor) OnSessionUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	sessionState := message.GetSessionState()
	revision := sessionState.GetNodeRevision()

	session := &fornaxv1.ApplicationSession{}
	if err := json.Unmarshal(sessionState.GetSessionData(), session); err != nil {
		klog.ErrorS(err, "Malformed SessionData, is not a valid fornaxv1.ApplicationSession object")
		return nil, err
	}

	klog.InfoS("Received a session state", "session", util.Name(session), "node revision", revision, "status", session.Status)
	nodeId := message.GetNodeIdentifier().GetIdentifier()
	nodeWS := nm.nodes.get(nodeId)
	if nodeWS == nil {
		return nil, nodeagent.NodeRevisionOutOfOrderError
	}
	if err := nm.checkNodeRevision(nodeWS, revision); err != nil {
		return nil, err
	}
	if revision == nodeWS.Revision {
		// klog.Infof("Received a same revision from node agent, revision: %d, skip", revision)
		return nil, nil
	}

	nodeWS.Revision = revision
	err := nm.nodeManager.UpdateSessionState(nodeId, session)
	if err != nil {
		klog.ErrorS(err, "Failed to update pod state", "pod", sessionState)
		return nil, err
	}

	return nil, nil
}

// OnRegistry setup a new node, send a a node configruation back to node for initialization,
// node will send back node ready message after node configruation finished
func (nm *nodeMonitor) OnRegistry(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	v1node := message.GetNodeRegistry().GetNode().DeepCopy()
	nodeId := message.GetNodeIdentifier().GetIdentifier()
	revision := message.GetNodeRegistry().GetNodeRevision()
	klog.InfoS("A node is registering", "node", nodeId, "revision", revision)

	// on node register, we reset revision
	if nodeWS := nm.nodes.get(nodeId); nodeWS == nil {
		nm.nodes.add(nodeId, &NodeWithRevision{
			NodeIdentifier: nodeId,
			Revision:       revision,
		})
	} else {
		currRevision := nodeWS.Revision
		if revision < currRevision {
			klog.Warningf("Node is registering with a older revision, it may crash or restart as a new node, %s", nodeId)
		}
		nodeWS.Revision = revision
	}

	fornaxnode, err := nm.nodeManager.SetupNode(nodeId, v1node)
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
			ClusterDomain: domain,
			Node:          fornaxnode.Node.DeepCopy(),
			DaemonPods:    daemons,
		},
	}
	messageType := grpc.MessageType_NODE_CONFIGURATION
	m := &grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &nodeConig,
	}

	klog.InfoS("Setup node", "node", nodeId, "configuration", m)
	return m, nil
}

// OnNodeConnect update node state, make node ready for schedule pod
func (nm *nodeMonitor) OnNodeConnect(nodeId string) error {
	klog.InfoS("A node connected to FornaxCore", "node", nodeId)
	nodeWS := nm.staleNodes.get(nodeId)
	if nodeWS != nil {
		nm.staleNodes.delete(nodeId)
		// ask node to full sync since this node was disconnected before
		return nodeagent.NodeRevisionOutOfOrderError
	}
	return nil
}

// OnNodeDisconnect update node state, make node ready for schedule pod
func (nm *nodeMonitor) OnNodeDisconnect(nodeId string) error {
	klog.InfoS("A node disconnected from FornaxCore", "node", nodeId)
	nodeWS := nm.nodes.get(nodeId)
	if nodeWS != nil {
		nm.staleNodes.add(nodeId, nodeWS)
	} else {
		nm.staleNodes.add(nodeId, &NodeWithRevision{
			NodeIdentifier: nodeId,
			Revision:       0,
		})
	}
	nm.nodeManager.DisconnectNode(nodeId)
	return nil
}

// OnNodeReady update node state, make node ready for schedule pod
func (nm *nodeMonitor) OnNodeReady(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	nodeReady := message.GetNodeReady()
	nodeId := message.GetNodeIdentifier().GetIdentifier()
	klog.InfoS("A node is ready", "node", nodeId, "revision", nodeReady.GetNodeRevision(), "pods", len(nodeReady.PodStates))

	err := nm.updateOrCreateNode(nodeId, nodeReady.GetNode(), nodeReady.GetNodeRevision(), nodeReady.GetPodStates())
	if err != nil {
		klog.ErrorS(err, "Failed to sync a node", "node", nodeId)
		return nil, err
	}
	return nil, nil
}

// OnNodeStateUpdate sync node pods state
func (nm *nodeMonitor) OnNodeStateUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	nodeState := message.GetNodeState()
	nodeId := message.GetNodeIdentifier().GetIdentifier()
	klog.InfoS("Received a node state", "node", nodeId, "revision", nodeState.GetNodeRevision(), "pods", len(nodeState.GetPodStates()))
	err := nm.updateOrCreateNode(nodeId, nodeState.GetNode(), nodeState.GetNodeRevision(), nodeState.GetPodStates())
	if err != nil {
		klog.ErrorS(err, "Failed to sync a node", "node", nodeId)
		return nil, err
	}

	return nil, nil
}

// OnPodStateUpdate update single pod state
func (nm *nodeMonitor) OnPodStateUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	podState := message.GetPodState()
	revision := podState.GetNodeRevision()
	klog.InfoS("Received a pod state",
		"pod", podutil.Name(podState.GetPod()),
		"fornax state", podState.GetState(),
		"pod phase", podState.GetPod().Status.Phase,
		"condition", k8spodutil.IsPodReady(podState.GetPod()),
		"node revision", revision)

	_, found := podState.GetPod().Labels[fornaxv1.LabelFornaxCoreNode]
	if !found {
		klog.Errorf("Pod miss node label: %s", fornaxv1.LabelFornaxCoreNode)
		return nil, nodeagent.ObjectMissingNodeLabelError
	}

	nodeIdentifier := message.GetNodeIdentifier()
	nodeWS := nm.nodes.get(nodeIdentifier.GetIdentifier())
	if nodeWS == nil {
		return nil, nodeagent.NodeRevisionOutOfOrderError
	}
	if err := nm.checkNodeRevision(nodeWS, revision); err != nil {
		return nil, err
	}
	if revision == nodeWS.Revision {
		klog.InfoS("Received a same revision from node agent of pod, skip", "pod", podState.Pod.Name)
		return nil, nil
	}

	// in incremental order
	sessions := []*fornaxv1.ApplicationSession{}
	for _, v := range podState.GetSessionStates() {
		session := &fornaxv1.ApplicationSession{}
		if err := json.Unmarshal(v.SessionData, session); err == nil {
			sessions = append(sessions, session)
		}
	}

	nodeWS.Revision = revision
	err := nm.nodeManager.UpdatePodState(nodeIdentifier.GetIdentifier(), podState.GetPod().DeepCopy(), sessions)
	if err != nil {
		klog.ErrorS(err, "Failed to update pod state", "pod", podState)
		return nil, err
	}
	return nil, nil
}

func (nm *nodeMonitor) checkNodeRevision(nodeWS *NodeWithRevision, revision int64) error {
	if nodeWS == nil {
		return nodeagent.NodeRevisionOutOfOrderError
	}

	currRevision := nodeWS.Revision
	if revision == currRevision+1 || revision == currRevision {
	} else if revision == currRevision {
		// klog.Infof("Received a same revision from node agent, revision: %d, skip", revision)
	} else {
		klog.Warningf("Received a disordred revision from node agent, fornax core revision: %d, received revision: %d", currRevision, revision)
		return nodeagent.NodeRevisionOutOfOrderError
	}
	return nil
}

func (nm *nodeMonitor) updateOrCreateNode(nodeId string, v1node *v1.Node, revision int64, podStates []*grpc.PodState) error {
	if nodeWS := nm.nodes.get(nodeId); nodeWS == nil {
		nm.nodes.add(nodeId, &NodeWithRevision{
			NodeIdentifier: nodeId,
			Revision:       revision,
		})
		// somehow node already register with other fornax cores
		_, err := nm.nodeManager.CreateNode(nodeId, v1node)
		if err != nil {
			klog.ErrorS(err, "Failed to create a node", "node", v1node)
			return err
		}
		nm.nodeManager.SyncNodePodStates(nodeId, podStates)
	} else {
		if revision < nodeWS.Revision {
			return nodeagent.NodeRevisionOutOfOrderError
		}

		nodeWS.Revision = revision
		_, err := nm.nodeManager.UpdateNode(nodeId, v1node)
		if err != nil {
			klog.ErrorS(err, "Failed to update a node", "node", v1node)
			return err
		}
		nm.nodeManager.SyncNodePodStates(nodeId, podStates)
	}

	return nil
}

// nm.staleNodeBucket.refreshNode(fornaxNode)
// nm.staleNodeBucket.refreshNode(nodeWS)
func (nm *nodeMonitor) CheckStaleNode() {
	// ask nodes to send full sync request
	// for _, node := range nm.staleNodes {
	// nm.FullSyncNode(util.Name(node.Node))
	// }
}

// nm.nodeUpdateBucket.appendUpdate(update)
// case <-nm.houseKeepingTicker.C:
//   nm.PrintNodeSummary()
//   nm.CheckStaleNode()
func NewNodeMonitor(nodeManager ie.NodeManagerInterface) *nodeMonitor {
	nm := &nodeMonitor{
		chQuit:      make(chan interface{}),
		nodeManager: nodeManager,
		nodes: NodeRevisionPool{
			mu:    sync.RWMutex{},
			nodes: map[string]*NodeWithRevision{},
		},
		staleNodes: NodeRevisionPool{
			mu:    sync.RWMutex{},
			nodes: map[string]*NodeWithRevision{},
		},
	}

	return nm
}
