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
	"math"
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

var DefaultNumOfNodesPerScheduler = 200

type NodeSortingMethod string

const (
	DefaultBackoffRetryDuration                    = 5 * time.Second
	NodeSortingMethodMoreMemory  NodeSortingMethod = "more_memory"   // chose node with more memory
	NodeSortingMethodLessLastUse NodeSortingMethod = "less_last_use" // choose oldest node
	NodeSortingMethodLessUse     NodeSortingMethod = "less_use"      // choose node with less pods
)

var (
	PodIsDeletedError         = fmt.Errorf("pod has a deletion timestamp")
	InsufficientResourceError = fmt.Errorf("can not find node with sufficient resources")
	PodBindToNodeError        = fmt.Errorf("Pod bind to node error")
)

// we want to use more memory node, so, lager value are put ahead in sorted list
func NodeHasMoreMemorySortFunc(pi, pj interface{}) bool {
	piResource := pi.(*SchedulableNode).GetAllocatableResources()
	piResourceV, _ := piResource.Memory().AsInt64()
	pjResource := pj.(*SchedulableNode).GetAllocatableResources()
	pjResourceV, _ := pjResource.Memory().AsInt64()
	return piResourceV > pjResourceV
}

// we want to use less used node, so, smaller value are put ahead in sorted list
func NodeLeastLastUseSortFunc(pi, pj interface{}) bool {
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
		return NodeHasMoreMemorySortFunc
	case NodeSortingMethodLessLastUse:
		return NodeLeastLastUseSortFunc
	default:
		return NodeLeastLastUseSortFunc
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
	nodeUpdateCh              chan *ie.NodeEvent
	podUpdateCh               chan *ie.PodEvent
	nodeInfoP                 ie.NodeInfoProviderInterface
	nodeAgentClient           nodeagent.NodeAgentClient
	scheduleQueue             *PodScheduleQueue
	nodePool                  *SchedulableNodePool
	ScheduleConditionBuilders []ConditionBuildFunc
	policy                    *SchedulePolicy
	schedulers                []*nodeChunkScheduler
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
	klog.InfoS("Bind pod to node", "pod", podName, "node", nodeId, "available resource", snode.GetAllocatableResources())

	resourceList := util.GetPodResourceList(pod)
	snode.AdmitPodOccupiedResourceList(resourceList)
	snode.LastUsed = time.Now()

	// set pod status
	pod.Status.StartTime = util.NewCurrentMetaTime()
	// when pod is scheduled but not returned from node, use it's host ip to help release resource
	pod.Status.HostIP = snode.NodeId
	pod.Status.Message = "Scheduled"
	pod.Status.Reason = "Scheduled"

	// call nodeagent to start pod
	err := ps.nodeAgentClient.CreatePod(nodeId, pod)
	if err != nil {
		klog.ErrorS(err, "Failed to bind pod, reschedule", "node", nodeId, "pod", podName)
		return err
	}

	return nil
}

// remove pod from node resource list
func (ps *podScheduler) unbindNode(node *SchedulableNode, pod *v1.Pod) {
	resourceList := util.GetPodResourceList(pod)
	node.ReleasePodOccupiedResourceList(resourceList)
	pod.Status.StartTime = nil
	pod.Status.HostIP = ""
	pod.Status.Message = "Schedule failed"
	pod.Status.Reason = "Schedule failed"
}

func (ps *podScheduler) schedulePod(pod *v1.Pod, candidateNodes []*SchedulableNode) error {
	st := time.Now().UnixMicro()
	defer func() {
		et := time.Now().UnixMicro()
		klog.InfoS("Schedule pod", "pod", util.Name(pod), "took-micro", et-st)
	}()
	if pod.DeletionTimestamp != nil {
		klog.InfoS("Pod has a deletion timestamp, remove it from queue", "pod", util.Name(pod))
		ps.RemovePod(pod)
		return PodIsDeletedError
	}

	availableNodes := []*SchedulableNode{}
	conditions := CalculateScheduleConditions(ps.ScheduleConditionBuilders, pod)
	for _, node := range candidateNodes {
		allocatedResources := node.GetAllocatableResources()
		goodNode := true
		for _, cond := range conditions {
			goodNode = goodNode && cond.Apply(node, &allocatedResources)
			if !goodNode {
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
		klog.InfoS("Can not find node met condition for pod, come back later", "pod", util.Name(pod), "required resource", util.GetPodResourceList(pod))
		return InsufficientResourceError
	} else {
		// sort candidates to use first one,
		sortedNodes := &SortedNodes{
			nodes:    availableNodes,
			lessFunc: BuildNodeSortingFunc(ps.policy.NodeSortingMethod),
		}
		sort.Sort(sortedNodes)

		var bindError error
		for _, node := range sortedNodes.nodes {
			bindError = ps.bindNode(node, pod)
			if bindError != nil {
				ps.unbindNode(node, pod)
				continue
			}
			break
		}
		if bindError != nil {
			return PodBindToNodeError
		}
	}

	return nil
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

func (ps *podScheduler) updatePodOccupiedResourceList(snode *SchedulableNode, pod *v1.Pod, updateType ie.PodEventType) {
	switch updateType {
	case ie.PodEventTypeDelete, ie.PodEventTypeTerminate:
		resourceList := util.GetPodResourceList(pod)
		snode.ReleasePodOccupiedResourceList(resourceList)
	case ie.PodEventTypeCreate:
		resourceList := util.GetPodResourceList(pod)
		snode.AdmitPodOccupiedResourceList(resourceList)
	}
}

func (ps *podScheduler) printScheduleSummary() {
	activeNum, retryNum := ps.scheduleQueue.Length()
	klog.InfoS("Scheduler summary", "active queue length", activeNum, "backoff queue length", retryNum, "available nodes", ps.nodePool.size(), "schedulers", len(ps.schedulers))
	// ps.nodePool.printSummary()
}

type nodeChunkScheduler struct {
	mu            sync.Mutex
	nodes         []*SchedulableNode
	scheduler     *podScheduler
	sortingMethod NodeSortingMethod
}

// use copy on write to build pool.sortedNodes to avoid concurrent map iteration and modification,
// sorting method are like free memory or pods on node
// scheduler use this list to find a number nodes from this list to schedule pods,
// when pods are assigned to nodes or terminated from nodes, pool.sortedNodes are not sorted again,
// this list need to resort, but do not want to resort every time since cow is expensive,
// scheduler control when a resort is needed
func (cps *nodeChunkScheduler) sortNodes() {
	nodes := []*SchedulableNode{}
	for _, v := range cps.nodes {
		nodes = append(nodes, v)
	}
	sortedNodes := &SortedNodes{
		nodes:    nodes,
		lessFunc: BuildNodeSortingFunc(cps.sortingMethod),
	}
	sort.Sort(sortedNodes)
	cps.nodes = sortedNodes.nodes
}

func (cps *nodeChunkScheduler) schedulePod(pod *v1.Pod) error {
	// lock to avoid overcommit, only one pod can can be scheduled at one time
	cps.mu.Lock()
	defer cps.mu.Unlock()
	return cps.scheduler.schedulePod(pod, cps.nodes)
}

func (ps *podScheduler) initializeChunkSchedulers() {
	numOfNodesPerScheduler := ps.policy.NumOfEvaluatedNodes
	chunkSchedulers := []*nodeChunkScheduler{}
	allNodes := ps.nodePool.GetNodes()
	numOfSchedulers := int(math.Ceil(float64(len(allNodes)) / float64(numOfNodesPerScheduler)))
	for i := 0; i < numOfSchedulers; i++ {
		nodes := allNodes[i*numOfNodesPerScheduler : int(math.Min(float64((i+1)*numOfNodesPerScheduler), float64(len(allNodes))))]
		cs := &nodeChunkScheduler{
			mu:            sync.Mutex{},
			nodes:         nodes,
			scheduler:     ps,
			sortingMethod: ps.policy.NodeSortingMethod,
		}
		cs.sortNodes()
		chunkSchedulers = append(chunkSchedulers, cs)
	}
	ps.schedulers = chunkSchedulers
}

func (ps *podScheduler) Run() {
	klog.Info("starting pod scheduler")
	go func() {
		for {
			if ps.stop {
				break
			} else {
				if len(ps.schedulers) == 0 {
					ps.initializeChunkSchedulers()
				}

				schedulers := ps.schedulers
				if len(schedulers) == 0 {
					// no scheduler, do not poll pods
					time.Sleep(100 * time.Millisecond)
					continue
				}
				// poll at most num of scheduler of pods, schdule them in parallel
				pods := []*v1.Pod{}
				numOfScheduler := len(schedulers)
				for i := 0; i < numOfScheduler; i++ {
					pod := ps.scheduleQueue.NextPod()
					pods = append(pods, pod)
					if !ps.scheduleQueue.HasMore() {
						break
					}
				}
				wg := sync.WaitGroup{}
				for i := 0; i < len(pods); i++ {
					wg.Add(1)
					// every pod start a routing to schedule, every pod start to loop chunck scheduler from a different position, if a it's scheduled, then break loop
					go func(index int) {
						pod := pods[index]
						var schedErr error
						for i := 0; i < numOfScheduler; i++ {
							scheduler := schedulers[(index+i)%numOfScheduler]
							schedErr = scheduler.schedulePod(pod)
							if schedErr == nil {
								break
							}
						}
						if schedErr != nil {
							ps.scheduleQueue.BackoffPod(pod, ps.policy.BackoffDuration)
						}
						wg.Done()
					}(i)
				}
				wg.Wait()
			}
		}
	}()

	// receive pod and node update to update scheduleable node resource and condition
	go func() {
		ticker := time.NewTicker(DefaultBackoffRetryDuration)
		defer func() {
			ps.stop = true
			close(ps.nodeUpdateCh)
			close(ps.podUpdateCh)
			ticker.Stop()
		}()

		// get initial nodes
		nodes := ps.nodeInfoP.List()
		for _, n := range nodes {
			ps.updateNodePool(n.NodeId, n.Node, ie.NodeEventTypeCreate)
		}

		// listen pod and node updates
		for {
			select {
			case <-ps.ctx.Done():
				ps.stop = true
				return
			case update := <-ps.podUpdateCh:
				nodeId := update.NodeId
				if len(nodeId) == 0 {
					nodeId = update.Pod.Status.HostIP
				}
				if len(nodeId) != 0 {
					snode := ps.nodePool.GetNode(nodeId)
					if snode != nil {
						// update resource list only if pod event is from node
						ps.updatePodOccupiedResourceList(snode, update.Pod, update.Type)
					}
				}
			case update := <-ps.nodeUpdateCh:
				oldSize := ps.nodePool.size()
				ps.updateNodePool(update.NodeId, update.Node.DeepCopy(), update.Type)
				newSize := ps.nodePool.size()
				if oldSize != newSize {
					// reinitialization is expensive, but it's ok for now since it only happen when node pool size changed
					ps.initializeChunkSchedulers()
				}
			case <-ticker.C:
				ps.printScheduleSummary()
				// sorting nodes using same node selection logic, move more likely nodes ahead,
				// we may use different sorting interval
				for _, v := range ps.schedulers {
					v.sortNodes()
				}

				// move item from backoff queue to active queue if item exceed cool down time
				if ps.scheduleQueue.backoffRetryQueue.queue.Len() > 0 {
					ps.scheduleQueue.ReviveBackoffItem()
				}
			}
		}
	}()
}

func NewPodScheduler(ctx context.Context, nodeAgent nodeagent.NodeAgentClient, nodeInfoP ie.NodeInfoProviderInterface, podInfoP ie.PodInfoProviderInterface, policy *SchedulePolicy) *podScheduler {
	ps := &podScheduler{
		ctx:             ctx,
		stop:            false,
		nodeUpdateCh:    make(chan *ie.NodeEvent, 100),
		podUpdateCh:     make(chan *ie.PodEvent, 1000),
		nodeInfoP:       nodeInfoP,
		nodeAgentClient: nodeAgent,
		scheduleQueue:   NewScheduleQueue(),
		nodePool: &SchedulableNodePool{
			mu:    sync.RWMutex{},
			nodes: map[string]*SchedulableNode{},
		},
		ScheduleConditionBuilders: []ConditionBuildFunc{
			NewPodCPUCondition,
			NewPodMemoryCondition,
		},
		policy:     policy,
		schedulers: []*nodeChunkScheduler{},
	}
	nodeInfoP.Watch(ps.nodeUpdateCh)
	podInfoP.Watch(ps.podUpdateCh)
	return ps
}
