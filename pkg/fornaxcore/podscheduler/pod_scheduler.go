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
	"sync"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	fornaxutil "centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type PodScheduler interface {
	AddPod(pod *v1.Pod, duration time.Duration)
	RemovePod(pod *v1.Pod)
}

var _ PodScheduler = &podScheduler{}

const (
	DefaultBackoffRetryDuration = 10 * time.Second
)

type SchedulePolicy struct {
	NumOfEvaluatedNodes int
	BackoffDuration     time.Duration
}

type podScheduler struct {
	stop                      bool
	ctx                       context.Context
	nodeUpdateCh              chan interface{}
	nodeInforProvider         ie.NodeInfoProvider
	nodeAgent                 nodeagent.NodeAgentProxy
	scheduleQueue             *PodScheduleQueue
	nodePool                  SchedulableNodePool
	ScheduleConditionBuilders []ConditionBuildFunc
	policy                    SchedulePolicy
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
	allocatedResources := node.getAllocatableResources()
	score := 0
	for _, v := range conditions {
		score += int(v.Score(node, &allocatedResources))
	}

	return score
}

func (ps *podScheduler) selectNode(pod *v1.Pod, nodes []*SchedulableNode) *SchedulableNode {
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

// add pod into node resource list, and send pod to node via grpc channel, if it channel failed, reschedule
func (ps *podScheduler) bindNode(snode *SchedulableNode, pod *v1.Pod) error {
	klog.InfoS("Bind pod to node", "pod", util.UniquePodName(pod), "node", util.UniqueNodeName(snode.Node))

	resourceList := fornaxutil.GetPodResourceList(pod)
	snode.AdmitPodOccupiedResourceList(resourceList)

	// set pod status
	pod.Status.HostIP = snode.Node.Status.Addresses[0].Address
	pod.Status.Phase = v1.PodPending
	pod.Status.Message = "Scheduled"
	pod.Status.Reason = "Scheduled"

	// call nodeagent to start pod
	err := ps.nodeAgent.CreatePod(fornaxutil.UniqueNodeName(snode.Node), pod)
	if err != nil {
		klog.ErrorS(err, "Failed to bind pod, reschedule", "node", snode, "pod", pod)
		ps.unbindNode(snode, pod)
		pod.Status.Phase = v1.PodPending
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
	klog.InfoS("Schedule a pod", "pod", util.UniquePodName(pod))

	// get pod schedule condition
	// 1/ resource, cpu, mem, volume, pid
	// 2/ other pod conditions, recent schedule usage, node affinity, name,
	//
	// go through nodes to find suitable node
	// node pool is a sorted list by conditions

	conditions := CalculateScheduleConditions(ps.ScheduleConditionBuilders, pod)
	availableNodes := []*SchedulableNode{}
	nodes := ps.nodePool
	for _, node := range nodes {
		allocatedResources := node.getAllocatableResources()
		good := true
		for _, cond := range conditions {
			good = good && cond.Apply(node, &allocatedResources)
			if !good {
				klog.InfoS("Schedule condition does not meet", "condition", cond)
				break
			}
		}

		if good {
			availableNodes = append(availableNodes, node)
		}
		if len(availableNodes) >= ps.policy.NumOfEvaluatedNodes {
			break
		}
	}

	// send pod to retry backoff queue
	if len(availableNodes) == 0 {
		klog.InfoS("Can not find a node candidate for scheduling pod, retry later", "pod", util.UniquePodName(pod))
		ps.scheduleQueue.BackoffPod(pod, ps.policy.BackoffDuration)
	} else {
		// select highest score node
		node := ps.selectNode(pod, availableNodes)
		if node == nil {
			klog.InfoS("All node candidate do not met requirement, retry later", "pod", util.UniquePodName(pod))
			ps.scheduleQueue.BackoffPod(pod, ps.policy.BackoffDuration)
		}

		// send to selected node
		ps.bindNode(node, pod)
	}
}

func (ps *podScheduler) updateNodePool(v1node *v1.Node, updateType ie.NodeUpdateType) *SchedulableNode {
	nodeName := fornaxutil.UniqueNodeName(v1node)
	if updateType == ie.NodeUpdateTypeDelete {
		delete(ps.nodePool, nodeName)
		return nil
	} else {
		if snode, found := ps.nodePool[fornaxutil.UniqueNodeName(v1node)]; found {
			snode.LastSeen = time.Now()
			if !util.IsNodeRunning(v1node) {
				delete(ps.nodePool, nodeName)
			}
			return snode
		} else {
			// only add ready node into scheduleable node list
			if util.IsNodeRunning(v1node) {
				snode := &SchedulableNode{
					mu:                         sync.Mutex{},
					Node:                       v1node.DeepCopy(),
					LastSeen:                   time.Now(),
					LastUsed:                   nil,
					Stat:                       ScheduleStat{},
					ResourceList:               GetNodeAllocatableResourceList(v1node),
					PodPreOccupiedResourceList: v1.ResourceList{},
				}
				ps.nodePool[nodeName] = snode
			}
			// TODO, if there are pod in backoff queue with similar resource req, notify to try schedule

			return snode
		}
	}
}

func (ps *podScheduler) updatePodOccupiedResourceList(snode *SchedulableNode, v1pod *v1.Pod, updateType ie.PodUpdateType) {
	switch updateType {
	case ie.PodUpdateTypeDelete, ie.PodUpdateTypeTerminate:
		// if pod deleted or disappear, remove it from preoccupied list
		resourceList := util.GetPodResourceList(v1pod)
		snode.ReleasePodOccupiedResourceList(resourceList)
		// TODO, if there are pod in backoff queue with similar resource req, notify to try schedule
	case ie.PodUpdateTypeCreate:
		resourceList := util.GetPodResourceList(v1pod)
		snode.AdmitPodOccupiedResourceList(resourceList)
	}
}

func (ps *podScheduler) printScheduleSummary() {
	activeNum, retryNum := ps.scheduleQueue.Length()
	klog.InfoS("Scheduler summary", "active queue length", activeNum, "backoff queue length", retryNum, "available nodes", len(ps.nodePool))
	// debug node pools
	// pools := ps.nodePool
	// for _, v := range pools {
	//  klog.InfoS("node available reource", "resourceList", v.Node.Status.Allocatable)
	//  resourceList := ps.getAllocatableResources(v)
	//  klog.InfoS("node allocatel reource", "resourceList", resourceList)
	// }
}

func (ps *podScheduler) Run() {
	// check active qeueue item, and schedule pod
	go func() {
		for {
			if ps.stop {
				break
			} else {
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
		nodes := ps.nodeInforProvider.ListNodes()
		for _, n := range nodes {
			ps.updateNodePool(n, ie.NodeUpdateTypeCreate)
		}
		for {
			select {
			case <-ps.ctx.Done():
				ps.stop = true
				break
			case update := <-ps.nodeUpdateCh:
				switch t := update.(type) {
				case *ie.PodUpdate:
					if u, ok := update.(*ie.PodUpdate); ok {
						snode := ps.updateNodePool(u.Node, ie.NodeUpdateTypeUpdate)
						if snode != nil {
							ps.updatePodOccupiedResourceList(snode, u.Pod, u.Update)
						}
					}
				case *ie.NodeUpdate:
					if u, ok := update.(*ie.NodeUpdate); ok {
						ps.updateNodePool(u.Node, u.Update)
					}
					ps.scheduleQueue.ReviveBackoffItem()
				default:
					klog.Errorf("unknown node update type %T!\n", t)
				}
			case <-ticker.C:
				// check backoff queue, move item to active queue if item exceed cool down time
				ps.printScheduleSummary()
				if ps.scheduleQueue.backoffRetryQueue.queue.Len() > 0 {
					klog.Info("check backoff schedule item and revive mature itemm")
					ps.scheduleQueue.ReviveBackoffItem()
				}
			}
		}
	}()
}

func NewPodScheduler(ctx context.Context, nodeAgent nodeagent.NodeAgentProxy, nodeInforProvider ie.NodeInfoProvider) *podScheduler {
	ps := &podScheduler{
		ctx:               ctx,
		stop:              false,
		nodeUpdateCh:      make(chan interface{}, 100),
		nodeInforProvider: nodeInforProvider,
		nodeAgent:         nodeAgent,
		scheduleQueue:     NewScheduleQueue(),
		nodePool:          map[string]*SchedulableNode{},
		ScheduleConditionBuilders: []ConditionBuildFunc{
			NewPodCPUCondition,
			NewPodMemoryCondition,
		},
		policy: SchedulePolicy{
			NumOfEvaluatedNodes: 10,
			BackoffDuration:     10 * time.Second,
		},
	}
	ps.nodeInforProvider.WatchNode(ps.nodeUpdateCh)
	return ps
}
