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

package podscheduler

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/collection"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type PodScheduler interface {
	AddPod(pod *v1.Pod, duration time.Duration)
	RemovePod(pod *v1.Pod)
}

var _ PodScheduler = &podScheduler{}

type NodeSortingMethod string

const (
	DefaultBackoffRetryDuration                    = 10 * time.Second
	NodeSortingMethodMoreMemory  NodeSortingMethod = "more_memory"   // chose node with more memory
	NodeSortingMethodLessLastUse NodeSortingMethod = "less_last_use" // choose oldest node
	NodeSortingMethodLessUse     NodeSortingMethod = "less_use"      // choose node with less pods
)

func MoreNodeMemorySortFunc(pi, pj interface{}) bool {
	piResource := pi.(*SchedulableNode).GetAllocatableResources()
	piResourceV, _ := piResource.Memory().AsInt64()
	pjResource := pj.(*SchedulableNode).GetAllocatableResources()
	pjResourceV, _ := pjResource.Memory().AsInt64()
	return piResourceV > pjResourceV
}

func LessNodeLastUseSortFunc(pi, pj interface{}) bool {
	piLastUsed := pi.(*SchedulableNode).LastUsed.UnixNano()
	pjLastUsed := pj.(*SchedulableNode).LastUsed.UnixNano()
	return piLastUsed < pjLastUsed
}

func NodeNameKeyFunc(pj interface{}) string {
	return pj.(*SchedulableNode).NodeId
}

func BuildNodeSortingFunc(sortingMethod NodeSortingMethod) collection.LessFunc {
	switch sortingMethod {
	case NodeSortingMethodMoreMemory:
		return MoreNodeMemorySortFunc
	case NodeSortingMethodLessLastUse:
		return LessNodeLastUseSortFunc
	default:
		return LessNodeLastUseSortFunc
	}
}

type SchedulePolicy struct {
	NumOfEvaluatedNodes int
	BackoffDuration     time.Duration
	NodeSortingMethod   NodeSortingMethod
}

type podScheduler struct {
	stop                      bool
	ctx                       context.Context
	updateCh                  chan interface{}
	nodeInfoP                 ie.NodeInfoProvider
	nodeAgentClient           nodeagent.NodeAgentClient
	scheduleQueue             *PodScheduleQueue
	nodePool                  *SchedulableNodePool
	ScheduleConditionBuilders []ConditionBuildFunc
	policy                    *SchedulePolicy
}

// RemovePod remove a pod from scheduling queue
func (ps *podScheduler) RemovePod(pod *v1.Pod) {
	ps.scheduleQueue.RemovePod(pod)
}

// AddPod add a pod into scheduling active queue, if there is a existing one with same name, replace it
func (ps *podScheduler) AddPod(pod *v1.Pod, duration time.Duration) {
	ps.scheduleQueue.AddPod(pod, duration)
}

func (ps *podScheduler) calcScore(node *SchedulableNode, conditions []ScheduleCondition) int {
	allocatedResources := node.GetAllocatableResources()
	score := 0
	for _, v := range conditions {
		score += int(v.Score(node, &allocatedResources))
	}

	return score
}

func (ps *podScheduler) scoreNode(pod *v1.Pod, nodes []*SchedulableNode) *SchedulableNode {
	maxScore := -1
	var bestNode *SchedulableNode
	conditions := CalculateScheduleConditions(ps.ScheduleConditionBuilders, pod)
	for _, node := range nodes {
		score := ps.calcScore(node, conditions)
		if score > maxScore {
			bestNode = node
		}
	}

	return bestNode
}
func (ps *podScheduler) selectNode(pod *v1.Pod, nodes []*SchedulableNode) *SchedulableNode {
	// randomly pickup one
	no := rand.Intn(len(nodes))
	return nodes[no]
}

// add pod into node resource list, and send pod to node via grpc channel, if it channel failed, reschedule
func (ps *podScheduler) bindNode(snode *SchedulableNode, pod *v1.Pod) error {
	podName := util.Name(pod)
	nodeId := snode.NodeId
	klog.InfoS("Bind pod to node", "pod", podName, "node", nodeId)

	resourceList := util.GetPodResourceList(pod)
	snode.AdmitPodOccupiedResourceList(resourceList)
	snode.LastUsed = time.Now()

	// set pod status
	pod.Status.HostIP = snode.Node.Status.Addresses[0].Address
	pod.Status.Message = "Scheduled"
	pod.Status.Reason = "Scheduled"

	// call nodeagent to start pod
	err := ps.nodeAgentClient.CreatePod(nodeId, pod)
	if err != nil {
		klog.ErrorS(err, "Failed to bind pod, reschedule", "node", nodeId, "pod", podName)
		ps.unbindNode(snode, pod)
		pod.Status.Message = "Schedule failed"
		pod.Status.Reason = "Schedule failed"
		return err
	}

	return nil
}

// remove pod from node resource list
func (ps *podScheduler) unbindNode(node *SchedulableNode, pod *v1.Pod) {
	resourceList := util.GetPodResourceList(pod)
	node.ReleasePodOccupiedResourceList(resourceList)
	pod.Status.HostIP = ""
}

func (ps *podScheduler) schedulePod(pod *v1.Pod) {
	podName := util.Name(pod)
	klog.InfoS("Schedule a pod", "pod", podName)
	if pod.DeletionTimestamp != nil {
		// pod has been requested to delete, skip
		return
	}

	conditions := CalculateScheduleConditions(ps.ScheduleConditionBuilders, pod)
	availableNodes := []*SchedulableNode{}
	nodes := ps.nodePool.GetNodes()
	for _, node := range nodes {
		allocatedResources := node.GetAllocatableResources()
		goodNode := true
		for _, cond := range conditions {
			goodNode = goodNode && cond.Apply(node, &allocatedResources)
			if !goodNode {
				klog.InfoS("Schedule condition does not meet", "condition", cond)
				break
			}
		}

		if goodNode {
			availableNodes = append(availableNodes, node)
		}

		if len(availableNodes) >= ps.policy.NumOfEvaluatedNodes {
			break
		}
	}

	if len(availableNodes) == 0 {
		// send pod to retry backoff queue
		klog.InfoS("Can not find a node candidate for scheduling pod, retry later", "pod", podName)
		ps.scheduleQueue.BackoffPod(pod, ps.policy.BackoffDuration)
	} else {
		// sort candidates to use first one, eg more memory or least used
		sortedNodes := &SortedNodes{
			nodes:    availableNodes,
			lessFunc: BuildNodeSortingFunc(ps.policy.NodeSortingMethod),
		}
		sort.Sort(sortedNodes)
		node := sortedNodes.nodes[0]
		fmt.Println(node.LastUsed.UnixNano())
		if node == nil {
			klog.InfoS("All node candidate do not met requirement, retry later", "pod", podName)
			ps.scheduleQueue.BackoffPod(pod, ps.policy.BackoffDuration)
		} else {
			err := ps.bindNode(node, pod)
			if err != nil {
				ps.scheduleQueue.BackoffPod(pod, ps.policy.BackoffDuration)
			}
		}
	}
}

func (ps *podScheduler) updateNodePool(nodeId string, v1node *v1.Node, updateType ie.NodeEventType) *SchedulableNode {
	nodeName := util.Name(v1node)
	if updateType == ie.NodeEventTypeDelete {
		ps.nodePool.DeleteNode(nodeName)
		return nil
	} else {
		if snode := ps.nodePool.GetNode(nodeName); snode != nil {
			snode.LastSeen = time.Now()
			if !util.IsNodeRunning(v1node) {
				ps.nodePool.DeleteNode(nodeName)
			}
			return snode
		} else {
			// only add ready node into scheduleable node list
			if util.IsNodeRunning(v1node) {
				snode := &SchedulableNode{
					mu:                         sync.Mutex{},
					NodeId:                     nodeId,
					Node:                       v1node.DeepCopy(),
					LastSeen:                   time.Now(),
					LastUsed:                   time.Now(),
					Stat:                       ScheduleStat{},
					ResourceList:               GetNodeAllocatableResourceList(v1node),
					PodPreOccupiedResourceList: v1.ResourceList{},
				}
				ps.nodePool.AddNode(nodeName, snode)
			}
			// TODO, if there are pod in backoff queue with similar resource req, notify to try schedule

			return snode
		}
	}
}

func (ps *podScheduler) updatePodOccupiedResourceList(snode *SchedulableNode, v1pod *v1.Pod, updateType ie.PodEventType) {
	switch updateType {
	case ie.PodEventTypeDelete, ie.PodEventTypeTerminate:
		// if pod deleted or disappear, remove it from preoccupied list
		resourceList := util.GetPodResourceList(v1pod)
		snode.ReleasePodOccupiedResourceList(resourceList)
		// TODO, if there are pod in backoff queue with similar resource req, notify to try schedule
	case ie.PodEventTypeCreate:
		resourceList := util.GetPodResourceList(v1pod)
		snode.AdmitPodOccupiedResourceList(resourceList)
	}
}

func (ps *podScheduler) printScheduleSummary() {
	activeNum, retryNum := ps.scheduleQueue.Length()
	klog.InfoS("Scheduler summary", "active queue length", activeNum, "backoff queue length", retryNum, "available nodes", ps.nodePool.size())
	// ps.nodePool.printSummary()
}

func (ps *podScheduler) Run() {
	klog.Info("starting pod scheduler")
	go func() {
		for {
			if ps.stop {
				break
			} else {
				// check active qeueue item, and schedule pod, we want to avoid resource conflict,
				// use single routine to do real schedule, schedulePod must be very fast
				pod := ps.scheduleQueue.NextPod()
				if pod != nil {
					ps.schedulePod(pod)
				}
			}
		}
	}()

	// receive pod and node update to update scheduleable node resource and condition
	go func() {
		ticker := time.NewTicker(DefaultBackoffRetryDuration)
		nodes := ps.nodeInfoP.List()
		for _, n := range nodes {
			ps.updateNodePool(n.NodeId, n.Node, ie.NodeEventTypeCreate)
		}
		for {
			select {
			case <-ps.ctx.Done():
				ps.stop = true
				break
			case update := <-ps.updateCh:
				switch t := update.(type) {
				case *ie.PodEvent:
					if u, ok := update.(*ie.PodEvent); ok {
						if len(u.NodeId) != 0 {
							snode := ps.nodePool.GetNode(u.NodeId)
							if snode != nil {
								ps.updatePodOccupiedResourceList(snode, u.Pod, u.Type)
							}
						}
					}
				case *ie.NodeEvent:
					if u, ok := update.(*ie.NodeEvent); ok {
						ps.updateNodePool(u.NodeId, u.Node.DeepCopy(), u.Type)
					}
					ps.scheduleQueue.ReviveBackoffItem()
				default:
					klog.Errorf("unknown node update type %T!\n", t)
				}
			case <-ticker.C:
				ps.printScheduleSummary()
				// we may use different sorting interval
				ps.nodePool.SortNodes()

				// move item from backoff queue to active queue if item exceed cool down time
				if ps.scheduleQueue.backoffRetryQueue.queue.Len() > 0 {
					ps.scheduleQueue.ReviveBackoffItem()
				}
			}
		}
	}()
}

func NewPodScheduler(ctx context.Context, nodeAgent nodeagent.NodeAgentClient, nodeInfoP ie.NodeInfoProvider, podInfoP ie.PodInfoProvider, policy *SchedulePolicy) *podScheduler {
	ps := &podScheduler{
		ctx:             ctx,
		stop:            false,
		updateCh:        make(chan interface{}, 500),
		nodeInfoP:       nodeInfoP,
		nodeAgentClient: nodeAgent,
		scheduleQueue:   NewScheduleQueue(),
		nodePool: &SchedulableNodePool{
			mu:            sync.Mutex{},
			nodes:         map[string]*SchedulableNode{},
			sortedNodes:   []*SchedulableNode{},
			sortingMethod: policy.NodeSortingMethod,
		},
		ScheduleConditionBuilders: []ConditionBuildFunc{
			NewPodCPUCondition,
			NewPodMemoryCondition,
		},
		policy: policy,
	}
	nodeInfoP.Watch(ps.updateCh)
	podInfoP.Watch(ps.updateCh)
	return ps
}
