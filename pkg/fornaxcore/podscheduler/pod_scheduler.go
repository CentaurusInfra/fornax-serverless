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
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxutil "centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type PodScheduler interface {
	AddPod(pod *v1.Pod, duration time.Duration)
	RemovePod(pod *v1.Pod)
}

var _ PodScheduler = &podScheduler{}

type SchedulePolicy struct {
	NumOfEvaluatedNodes int
	BackoffDuration     time.Duration
}

type podScheduler struct {
	nodeUpdateCh              chan interface{}
	nodeInforProvider         ie.NodeInfoProvider
	nodeAgent                 nodeagent.NodeAgentProxy
	scheduleQueue             PodScheduleQueue
	nodePool                  NodePool
	ScheduleConditionBuilders map[string]ConditionBuildFunc
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
	allocatedResources := ps.getAllocatableResources(node)
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
func (ps *podScheduler) bindNode(fornaxNode *SchedulableNode, pod *v1.Pod) *SchedulableNode {
	resourceList := fornaxutil.GetPodResource(pod)
	//pod is removed from PodPreOccupiedResourceList when node received pod failed or running state
	fornaxNode.PodPreOccupiedResourceList[fornaxutil.UniquePodName(pod)] = resourceList.DeepCopy()

	// set pod status
	pod.Status.HostIP = fornaxNode.Node.Status.Addresses[0].Address
	pod.Status.Phase = v1.PodPending
	pod.Status.Message = "Scheduled"
	pod.Status.Reason = "Scheduled"

	pod.ObjectMeta.Labels[fornaxv1.LabelFornaxCoreNode] = fornaxNode.Node.Name
	// call nodeagent to start pod
	err := ps.nodeAgent.CreatePod(fornaxutil.UniqueNodeName(fornaxNode.Node), pod)
	if err != nil {
		klog.ErrorS(err, "Failed to bind pod, reschedule", "node", fornaxNode, "pod", pod)
	} else {
		// set pod status
		pod.Status.HostIP = ""
		pod.Status.Phase = v1.PodPending
		pod.Status.Message = "Schedule failed"
		pod.Status.Reason = "Schedule failed"
		delete(pod.ObjectMeta.Labels, fornaxv1.LabelFornaxCoreNode)
	}

	return nil
}

// remove pod from node resource list when pod is terminated or failed
func (ps *podScheduler) unbindNode(node *SchedulableNode, pod *v1.Pod) *SchedulableNode {
	delete(node.PodPreOccupiedResourceList, fornaxutil.UniquePodName(pod))
	pod.Status.HostIP = ""
	return node
}

// getAllocatableResources return node resourceList - sum(preoccupied pod resources)
func (ps *podScheduler) getAllocatableResources(node *SchedulableNode) v1.ResourceList {
	allocatedResources := v1.ResourceList{}

	nodeCpu := node.ResourceList.Cpu().DeepCopy()
	for _, v := range node.PodPreOccupiedResourceList {
		cpu := v.Cpu()
		nodeCpu.Sub(*cpu)
		if nodeCpu.Sign() <= 0 {
			nodeCpu.Set(0)
			break
		}
	}
	allocatedResources[v1.ResourceCPU] = nodeCpu.DeepCopy()

	nodeMemory := node.ResourceList.Memory().DeepCopy()
	for _, v := range node.PodPreOccupiedResourceList {
		memory := v.Memory()
		nodeMemory.Sub(*memory)
		if nodeMemory.Sign() <= 0 {
			nodeMemory.Set(0)
			break
		}
	}
	allocatedResources[v1.ResourceMemory] = nodeMemory

	nodeStorage := node.ResourceList.Storage().DeepCopy()
	for _, v := range node.PodPreOccupiedResourceList {
		storage := v.Storage()
		nodeStorage.Sub(*storage)
		if nodeStorage.Sign() <= 0 {
			nodeStorage.Set(0)
			break
		}
	}
	allocatedResources[v1.ResourceStorage] = nodeStorage

	return allocatedResources
}

func (ps *podScheduler) schedulePod(pod *v1.Pod) {
	// get pod schedule condition
	// 1/ resource, cpu, mem, volume, pid
	// 2/ other pod conditions, recent schedule usage, node affinity, name,
	//
	// go through nodes to find suitable node
	// node pool is a sorted list by conditions

	conditions := CalculateScheduleConditions(ps.ScheduleConditionBuilders, pod)
	availableNodes := []*SchedulableNode{}
	for _, node := range ps.nodePool {
		allocatedResources := ps.getAllocatableResources(node)
		good := true
		for _, cond := range conditions {
			klog.InfoS("Checking schedule condition", "condition", cond)
			good = good && cond.Apply(node, &allocatedResources)
			if !good {
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

	// send pod to retry pool for backoff retry
	if len(availableNodes) == 0 {
		ps.scheduleQueue.RetryPod(pod, ps.policy.BackoffDuration)
	}

	// select highest score node
	node := ps.selectNode(pod, availableNodes)

	// send to selected node
	ps.bindNode(node, pod)
}

func (ps *podScheduler) updateNodePool(v1node *v1.Node, updateType ie.NodeUpdateType) *SchedulableNode {
	if snode, found := ps.nodePool[fornaxutil.UniqueNodeName(v1node)]; found {
		snode.LastSeen = time.Now()
		//TODO, update node resoruce list, it should not change after
		return snode
	} else {
		snode := &SchedulableNode{
			Node:                       v1node.DeepCopy(),
			LastSeen:                   time.Now(),
			LastUsed:                   nil,
			ResourceList:               GetNodeAllocatableResourceList(v1node),
			PodPreOccupiedResourceList: map[string]v1.ResourceList{},
		}
		ps.nodePool[fornaxutil.UniqueNodeName(v1node)] = snode
		// TODO, if there are pod in backoff queue with similar resource req, notify to try schedule

		return snode
	}
}

func (ps *podScheduler) updatePodPreOccupiedResourceList(snode *SchedulableNode, v1pod *v1.Pod, updateType ie.PodUpdateType) {
	switch updateType {
	case ie.PodUpdateTypeDelete, ie.PodUpdateTypeTerminate:
		// if pod deleted or disappear, remove it from preoccupied list
		delete(snode.PodPreOccupiedResourceList, fornaxutil.UniquePodName(v1pod))
		// TODO, if there are pod in backoff queue with similar resource req, notify to try schedule
	case ie.PodUpdateTypeCreate, ie.PodUpdateTypeUpdate:
		if _, found := snode.PodPreOccupiedResourceList[fornaxutil.UniquePodName(v1pod)]; found {
			// TODO, update pod resource list?
		} else {
			// somehow this pod is not in node occupied pod resource list, add it back
			res := fornaxutil.GetPodResource(v1pod)
			snode.PodPreOccupiedResourceList[fornaxutil.UniquePodName(v1pod)] = res.DeepCopy()
		}
	}
}

func (ps *podScheduler) Run() {
	go func() {
		for {
			pod := ps.scheduleQueue.NextPod()
			if pod != nil {
				ps.schedulePod(pod)
			}
		}
	}()

	go func() {
		nodes := ps.nodeInforProvider.ListNodes()
		for _, n := range nodes {
			ps.updateNodePool(n, ie.NodeUpdateTypeCreate)
		}
		for {
			select {
			case update := <-ps.nodeUpdateCh:
				switch t := update.(type) {
				case *ie.PodUpdate:
					if u, ok := update.(*ie.PodUpdate); ok {
						snode := ps.updateNodePool(u.Node, ie.NodeUpdateTypeUpdate)
						ps.updatePodPreOccupiedResourceList(snode, u.Pod, u.Update)
					}
				case *ie.NodeUpdate:
					if u, ok := update.(*ie.NodeUpdate); ok {
						ps.updateNodePool(u.Node, u.Update)
					}
				default:
					klog.Errorf("unknown node update type %T!\n", t)
				}
			}
		}
	}()
}

func NewPodScheduler(nodeAgent nodeagent.NodeAgentProxy, nodeInforProvider ie.NodeInfoProvider) *podScheduler {
	ps := &podScheduler{
		nodeUpdateCh:              make(chan interface{}),
		nodeInforProvider:         nodeInforProvider,
		nodeAgent:                 nodeAgent,
		scheduleQueue:             NewScheduleQueue(),
		nodePool:                  map[string]*SchedulableNode{},
		ScheduleConditionBuilders: map[string]ConditionBuildFunc{},
		policy: SchedulePolicy{
			NumOfEvaluatedNodes: 10,
			BackoffDuration:     10 * time.Second,
		},
	}
	ps.nodeInforProvider.WatchNode(ps.nodeUpdateCh)
	return ps
}
