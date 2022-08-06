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
	"errors"
	"fmt"
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/collection"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxpod "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/store"
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

type NodeManager interface {
	ie.NodeInfoProvider
	FindNode(name string) *FornaxNodeWithState
	CreateNode(node *v1.Node, revision int64) (*FornaxNodeWithState, error)
	DeleteNode(node *v1.Node) error
	UpdateNode(node *v1.Node, revision int64) (*FornaxNodeWithState, error)
	SetupNode(node *v1.Node, revision int64) (*FornaxNodeWithState, error)
	UpdatePodState(pod *v1.Pod, revision int64) error
	SyncNodePodStates(node *v1.Node, podStates []*grpc.PodState)
	GetRevision() int64
}

var _ NodeManager = &nodeManager{}

type nodeManager struct {
	ctx                context.Context
	nodeUpdates        chan interface{}
	watchers           []chan<- interface{}
	nodes              map[string]*FornaxNodeWithState
	nodeUpdateBucket   *NodeUpdateBucket
	nodeAgent          nodeagent.NodeAgentProxy
	podManager         fornaxpod.PodManager
	nodePodCidrManager NodeCidrManager
	nodeDaemonManager  NodeDaemonManager
	staleNodeBucket    *StaleNodeBucket
	houseKeepingTicker *time.Ticker
}

// WatchNode implements NodeManager
func (nm *nodeManager) Watch(watcher chan<- interface{}) error {
	nm.watchers = append(nm.watchers, watcher)
	return nil
}

// ListNodes return all nodes
func (nm *nodeManager) List() []*v1.Node {
	nodes := []*v1.Node{}
	for _, v := range nm.nodes {
		nodes = append(nodes, v.Node.DeepCopy())
	}
	return nodes
}

// GetRevision return revision of node tracked in fornax core
func (nm *nodeManager) GetRevision() int64 {
	return nm.nodeUpdateBucket.internalGlobalRevision
}

// UpdatePodState check node revision and update single pod state
func (nm *nodeManager) UpdatePodState(pod *v1.Pod, revision int64) error {
	nodeIdentifier, found := pod.Labels[fornaxv1.LabelFornaxCoreNode]
	if !found {
		msg := fmt.Sprintf("Pod miss node label: %s", fornaxv1.LabelFornaxCoreNode)
		// TODO, terminate this pod
		return errors.New(msg)
	}

	if nodeWS, found := nm.nodes[nodeIdentifier]; found {
		nodeWS.LastSeen = time.Now()
		nodeWS.Revision = revision
		nm.handleAPodState(nodeWS, pod.DeepCopy())
	} else {
		// TODO, node supposed to exist
		klog.InfoS("Node does not exist, ask node full sync", "node", nodeIdentifier)
	}

	return nil
}

func (nm *nodeManager) SyncNodePodStates(node *v1.Node, podStates []*grpc.PodState) {
	nodeName := fornaxutil.UniqueNodeName(node)
	klog.InfoS("Sync pods state for node", "node", nodeName)
	var err error
	var nodeWS *FornaxNodeWithState
	var found bool
	if nodeWS, found = nm.nodes[nodeName]; !found {
		// TODO, node supposed to exist
		return
	}

	nodeWS.LastSeen = time.Now()
	existingPodNames := nodeWS.Pods.GetKeys()
	reportedPods := map[string]bool{}
	for _, podState := range podStates {
		podName := fornaxutil.UniquePodName(podState.GetPod())
		reportedPods[podName] = true
		err = nm.handleAPodState(nodeWS, podState.GetPod().DeepCopy())
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
				if util.IsPodNotTerminated(pod) {
					pod.Status.Phase = v1.PodFailed
				}
				_, err = nm.podManager.DeletePod(pod, nodeName)
				if err != nil && err != fornaxpod.PodNotFoundError {
					klog.ErrorS(err, "Failed to delete a pod, wait for next sync", "pod", podName)
				}
			} else {
				deletedPods = append(deletedPods, podName)
			}
		}
	}

	for _, v := range deletedPods {
		nodeWS.Pods.DeleteItem(v)
	}
}

// handleAPodState
func (nm *nodeManager) handleAPodState(fornaxnode *FornaxNodeWithState, newStatePod *v1.Pod) error {
	podName := fornaxutil.UniquePodName(newStatePod)

	// even a pod is terminated status, we still keep it in podmanager, it got deleted until next time pod does not report it again
	_, err := nm.podManager.AddPod(newStatePod, util.UniqueNodeName(fornaxnode.Node))
	if err != nil {
		return err
	}

	fornaxnode.Pods.AddItem(podName)
	return nil
}

// SetupNode complete node spec info provided by node agent, including
// 1/ pod cidr
// 2/ cloud providerID
// it also return daemon pods node should initialize before taking service pods
func (nm *nodeManager) SetupNode(node *v1.Node, revision int64) (fornaxnode *FornaxNodeWithState, err error) {
	if oldcopy, found := nm.nodes[fornaxutil.UniqueNodeName(node)]; found {
		fornaxutil.MergeNodeStatus(oldcopy.Node, node)

		// reassign cidrs if control plane changed
		cidrs := nm.nodePodCidrManager.GetCidr(node)
		if cidrs[0] != oldcopy.Node.Spec.PodCIDR {
			oldcopy.Node.Spec.PodCIDR = cidrs[0]
			oldcopy.Node.Spec.PodCIDRs = cidrs
		}
		oldcopy.Revision = revision
		fornaxnode = oldcopy
	} else {
		if fornaxnode, err = nm.CreateNode(node, revision); err != nil {
			klog.ErrorS(err, "Failed to create a node", "node", fornaxnode)
			return nil, err
		}
	}

	// recalculate daemon pods on node always to make sure node has correct setup
	daemons := nm.nodeDaemonManager.GetDaemons(fornaxnode)
	fornaxnode.DaemonPods = daemons
	return fornaxnode, nil
}

// FindNode implements NodeManager
func (nm *nodeManager) FindNode(name string) *FornaxNodeWithState {
	nodeWs, found := nm.nodes[name]
	if found {
		return nodeWs
	} else {
		return nil
	}
}

// CreateNode create a new node, and assign pod cidr
func (nm *nodeManager) CreateNode(node *v1.Node, revision int64) (nodeWS *FornaxNodeWithState, err error) {
	var found bool
	if nodeWS, found = nm.nodes[fornaxutil.UniqueNodeName(node)]; found {
		return nil, nodeagent.NodeAlreadyExistError
	}

	nodeWS = &FornaxNodeWithState{
		Node:       node.DeepCopy(),
		Revision:   revision,
		State:      NodeWorkingStateRegistering,
		Pods:       collection.NewConcurrentSet(),
		DaemonPods: map[string]*v1.Pod{},
		LastSeen:   time.Now(),
	}

	nm.staleNodeBucket.refreshNode(nodeWS)
	if util.IsNodeCondtionReady(node) {
		nodeWS.State = NodeWorkingStateRunning
	}

	cidrs := nm.nodePodCidrManager.GetCidr(node)
	nodeWS.Node.Spec.PodCIDR = cidrs[0]
	nodeWS.Node.Spec.PodCIDRs = cidrs

	daemons := nm.nodeDaemonManager.GetDaemons(nodeWS)
	nodeWS.DaemonPods = daemons
	nm.nodes[fornaxutil.UniqueNodeName(node)] = nodeWS
	nm.nodeUpdates <- &ie.NodeEvent{
		Node: nodeWS.Node.DeepCopy(),
		Type: ie.NodeEventTypeCreate,
	}
	return nodeWS, nil
}

// UpdateNode implements NodeManager
func (nm *nodeManager) UpdateNode(node *v1.Node, revision int64) (*FornaxNodeWithState, error) {
	if nodeWS, found := nm.nodes[fornaxutil.UniqueNodeName(node)]; found {
		nm.staleNodeBucket.refreshNode(nodeWS)
		if util.IsNodeCondtionReady(node) {
			nodeWS.State = NodeWorkingStateRunning
		}
		nodeWS.LastSeen = time.Now()

		nodeWS.Revision = revision
		fornaxutil.MergeNodeStatus(nodeWS.Node, node)
		nm.nodeUpdates <- &ie.NodeEvent{
			Node: nodeWS.Node.DeepCopy(),
			Type: ie.NodeEventTypeUpdate,
		}
		return nodeWS, nil
	} else {
		return nil, nodeagent.NodeNotFoundError
	}
}

// DeleteNode mark node not schedulable, it got removed from list after a graceful period
func (nm *nodeManager) DeleteNode(node *v1.Node) error {
	fornaxnode, found := nm.nodes[fornaxutil.UniqueNodeName(node)]
	if found {
		fornaxnode.Revision = fornaxnode.Revision + 1
		fornaxnode.State = NodeWorkingStateTerminating
		fornaxnode.Node.Status.Phase = v1.NodeTerminated
		fornaxnode.Node.DeletionTimestamp = util.NewCurrentMetaTime()
		nm.nodeUpdates <- &ie.NodeEvent{
			Node: fornaxnode.Node.DeepCopy(),
			Type: ie.NodeEventTypeDelete,
		}
	}

	return nil
}

func (nm *nodeManager) CheckStaleNode() {
	staleNodes := nm.staleNodeBucket.getStaleNodes()
	// ask nodes to send full sync request
	for _, node := range staleNodes {
		nm.nodeAgent.FullSyncNode(util.UniqueNodeName(node.Node))
	}
}

func (nm *nodeManager) Run() error {
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
				// nm.nodeUpdateBucket.appendUpdate(update)
			case <-nm.houseKeepingTicker.C:
				nm.PrintNodeSummary()
				nm.CheckStaleNode()
			}
		}
	}()

	return nil
}

func (nm *nodeManager) PrintNodeSummary() {
	klog.InfoS("node summary:", "#node", len(nm.nodes))
	for k, v := range nm.nodes {
		klog.InfoS("node", "node", k, "state", v.State, "revison", v.Revision, "#pod", v.Pods.Len(), "#daemon pod", len(v.DaemonPods))
	}
}

func NewNodeManager(ctx context.Context, checkPeriod time.Duration, nodeAgent nodeagent.NodeAgentProxy, podManager fornaxpod.PodManager) *nodeManager {
	bucketA := &store.ArrayBucket{
		Prev:     nil,
		Next:     nil,
		Elements: []interface{}{},
	}

	return &nodeManager{
		ctx:         ctx,
		nodeUpdates: make(chan interface{}, 100),
		watchers:    []chan<- interface{}{},
		nodeAgent:   nodeAgent,
		nodeUpdateBucket: &NodeUpdateBucket{
			internalGlobalRevision: 0,
			bucketHead:             bucketA,
			bucketTail:             bucketA,
		},
		nodePodCidrManager: NewPodCidrManager(),
		nodeDaemonManager:  NewNodeDaemonManager(),
		houseKeepingTicker: time.NewTicker(checkPeriod),
		podManager:         podManager,
		staleNodeBucket: &StaleNodeBucket{
			RWMutex:       sync.RWMutex{},
			bucketStale:   map[string]*FornaxNodeWithState{},
			bucketRefresh: map[string]*FornaxNodeWithState{},
		},
		nodes: map[string]*FornaxNodeWithState{},
	}
}
