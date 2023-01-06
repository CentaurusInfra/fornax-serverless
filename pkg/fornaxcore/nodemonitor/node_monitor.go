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
	"encoding/json"
	"fmt"
	"sync"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	// k8spodutil "k8s.io/kubernetes/pkg/api/v1/pod"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var _ ie.NodeMonitorInterface = &nodeMonitor{}

type NodeWithRevision struct {
	NodeId   string
	Revision int64
}

type NodeRevisionMap struct {
	mu    sync.RWMutex
	nodes map[string]*NodeWithRevision
}

func (pool *NodeRevisionMap) add(name string, node *NodeWithRevision) {
	pool.mu.Lock()
	pool.nodes[name] = node
	pool.mu.Unlock()
}

func (pool *NodeRevisionMap) delete(name string) {
	pool.mu.Lock()
	delete(pool.nodes, name)
	pool.mu.Unlock()
}

func (pool *NodeRevisionMap) get(name string) *NodeWithRevision {
	pool.mu.RLock()
	if n, found := pool.nodes[name]; found {
		pool.mu.RUnlock()
		return n
	}

	pool.mu.RUnlock()
	return nil
}

type nodeMonitor struct {
	chQuit      chan interface{}
	nodeManager ie.NodeManagerInterface
	nodes       NodeRevisionMap
	staleNodes  NodeRevisionMap
}

// OnSessionUpdate implements server.NodeMonitor
func (nm *nodeMonitor) OnSessionUpdate(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	sessionState := message.GetSessionState()
	revision := sessionState.GetNodeRevision()
	nodeId := message.GetNodeIdentifier().GetIdentifier()
	session := &fornaxv1.ApplicationSession{}
	if err := json.Unmarshal(sessionState.GetSessionData(), session); err != nil {
		klog.ErrorS(err, "Malformed SessionData, is not a valid fornaxv1.ApplicationSession object")
		return nil, err
	}
	nodeWRev := nm.nodes.get(nodeId)
	if nodeWRev == nil {
		return nil, nodeagent.NodeRevisionOutOfOrderError
	}
	if err := nm.validateNodeRevision(nodeWRev, revision); err != nil {
		return nil, err
	}
	if revision == nodeWRev.Revision {
		klog.InfoS("Received a session with same revision of current node revision, continue to handle single session state", "session", util.Name(session))
	}

	nodeWRev.Revision = revision
	err := nm.nodeManager.UpdateSessionState(nodeId, session)
	if err != nil {
		klog.ErrorS(err, "Failed to update pod state", "pod", sessionState)
		return nil, err
	}

	return nil, nil
}

// OnRegistry setup a new node, send a a node configruation back to node for initialization,
// node will send back node ready message after node configruation finished
func (nm *nodeMonitor) OnNodeRegistry(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	v1node := message.GetNodeRegistry().GetNode().DeepCopy()
	nodeId := message.GetNodeIdentifier().GetIdentifier()
	revision := message.GetNodeRegistry().GetNodeRevision()
	klog.InfoS("A node is registering", "node", nodeId, "revision", revision)

	// on node register, we reset revision
	if nodeWRev := nm.nodes.get(nodeId); nodeWRev == nil {
		nm.nodes.add(nodeId, &NodeWithRevision{
			NodeId:   nodeId,
			Revision: revision,
		})
	} else {
		currRevision := nodeWRev.Revision
		if revision < currRevision {
			klog.Warningf("Node is registering with a older revision, it may crash or restart as a new node, %s", nodeId)
		}
		nodeWRev.Revision = revision
	}

	fornaxNode, err := nm.nodeManager.UpdateNodeState(nodeId, v1node)
	if err != nil {
		klog.ErrorS(err, "Failed to setup node", "node", v1node)
		return nil, err
	}

	daemons := []*v1.Pod{}
	for _, v := range fornaxNode.DaemonPods {
		daemons = append(daemons, v.DeepCopy())
	}
	// update node, send node configuration
	domain := default_config.DefaultDomainName
	nodeConig := grpc.FornaxCoreMessage_NodeConfiguration{
		NodeConfiguration: &grpc.NodeConfiguration{
			ClusterDomain: domain,
			Node:          fornaxNode.Node.DeepCopy(),
			DaemonPods:    daemons,
		},
	}
	messageType := grpc.MessageType_NODE_CONFIGURATION
	m := &grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &nodeConig,
	}

	return m, nil
}

// OnNodeConnect update node state, make node ready for schedule pod
func (nm *nodeMonitor) OnNodeConnect(nodeId string) error {
	klog.InfoS("A node connected to FornaxCore", "node", nodeId)
	nodeWRev := nm.staleNodes.get(nodeId)
	if nodeWRev != nil {
		nm.staleNodes.delete(nodeId)
		// ask node to full sync since this node was disconnected before
		return nodeagent.NodeRevisionOutOfOrderError
	}
	return nil
}

// OnNodeDisconnect update node state, make node ready for schedule pod
func (nm *nodeMonitor) OnNodeDisconnect(nodeId string) error {
	klog.InfoS("A node disconnected from FornaxCore", "node", nodeId)
	nodeWRev := nm.nodes.get(nodeId)
	if nodeWRev != nil {
		nm.staleNodes.add(nodeId, nodeWRev)
	} else {
		nm.staleNodes.add(nodeId, &NodeWithRevision{
			NodeId:   nodeId,
			Revision: 0,
		})
	}
	nm.nodeManager.DisconnectNode(nodeId)
	return nil
}

// OnNodeReady update node state, make node ready for schedule pod
func (nm *nodeMonitor) OnNodeReady(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
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
func (nm *nodeMonitor) OnNodeStateUpdate(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	nodeState := message.GetNodeState()
	nodeId := message.GetNodeIdentifier().GetIdentifier()
	// klog.V(5).InfoS("Received a node state", "node", nodeId, "revision", nodeState.GetNodeRevision(), "pods", len(nodeState.GetPodStates()))
	err := nm.updateOrCreateNode(nodeId, nodeState.GetNode(), nodeState.GetNodeRevision(), nodeState.GetPodStates())
	if err != nil {
		klog.ErrorS(err, "Failed to sync a node", "node", nodeId)
		return nil, err
	}

	return nil, nil
}

// OnPodStateUpdate update single pod state
func (nm *nodeMonitor) OnPodStateUpdate(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	podState := message.GetPodState()
	revision := podState.GetNodeRevision()
	nodeId := message.GetNodeIdentifier()
	// klog.V(5).InfoS("Received a pod state", "nodeId", nodeId,
	//  "pod", util.Name(podState.GetPod()),
	//  "pod phase", podState.GetPod().Status.Phase,
	//  "condition", k8spodutil.IsPodReady(podState.GetPod()),
	//  "node revision", revision)

	nodeWRev := nm.nodes.get(nodeId.GetIdentifier())
	if nodeWRev == nil {
		return nil, nodeagent.NodeRevisionOutOfOrderError
	}
	if err := nm.validateNodeRevision(nodeWRev, revision); err != nil {
		return nil, err
	}
	if revision == nodeWRev.Revision {
		klog.InfoS("Received a pod state with same node revision as current node revision", "pod", podState.Pod.Name)
	}

	nodeWRev.Revision = revision
	nodeLabel := util.GetPodFornaxNodeIdAnnotation(podState.GetPod())
	if nodeId.GetIdentifier() != nodeLabel || len(nodeLabel) == 0 {
		err := fmt.Errorf("Pod %s from does not have %s label, or value != received nodeId %s", util.Name(podState.Pod), fornaxv1.AnnotationFornaxCoreNode, nodeId.GetIdentifier())
		klog.ErrorS(err, "pod", podState)
		return nil, err
	}
	err := nm.nodeManager.UpdatePodState(nodeId.GetIdentifier(), podState.GetPod(), podState.GetSessionStates())
	if err != nil {
		klog.ErrorS(err, "Failed to update pod state", "pod", podState)
		return nil, err
	}
	return nil, nil
}

func (nm *nodeMonitor) validateNodeRevision(nodeWRev *NodeWithRevision, revision int64) error {
	if nodeWRev == nil {
		return nodeagent.NodeRevisionOutOfOrderError
	}

	currRevision := nodeWRev.Revision
	if revision == currRevision+1 || revision == currRevision {
		// allow same revison for node state update
	} else {
		klog.Warningf("Received a disordred revision from node %s, fornax core revision: %d, received revision: %d", nodeWRev.NodeId, currRevision, revision)
		return nodeagent.NodeRevisionOutOfOrderError
	}
	return nil
}

func (nm *nodeMonitor) updateOrCreateNode(nodeId string, v1node *v1.Node, revision int64, podStates []*grpc.PodState) error {
	if nodeWRev := nm.nodes.get(nodeId); nodeWRev == nil {
		nm.nodes.add(nodeId, &NodeWithRevision{NodeId: nodeId, Revision: revision})
	} else {
		// 1 is initial revision of new node, node revision goes back to 1 if node crash and lost history data
		if nodeWRev.Revision > revision && revision != 1 {
			klog.Warning("node revision is older than current revision", "node", v1node, "currRevision", nodeWRev.Revision, "revision", revision)
			return nil
		}
		nodeWRev.Revision = revision
	}
	_, err := nm.nodeManager.UpdateNodeState(nodeId, v1node)
	if err != nil {
		klog.ErrorS(err, "Failed to update a node", "node", v1node)
		return err
	}
	nm.nodeManager.SyncPodStates(nodeId, podStates)

	return nil
}

func NewNodeMonitor(nodeManager ie.NodeManagerInterface) *nodeMonitor {
	nm := &nodeMonitor{
		chQuit:      make(chan interface{}),
		nodeManager: nodeManager,
		nodes: NodeRevisionMap{
			mu:    sync.RWMutex{},
			nodes: map[string]*NodeWithRevision{},
		},
		staleNodes: NodeRevisionMap{
			mu:    sync.RWMutex{},
			nodes: map[string]*NodeWithRevision{},
		},
	}

	return nm
}
