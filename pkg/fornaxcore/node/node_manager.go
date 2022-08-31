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

package node

import (
	"context"
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/collection"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxpod "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	fornaxutil "centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// todo: determine more proper timeout
const (
	DefaultStaleNodeTimeout = 120 * time.Second
	MaxlengthOfNodeUpdates  = 5000
)

var _ ie.NodeManagerInterface = &nodeManager{}

type nodeManager struct {
	ctx                context.Context
	nodeUpdates        chan interface{}
	watchers           []chan<- interface{}
	nodes              NodePool
	nodeAgent          nodeagent.NodeAgentClient
	podManager         ie.PodManagerInterface
	sessionManager     ie.SessionManagerInterface
	nodePodCidrManager NodeCidrManager
	nodeDaemonManager  NodeDaemonManager
	houseKeepingTicker *time.Ticker
}

// UpdateSessionState implements NodeManager
func (nm *nodeManager) UpdateSessionState(nodeIdentifier string, session *fornaxv1.ApplicationSession) error {
	podName := session.Status.PodReference.Name
	pod := nm.podManager.FindPod(podName)
	if pod != nil {
		nm.sessionManager.UpdateSessionStatusFromNode(nodeIdentifier, pod, []*fornaxv1.ApplicationSession{session})
	} else {
		klog.InfoS("Pod does not exist in pod manager, can not update session info", "session", util.Name(session), "pod", podName)
	}
	return nil
}

// WatchNode implements NodeManager
func (nm *nodeManager) Watch(watcher chan<- interface{}) {
	nm.watchers = append(nm.watchers, watcher)
}

// ListNodes return all nodes
func (nm *nodeManager) List() []*ie.NodeEvent {
	nodes := []*ie.NodeEvent{}
	for _, v := range nm.nodes.list() {
		nodes = append(nodes, &ie.NodeEvent{
			NodeId: v.NodeId,
			Node:   v.Node.DeepCopy(),
			Type:   ie.NodeEventTypeCreate,
		})
	}

	return nodes
}

// UpdatePodState check node status and update single pod state
func (nm *nodeManager) UpdatePodState(nodeId string, pod *v1.Pod, sessions []*fornaxv1.ApplicationSession) error {
	if nodeWS := nm.nodes.get(nodeId); nodeWS != nil {
		nodeWS.LastSeen = time.Now()
		nm.handleAPodState(nodeId, nodeWS, pod.DeepCopy())
		nm.sessionManager.UpdateSessionStatusFromNode(nodeId, pod, sessions)
	} else {
		// TODO, node supposed to exist
		klog.InfoS("Node does not exist, ask node full sync", "node", nodeId)
	}

	return nil
}

func (nm *nodeManager) SyncNodePodStates(nodeId string, podStates []*grpc.PodState) {
	klog.InfoS("Sync pods state for node", "node", nodeId)
	var err error
	nodeWS := nm.nodes.get(nodeId)
	if nodeWS == nil {
		// TODO, node supposed to exist
		return
	}

	nodeWS.LastSeen = time.Now()
	existingPodNames := nodeWS.Pods.GetKeys()
	reportedPods := map[string]bool{}
	for _, podState := range podStates {
		podName := fornaxutil.Name(podState.GetPod())
		reportedPods[podName] = true
		err = nm.handleAPodState(nodeId, nodeWS, podState.GetPod().DeepCopy())
		if err != nil {
			klog.ErrorS(err, "Failed to update a pod state, wait for next sync", "pod", podName)
		}
	}

	// reverse lookup, find deleted pods
	deletedPods := []string{}
	for _, podName := range existingPodNames {
		if _, found := reportedPods[podName]; !found {
			pod := nm.podManager.FindPod(podName)
			if pod != nil {
				// node do not report this pod again, delete it immediately
				if util.PodNotTerminated(pod) {
					pod.Status.Phase = v1.PodFailed
				}
				_, err = nm.podManager.DeletePod(nodeId, pod)
				if err != nil && err != fornaxpod.PodNotFoundError {
					klog.ErrorS(err, "Failed to delete a pod, wait for next sync", "pod", podName)
				}
			} else {
				deletedPods = append(deletedPods, podName)
			}
		}
	}

	for _, v := range deletedPods {
		nodeWS.Pods.Delete(v)
	}
}

func (nm *nodeManager) handleAPodState(nodeId string, fornaxnode *ie.FornaxNodeWithState, newStatePod *v1.Pod) error {
	podName := fornaxutil.Name(newStatePod)

	// even a pod is terminated status, we still keep it in podmanager, it got deleted until next time pod does not report it again
	_, err := nm.podManager.AddPod(nodeId, newStatePod)
	if err != nil {
		return err
	}

	fornaxnode.Pods.Add(podName)
	return nil
}

// SetupNode complete node spec info provided by node agent, including
// 1/ pod cidr
// 2/ cloud providerID
// it also return daemon pods node should initialize before taking service pods
func (nm *nodeManager) SetupNode(nodeId string, node *v1.Node) (fornaxnode *ie.FornaxNodeWithState, err error) {
	if nodeWS := nm.nodes.get(nodeId); nodeWS != nil {
		fornaxutil.MergeNodeStatus(nodeWS.Node, node)

		// reassign cidrs if control plane changed
		cidrs := nm.nodePodCidrManager.GetCidr(node)
		if cidrs[0] != nodeWS.Node.Spec.PodCIDR {
			nodeWS.Node.Spec.PodCIDR = cidrs[0]
			nodeWS.Node.Spec.PodCIDRs = cidrs
		}
		fornaxnode = nodeWS
	} else {
		if fornaxnode, err = nm.CreateNode(nodeId, node); err != nil {
			klog.ErrorS(err, "Failed to create a node", "node", fornaxnode)
			return nil, err
		}
	}

	// recalculate daemon pods on node always to make sure node has correct setup
	daemons := nm.nodeDaemonManager.GetDaemons(fornaxnode)
	fornaxnode.DaemonPods = daemons
	return fornaxnode, nil
}

func (nm *nodeManager) FindNode(name string) *ie.FornaxNodeWithState {
	return nm.nodes.get(name)
}

// CreateNode create a new node, and assign pod cidr
func (nm *nodeManager) CreateNode(nodeId string, node *v1.Node) (fornaxNode *ie.FornaxNodeWithState, err error) {
	if fornaxNode = nm.nodes.get(nodeId); fornaxNode != nil {
		return nil, nodeagent.NodeAlreadyExistError
	}

	fornaxNode = &ie.FornaxNodeWithState{
		NodeId:     nodeId,
		Node:       node.DeepCopy(),
		State:      ie.NodeWorkingStateRegistering,
		Pods:       collection.NewConcurrentSet(),
		DaemonPods: map[string]*v1.Pod{},
		LastSeen:   time.Now(),
	}

	if util.IsNodeCondtionReady(node) {
		fornaxNode.State = ie.NodeWorkingStateRunning
	}

	cidrs := nm.nodePodCidrManager.GetCidr(node)
	fornaxNode.Node.Spec.PodCIDR = cidrs[0]
	fornaxNode.Node.Spec.PodCIDRs = cidrs

	daemons := nm.nodeDaemonManager.GetDaemons(fornaxNode)
	fornaxNode.DaemonPods = daemons
	nm.nodes.add(fornaxutil.Name(node), fornaxNode)
	nm.nodeUpdates <- &ie.NodeEvent{
		NodeId: nodeId,
		Node:   fornaxNode.Node.DeepCopy(),
		Type:   ie.NodeEventTypeCreate,
	}
	return fornaxNode, nil
}

// UpdateNode implements NodeManager
func (nm *nodeManager) UpdateNode(nodeId string, node *v1.Node) (*ie.FornaxNodeWithState, error) {
	if nodeWS := nm.nodes.get(nodeId); nodeWS != nil {
		oldNodeWSState := nodeWS.State
		if util.IsNodeCondtionReady(node) {
			nodeWS.State = ie.NodeWorkingStateRunning
		}
		nodeWS.LastSeen = time.Now()

		// nodeWS.State change means node probably disconnected, need to resend node event
		if oldNodeWSState == nodeWS.State && node.ResourceVersion == nodeWS.Node.ResourceVersion {
			return nodeWS, nil
		}
		fornaxutil.MergeNodeStatus(nodeWS.Node, node)
		nm.nodeUpdates <- &ie.NodeEvent{
			NodeId: nodeId,
			Node:   nodeWS.Node.DeepCopy(),
			Type:   ie.NodeEventTypeUpdate,
		}
		return nodeWS, nil
	} else {
		return nil, nodeagent.NodeNotFoundError
	}
}

// DeleteNode send node event tell node not schedulable, it got removed from list after a graceful period
func (nm *nodeManager) DisconnectNode(nodeId string) error {
	if nodeWS := nm.nodes.get(nodeId); nodeWS != nil {
		nodeWS.State = ie.NodeWorkingStateDisconnected
		nodeWS.Node.Status.Phase = v1.NodePending
		nm.nodeUpdates <- &ie.NodeEvent{
			NodeId: nodeId,
			Node:   nodeWS.Node.DeepCopy(),
			Type:   ie.NodeEventTypeUpdate,
		}
	}
	return nil
}

func (nm *nodeManager) Run() error {
	klog.Info("starting node manager")
	go func() {
		for {
			select {
			case <-nm.ctx.Done():
				// TODO, shutdown more gracefully, handoff fornaxcore primary ownership
				break
			case update := <-nm.nodeUpdates:
				for _, watcher := range nm.watchers {
					watcher <- update
				}
			}
		}
	}()

	return nil
}

func (nm *nodeManager) PrintNodeSummary() {
	klog.InfoS("node summary:", "#node", nm.nodes.length())
	for _, v := range nm.nodes.list() {
		klog.InfoS("node", "node", v.Node.Name, "state", v.State, "#pod", v.Pods.Len(), "#daemon pod", len(v.DaemonPods))
	}
}

func NewNodeManager(ctx context.Context, checkPeriod time.Duration, nodeAgent nodeagent.NodeAgentClient, podManager ie.PodManagerInterface, sessionManager ie.SessionManagerInterface) *nodeManager {
	return &nodeManager{
		ctx:                ctx,
		nodeUpdates:        make(chan interface{}, 100),
		watchers:           []chan<- interface{}{},
		nodeAgent:          nodeAgent,
		nodePodCidrManager: NewPodCidrManager(),
		nodeDaemonManager:  NewNodeDaemonManager(),
		houseKeepingTicker: time.NewTicker(checkPeriod),
		podManager:         podManager,
		sessionManager:     sessionManager,
		nodes: NodePool{
			mu:    sync.RWMutex{},
			nodes: map[string]*ie.FornaxNodeWithState{},
		},
	}
}
