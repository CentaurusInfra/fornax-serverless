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
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxpod "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/store"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	fornaxutil "centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// todo: determine more proper timeout
const (
	DefaultStaleNodeTimeout = 120 * time.Second
	MaxlengthOfNodeUpdates  = 5000
)

var (
	NodeRevisionOutofOrderError = errors.New("revision out of order between fornaxcore and node")
	NodeNotFoundError           = errors.New("node not found")
)

type NodeManager interface {
	ie.NodeInfoProvider
	FindNode(name string) *FornaxNodeWithState
	CreateNode(node *v1.Node, revision int64) (*FornaxNodeWithState, error)
	DeleteNode(node *v1.Node) error
	UpdateNode(node *v1.Node, revision int64) (*FornaxNodeWithState, error)
	SetupNode(node *v1.Node, revision int64) (*FornaxNodeWithState, error)
	UpdatePodState(podState *grpc.PodState) error
	SyncNodePodStates(nodeState *FornaxNodeWithState, revision int64, podStates []*grpc.PodState) error
	GetRevision() int64
}

var _ NodeManager = &nodeManager{}

type nodeManager struct {
	sync.RWMutex
	chQuit      chan interface{}
	nodeUpdates chan interface{}
	watchers    []chan<- interface{}

	nodeUpdateBucket *NodeUpdateBucket

	nodeAgent nodeagent.NodeAgentProxy

	// maintain pod objects reported by node
	podManager fornaxpod.PodManager

	// allocate pod cidrs of registered node
	nodePodCidrManager NodeCidrManager

	// assign daemon pods to node
	nodeDaemonManager NodeDaemonManager

	nodes map[string]*FornaxNodeWithState

	staleNodeBucket *StaleNodeBucket
	ticker          *time.Ticker
}

// WatchNode implements NodeManager
func (nm *nodeManager) WatchNode(watcher chan<- interface{}) error {
	nm.watchers = append(nm.watchers, watcher)
	return nil
}

// ListNodes return all nodes
func (nm *nodeManager) ListNodes() []*v1.Node {
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
func (nm *nodeManager) UpdatePodState(podState *grpc.PodState) error {
	revision := podState.GetNodeRevision()
	nodeIdentifier, found := podState.GetPod().Labels[fornaxv1.LabelFornaxCoreNode]
	if !found {
		msg := fmt.Sprintf("Pod miss node label: %s", fornaxv1.LabelFornaxCoreNode)
		// TODO, terminate this pod
		return errors.New(msg)
	}

	if fornaxnode, found := nm.nodes[nodeIdentifier]; found {
		crevision := fornaxnode.Revision
		if revision > crevision {
			nm.handleAPodState(fornaxnode, podState)
		} else {
			klog.Warningf("Received a older revision from node agent, fornax core revision: %d, received revision: %d", crevision, revision)
			return NodeRevisionOutofOrderError
		}
	}

	return nil
}

// SyncNodePodStates check node revision and sync full list of pod states of a node if revision is newer
func (nm *nodeManager) SyncNodePodStates(fornaxnode *FornaxNodeWithState, revision int64, podStates []*grpc.PodState) error {
	if fornaxnode, found := nm.nodes[fornaxutil.UniqueNodeName(fornaxnode.Node)]; found {
		crevision := fornaxnode.Revision
		if revision > crevision {
			// received newer revision
			newPods := map[string]*v1.Pod{}
			for _, podState := range podStates {
				podName := fornaxutil.UniquePodName(podState.GetPod())
				newPods[podName] = podState.GetPod().DeepCopy()
				nm.handleAPodState(fornaxnode, podState)
			}

			// reverse lookup, find deleted pods
			for _, pod := range fornaxnode.Pods {
				if _, found := newPods[fornaxutil.UniquePodName(pod)]; !found {
					// node do not report this pod again, delete immediately
					pod, _ := nm.podManager.DeletePod(pod)
					nm.nodeUpdates <- &ie.PodUpdate{
						Pod:    pod.DeepCopy(),
						Update: ie.PodUpdateTypeDelete,
					}
				}
			}
		} else if revision == crevision {
			klog.Warningf("Received same revision from node agent, skip, revision: %d", revision)
		} else {
			klog.Warningf("Received a older revision from node agent, fornax core revision: %d, received revision: %d", crevision, revision)
			return NodeRevisionOutofOrderError
		}
	} else {
		// TODO, node supposed to exist
	}

	return nil
}

// handleAPodState
func (nm *nodeManager) handleAPodState(fornaxnode *FornaxNodeWithState, podState *grpc.PodState) error {
	podName := fornaxutil.UniquePodName(podState.GetPod())
	if pod, ok := fornaxnode.Pods[podName]; ok {
		if !reflect.DeepEqual(podState.GetPod(), pod) {
			pod, err := nm.podManager.CreatePod(pod, util.UniqueNodeName(fornaxnode.Node))
			if err != nil {
				return err
			}
			nm.nodeUpdates <- &ie.PodUpdate{
				Pod:    pod.DeepCopy(),
				Update: ie.PodUpdateTypeUpdate,
			}
			// set updated pod back to node's pod map
			fornaxnode.Pods[podName] = pod
		}
	} else {
		pod := podState.GetPod().DeepCopy()
		// call podmanager to create one, and save created reference
		pod, err := nm.podManager.CreatePod(pod, util.UniqueNodeName(fornaxnode.Node))
		if err != nil {
			return err
		}
		fornaxnode.Pods[podName] = pod
		nm.nodeUpdates <- &ie.PodUpdate{
			Pod:    podState.GetPod().DeepCopy(),
			Update: ie.PodUpdateTypeCreate,
		}
	}
	return nil
}

// UpdateNode implements NodeManager
func (nm *nodeManager) UpdateNode(node *v1.Node, revision int64) (*FornaxNodeWithState, error) {
	if oldcopy, found := nm.nodes[fornaxutil.UniqueNodeName(node)]; found {
		crevision := oldcopy.Revision
		if revision > crevision {
			// received newer revision,
			// TODO, update node resource and status
			oldcopy.Revision = revision
			fornaxutil.MergeNodeStatus(oldcopy.Node, node)
		} else if revision == crevision {
			// received same revision, skip
		} else {
			klog.Warningf("Received a older revision from node agent, fornax core revision: %d, received revision: %d", crevision, revision)
			return nil, NodeRevisionOutofOrderError
		}
	} else {
		return nil, NodeNotFoundError
	}
	return nil, nil
}

// SetupNode complete node spec info provided by node agent, including
// 1/ pod cidr
// 2/ cloud providerID
// it also return daemon pods node should initialize before taking service pods
func (nm *nodeManager) SetupNode(node *v1.Node, revision int64) (fornaxnode *FornaxNodeWithState, err error) {
	if oldcopy, found := nm.nodes[fornaxutil.UniqueNodeName(node)]; found {
		crevision := oldcopy.Revision
		if revision > crevision {
			// received newer revision,
			fornaxutil.MergeNodeStatus(oldcopy.Node, node)

			// reassign cidrs if control plane changed
			cidrs := nm.nodePodCidrManager.GetCidr(oldcopy)
			if cidrs[0] != oldcopy.Node.Spec.PodCIDR {
				oldcopy.Node.Spec.PodCIDR = cidrs[0]
				oldcopy.Node.Spec.PodCIDRs = cidrs
			}
			oldcopy.Revision = revision
			fornaxnode = oldcopy
		} else if revision == crevision {
			// received same revision, skip
		} else {
			klog.Warningf("Received a older revision from node agent, fornax core revision: %d, received revision: %d", crevision, revision)
			return nil, NodeRevisionOutofOrderError
		}
	} else {
		if fornaxnode, err = nm.CreateNode(node, revision); err != nil {
			klog.ErrorS(err, "Failed to create a node", "node", fornaxnode)
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
func (nm *nodeManager) CreateNode(node *v1.Node, revision int64) (*FornaxNodeWithState, error) {
	fornaxnode := &FornaxNodeWithState{
		Node:       node.DeepCopy(),
		Revision:   revision,
		State:      NodeWorkingStateRegistering,
		Pods:       map[string]*v1.Pod{},
		DaemonPods: map[string]*v1.Pod{},
	}

	fornaxnode.Node.Status.Phase = v1.NodePending
	fornaxnode.Node.ObjectMeta.CreationTimestamp = metav1.Time{
		Time: time.Now(),
	}
	cidrs := nm.nodePodCidrManager.GetCidr(fornaxnode)
	fornaxnode.Node.Spec.PodCIDR = cidrs[0]
	fornaxnode.Node.Spec.PodCIDRs = cidrs

	daemons := nm.nodeDaemonManager.GetDaemons(fornaxnode)
	fornaxnode.DaemonPods = daemons
	nm.nodes[fornaxutil.UniqueNodeName(node)] = fornaxnode
	return fornaxnode, nil
}

// DeleteNode mark node not schedulable, it got removed from list after a graceful period
func (nm *nodeManager) DeleteNode(node *v1.Node) error {
	fornaxnode, found := nm.nodes[fornaxutil.UniqueNodeName(node)]
	if found {
		fornaxnode.Revision = fornaxnode.Revision + 1
		fornaxnode.State = NodeWorkingStateTerminating
		fornaxnode.Node.Status.Phase = v1.NodeTerminated
		fornaxnode.Node.ObjectMeta.DeletionTimestamp = &metav1.Time{
			Time: time.Now(),
		}
	}

	return nil
}

func (nm *nodeManager) CheckNodeStaleState() {
	staleNodes := nm.staleNodeBucket.getStaleNodes()
	// ask nodes to send full sync request
	for _, node := range staleNodes {
		nm.nodeAgent.SyncNode(node)
	}
}

func (nm *nodeManager) SetPodManager(podmanager fornaxpod.PodManager) {
	nm.podManager = podmanager
}

func (nm *nodeManager) Stop() {
	nm.chQuit <- true
}

func (nm *nodeManager) Run() error {
	go func() {
		for {
			select {
			case <-nm.chQuit:
				// TODO, shutdown more gracefully, handoff fornaxcore primary ownership
				break
			case update := <-nm.nodeUpdates:
				for _, watcher := range nm.watchers {
					watcher <- update
				}
				switch v := update.(type) {
				case *ie.PodUpdate:
					if u, ok := update.(*ie.PodUpdate); ok {
						nm.staleNodeBucket.refreshNode(u.Node)
					}
				case *ie.NodeUpdate:
					if u, ok := update.(*ie.NodeUpdate); ok {
						nm.staleNodeBucket.refreshNode(u.Node)
					}
				default:
					klog.Errorf("unknown node update type %T!\n", v)
				}
				nm.nodeUpdateBucket.appendUpdate(update)
			case <-nm.ticker.C:
				nm.CheckNodeStaleState()
			}
		}
	}()

	return nil
}

func NewNodeManager(checkPeriod time.Duration) *nodeManager {
	bucketA := &store.ArrayBucket{
		Prev:     nil,
		Next:     nil,
		Elements: []interface{}{},
	}
	return &nodeManager{
		RWMutex:     sync.RWMutex{},
		chQuit:      make(chan interface{}),
		nodeUpdates: make(chan interface{}),
		watchers:    []chan<- interface{}{},
		nodeUpdateBucket: &NodeUpdateBucket{
			internalGlobalRevision: 0,
			bucketHead:             bucketA,
			bucketTail:             bucketA,
		},
		nodePodCidrManager: NewPodCidrManager(),
		nodeDaemonManager:  NewNodeDaemonManager(),
		ticker:             time.NewTicker(checkPeriod),
		staleNodeBucket: &StaleNodeBucket{
			RWMutex:       sync.RWMutex{},
			bucketStale:   map[string]*v1.Node{},
			bucketRefresh: map[string]*v1.Node{},
		},
		nodes: map[string]*FornaxNodeWithState{},
	}
}
