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
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	k8spodutil "k8s.io/kubernetes/pkg/api/v1/pod"

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
	defer pool.mu.Unlock()
	pool.nodes[name] = node
}

func (pool *NodeRevisionMap) delete(name string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.nodes, name)
}

func (pool *NodeRevisionMap) get(name string) *NodeWithRevision {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	if n, found := pool.nodes[name]; found {
		return n
	}

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
	st := time.Now().UnixMicro()
	sessionState := message.GetSessionState()
	revision := sessionState.GetNodeRevision()
	nodeId := message.GetNodeIdentifier().GetIdentifier()
	session := &fornaxv1.ApplicationSession{}
	if err := json.Unmarshal(sessionState.GetSessionData(), session); err != nil {
		klog.ErrorS(err, "Malformed SessionData, is not a valid fornaxv1.ApplicationSession object")
		return nil, err
	}
	defer func() {
		et := time.Now().UnixMicro()
		klog.InfoS("Done Received a session state", "node", nodeId, "session", util.Name(session), "node revision", revision, "status", session.Status, "took-micro", et-st)
	}()

	nodeWRev := nm.nodes.get(nodeId)
	if nodeWRev == nil {
		return nil, nodeagent.NodeRevisionOutOfOrderError
	}
	if err := nm.validateNodeRevision(nodeWRev, revision); err != nil {
		return nil, err
	}
	if revision == nodeWRev.Revision {
		klog.InfoS("Received a session with same revision of current node state, node probably send its state earlier with this revison, continue to handle single session state", "session", util.Name(session))
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
func (nm *nodeMonitor) OnRegistry(message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
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
	klog.InfoS("Received a node state", "node", nodeId, "revision", nodeState.GetNodeRevision(), "pods", len(nodeState.GetPodStates()))
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
	klog.InfoS("Received a pod state", "nodeId", nodeId, "pod", util.Name(podState.GetPod()), "pod phase", podState.GetPod().Status.Phase, "condition", k8spodutil.IsPodReady(podState.GetPod()), "node revision", revision)
	st := time.Now().UnixMicro()
	defer func() {
		et := time.Now().UnixMicro()
		klog.InfoS("Done update pod state", "pod", util.Name(podState.GetPod()), "took-micro", et-st)
	}()

	nodeWRev := nm.nodes.get(nodeId.GetIdentifier())
	if nodeWRev == nil {
		return nil, nodeagent.NodeRevisionOutOfOrderError
	}
	if err := nm.validateNodeRevision(nodeWRev, revision); err != nil {
		return nil, err
	}
	if revision == nodeWRev.Revision {
		klog.InfoS("Received a pod with same revision of current node, node probably send its state earlier with this revison, continue to handle single pod state", "pod", podState.Pod.Name)
	}

	nodeWRev.Revision = revision
	sessions := []*fornaxv1.ApplicationSession{}
	for _, v := range podState.GetSessionStates() {
		session := &fornaxv1.ApplicationSession{}
		if err := json.Unmarshal(v.SessionData, session); err == nil {
			sessions = append(sessions, session)
		}
	}
	err := nm.nodeManager.UpdatePodState(nodeId.GetIdentifier(), podState.GetPod().DeepCopy(), sessions)
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
		// ok
	} else {
		klog.Warningf("Received a disordred revision from node %s, fornax core revision: %d, received revision: %d", nodeWRev.NodeId, currRevision, revision)
		return nodeagent.NodeRevisionOutOfOrderError
	}
	return nil
}

func (nm *nodeMonitor) updateOrCreateNode(nodeId string, v1node *v1.Node, revision int64, podStates []*grpc.PodState) error {
	if nodeWRev := nm.nodes.get(nodeId); nodeWRev == nil {
		nm.nodes.add(nodeId, &NodeWithRevision{
			NodeId:   nodeId,
			Revision: revision,
		})
		_, err := nm.nodeManager.CreateNode(nodeId, v1node)
		if err != nil {
			klog.ErrorS(err, "Failed to create a node", "node", v1node)
			return err
		}
		nm.nodeManager.SyncNodePodStates(nodeId, podStates)
	} else {
		if nodeWRev.Revision > revision && revision != 1 {
			klog.Warning("received a revision which is older than current revision", "node", v1node, "currRevision", nodeWRev.Revision, "revision", revision)
			return nil
		}
		nodeWRev.Revision = revision
		_, err := nm.nodeManager.UpdateNode(nodeId, v1node)
		if err != nil {
			klog.ErrorS(err, "Failed to update a node", "node", v1node)
			return err
		}
		nm.nodeManager.SyncNodePodStates(nodeId, podStates)
	}

	return nil
}

func (nm *nodeMonitor) CheckStaleNode() {
	//TODO
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
