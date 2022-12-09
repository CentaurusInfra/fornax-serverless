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
	"encoding/json"
	"sync"

	// "sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/collection"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxpod "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	fornaxstore "centaurusinfra.io/fornax-serverless/pkg/store"
	"centaurusinfra.io/fornax-serverless/pkg/store/factory"
	"centaurusinfra.io/fornax-serverless/pkg/util"
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
	nodeUpdates        chan *ie.NodeEvent
	watchers           []chan<- *ie.NodeEvent
	nodeStore          fornaxstore.ApiStorageInterface
	nodes              NodePool
	nodeAgent          nodeagent.NodeAgentClient
	podManager         ie.PodManagerInterface
	sessionManager     ie.SessionManagerInterface
	nodePodCidrManager NodeCidrManager
	nodeDaemonManager  NodeDaemonManager
	houseKeepingTicker *time.Ticker
}

// Watch add a watcher, and beging to send NodeEvent to watcher
func (nm *nodeManager) Watch(watcher chan<- *ie.NodeEvent) {
	nm.watchers = append(nm.watchers, watcher)
}

// List return all nodes in NodeEvent
func (nm *nodeManager) List() []*ie.NodeEvent {
	nodeEvents := []*ie.NodeEvent{}
	for _, v := range nm.nodes.list() {
		nodeEvents = append(nodeEvents, &ie.NodeEvent{
			Node: v.Node.DeepCopy(),
			Type: ie.NodeEventTypeCreate,
		})
	}

	return nodeEvents
}

// UpdateSessionState implements NodeManagerInterface
func (nm *nodeManager) UpdateSessionState(nodeId string, session *fornaxv1.ApplicationSession) error {
	podName := session.Status.PodReference.Name
	pod := nm.podManager.FindPod(podName)
	if pod != nil {
		nm.sessionManager.OnSessionStatusFromNode(nodeId, pod, session)
	} else {
		klog.Warningf("Pod %s does not exist in pod manager, can not update session %s", podName, util.Name(session))
	}
	return nil
}

// UpdatePodState check pod revision status and update single pod state
// even a pod is terminated status, we still keep it in podmanager,
// it got deleted until next time pod does not report it again in node state event
func (nm *nodeManager) UpdatePodState(nodeId string, pod *v1.Pod, sessions []*fornaxv1.ApplicationSession) error {
	podName := util.Name(pod)
	if nodeWS := nm.nodes.get(nodeId); nodeWS != nil {
		nodeWS.LastSeen = time.Now()
		if existingPod := nm.podManager.FindPod(podName); existingPod != nil {
			if largerRv, _ := util.NodeRevisionLargerThan(pod, existingPod); !largerRv {
				return nil
			}
		}
		updatedPod, err := nm.podManager.AddOrUpdatePod(pod)
		if err != nil {
			return err
		}
		nodeWS.Pods.Add(podName)
		for _, session := range sessions {
			nm.sessionManager.OnSessionStatusFromNode(nodeId, updatedPod, session)
		}
	} else {
		// not supposed to happend node state is send when node register
		klog.Warningf("Node %s does not exist, ask node to do full sync", nodeId)
		return nodeagent.NodeRevisionOutOfOrderError
	}
	return nil
}

func (nm *nodeManager) SyncPodStates(nodeId string, podStates []*grpc.PodState) {
	var err error
	nodeWS := nm.nodes.get(nodeId)
	if nodeWS == nil {
		// this is a situation could not happen, just for safety check
		return
	}

	nodeWS.LastSeen = time.Now()
	existingPodNames := nodeWS.Pods.GetKeys()
	reportedPods := map[string]bool{}
	for _, podState := range podStates {
		podName := util.Name(podState.GetPod())
		reportedPods[podName] = true
		sessions := []*fornaxv1.ApplicationSession{}
		for _, v := range podState.GetSessionStates() {
			session := &fornaxv1.ApplicationSession{}
			if err := json.Unmarshal(v.SessionData, session); err == nil {
				sessions = append(sessions, session)
			}
		}
		err = nm.UpdatePodState(nodeId, podState.GetPod().DeepCopy(), sessions)
		if err != nil {
			klog.ErrorS(err, "Failed to update a pod state, wait for next sync", "pod", podName)
		}
	}

	// reverse lookup, find deleted pods
	deletedPods := []string{}
	for _, podName := range existingPodNames {
		// could have a race condition, pod scheduler just send a pod request to node and node was reporting state before it received request
		// need to check a grace period, creation time stamp is not good one if pod stay in pod scheduler for a while
		if _, found := reportedPods[podName]; !found {
			pod := nm.podManager.FindPod(podName)
			if pod != nil {
				// node do not report this pod again, delete it immediately
				if util.PodNotTerminated(pod) {
					pod.Status.Phase = v1.PodFailed
				}
				_, err = nm.podManager.DeletePod(pod)
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

// UpdateNodeState complete node spec info provided by node agent, including
// 1/ node pod cidr
// 2/ node daemons
// it also return daemon pods node should initialize before taking service pods
func (nm *nodeManager) UpdateNodeState(nodeId string, node *v1.Node) (fornaxNode *ie.FornaxNodeWithState, err error) {
	cidrs := nm.nodePodCidrManager.GetCidr(node)
	if node.Spec.PodCIDR != cidrs[0] {
		node.Spec.PodCIDR = cidrs[0]
		node.Spec.PodCIDRs = cidrs
	}
	if fornaxNode = nm.nodes.get(nodeId); fornaxNode != nil {
		if fornaxNode, err = nm.updateNode(nodeId, node); err != nil {
			klog.ErrorS(err, "Failed to update a node", "node", node)
			return nil, err
		}
	} else {
		if fornaxNode, err = nm.createNode(nodeId, node); err != nil {
			klog.ErrorS(err, "Failed to create a node", "node", node)
			return nil, err
		}
	}

	// recalculate daemon pods on node always to make sure node has correct setup
	daemons := nm.nodeDaemonManager.GetDaemons(fornaxNode)
	fornaxNode.DaemonPods = daemons
	return fornaxNode, nil
}

func (nm *nodeManager) createOrUpdateNodeInStore(node *v1.Node) (*v1.Node, error) {
	nodeInStore, err := factory.GetFornaxNodeCache(nm.nodeStore, util.Name(node))
	if err != nil {
		return nil, err
	}
	if nodeInStore == nil {
		nodeInStore = node.DeepCopy()
		nodeInStore, err = factory.CreateFornaxNode(nm.ctx, nm.nodeStore, nodeInStore)
		if err != nil {
			return nil, err
		}
	} else {
		util.MergeNodeStatus(nodeInStore, node)
		nodeInStore, err = factory.UpdateFornaxNode(nm.ctx, nm.nodeStore, nodeInStore)
		if err != nil {
			return nil, err
		}
	}

	return nodeInStore, nil
}

// createNode create a new node, and assign pod cidr
func (nm *nodeManager) createNode(nodeId string, node *v1.Node) (fornaxNode *ie.FornaxNodeWithState, err error) {
	if fornaxNode = nm.nodes.get(nodeId); fornaxNode != nil {
		return nil, nodeagent.NodeAlreadyExistError
	}

	fornaxNode = &ie.FornaxNodeWithState{
		NodeId:     nodeId,
		Revision:   node.ResourceVersion,
		State:      ie.NodeWorkingStateRegistering,
		Pods:       collection.NewConcurrentSet(),
		DaemonPods: map[string]*v1.Pod{},
		LastSeen:   time.Now(),
	}

	if util.IsNodeCondtionReady(node) {
		fornaxNode.State = ie.NodeWorkingStateRunning
	}

	nodeInStore, err := nm.createOrUpdateNodeInStore(node)
	if err != nil {
		return nil, err
	}
	fornaxNode.Node = nodeInStore
	daemons := nm.nodeDaemonManager.GetDaemons(fornaxNode)
	fornaxNode.DaemonPods = daemons
	nm.nodes.add(util.Name(node), fornaxNode)
	nm.nodeUpdates <- &ie.NodeEvent{
		Node: nodeInStore.DeepCopy(),
		Type: ie.NodeEventTypeCreate,
	}
	return fornaxNode, nil
}

// updateNode implements NodeManager
func (nm *nodeManager) updateNode(nodeId string, node *v1.Node) (*ie.FornaxNodeWithState, error) {
	if fornaxNode := nm.nodes.get(nodeId); fornaxNode != nil {
		if util.IsNodeCondtionReady(node) {
			fornaxNode.State = ie.NodeWorkingStateRunning
		}
		fornaxNode.LastSeen = time.Now()

		nodeInStore, err := nm.createOrUpdateNodeInStore(node)
		if err != nil {
			return nil, err
		}
		fornaxNode.Node = nodeInStore
		nm.nodeUpdates <- &ie.NodeEvent{
			Node: fornaxNode.Node.DeepCopy(),
			Type: ie.NodeEventTypeUpdate,
		}
		return fornaxNode, nil
	} else {
		return nil, nodeagent.NodeNotFoundError
	}
}

// DeleteNode send node event tell node not schedulable, it got removed from list after a graceful period
func (nm *nodeManager) DisconnectNode(nodeId string) error {
	if fornaxNode := nm.nodes.get(nodeId); fornaxNode != nil {
		fornaxNode.State = ie.NodeWorkingStateDisconnected
		node := fornaxNode.Node.DeepCopy()
		node.Status.Phase = v1.NodePending
		nodeInStore, err := nm.createOrUpdateNodeInStore(node)
		if err != nil {
			return err
		}
		fornaxNode.Node = nodeInStore
		nm.nodeUpdates <- &ie.NodeEvent{
			Node: fornaxNode.Node.DeepCopy(),
			Type: ie.NodeEventTypeUpdate,
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

func NewNodeManager(ctx context.Context, nodeStore fornaxstore.ApiStorageInterface, nodeAgent nodeagent.NodeAgentClient, podManager ie.PodManagerInterface, sessionManager ie.SessionManagerInterface) *nodeManager {
	return &nodeManager{
		ctx:                ctx,
		nodeUpdates:        make(chan *ie.NodeEvent, 100),
		watchers:           []chan<- *ie.NodeEvent{},
		nodeStore:          nodeStore,
		nodeAgent:          nodeAgent,
		nodePodCidrManager: NewPodCidrManager(),
		nodeDaemonManager:  NewNodeDaemonManager(),
		houseKeepingTicker: time.NewTicker(DefaultStaleNodeTimeout),
		podManager:         podManager,
		sessionManager:     sessionManager,
		nodes: NodePool{
			mu:    sync.RWMutex{},
			nodes: map[string]*ie.FornaxNodeWithState{},
		},
	}
}
