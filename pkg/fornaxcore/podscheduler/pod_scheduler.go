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

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
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
	DefaultBackoffRetryDuration                    = 5 * time.Second
	NodeSortingMethodMoreMemory  NodeSortingMethod = "more_memory"   // choose node with more memory
	NodeSortingMethodLessLastUse NodeSortingMethod = "less_last_use" // choose node least used before
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
	return pj.(*SchedulableNode).NodeName
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

// pod scheduler group available nodes into multiple groups(100 node a group by default),
// and do parallel scheduling on node groups, one group work for a pod only at a time,
// if can not find available node in a group, pod scheduler move this pod to next group
// node group is rebuild when there are new nodes join or leave
type nodeGroup struct {
	name          string
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
func (cps *nodeGroup) sortNodes() {
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

func (cps *nodeGroup) schedulePod(pod *v1.Pod) error {
	// lock to avoid overcommit, only one pod can can be scheduled at one time
	cps.mu.Lock()
	defer cps.mu.Unlock()
	return cps.scheduler.schedulePod(pod, cps.nodes)
}

// pod scheduler provide function to add/remove pending pod into scheduler queue,
// it watch pod updates and node updates to track node resources, and find and bind available node for a pending pod
type podScheduler struct {
	stop                      bool
	ctx                       context.Context
	nodeUpdateCh              chan *ie.NodeEvent
	podUpdateCh               chan *ie.PodEvent
	nodeInfoLW                ie.NodeInfoLWInterface
	podInfoLW                 ie.PodInfoLWInterface
	nodeAgentClient           nodeagent.NodeAgentClient
	scheduleQueue             *PodScheduleQueue
	nodePool                  *SchedulableNodePool
	ScheduleConditionBuilders []ConditionBuildFunc
	policy                    *SchedulePolicy
	nodeGroups                []*nodeGroup
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

// bind pod to node via nodeAgentClient, add substract pod requested resource from node resource list,
// it substract resource earlier to avoid duplicate use, if any error, unbindNode should release it
func (ps *podScheduler) bindNode(snode *SchedulableNode, pod *v1.Pod) error {
	podName := util.Name(pod)
	nodeId := snode.NodeName
	klog.V(5).InfoS("Bind pod to node", "pod", podName, "node", nodeId, "available resource", snode.GetAllocatableResources())

	resourceList := util.GetPodResourceList(pod)
	snode.AdmitPodOccupiedResourceList(resourceList)
	// set pod status
	pod.Status.StartTime = util.NewCurrentMetaTime()
	// when pod is scheduled but not returned from node, use it's host ip to help release resource
	pod.Status.HostIP = snode.Node.Status.Addresses[0].Address
	pod.Status.Message = "Scheduled"
	pod.Status.Reason = "Scheduled"
	pod.Annotations[fornaxv1.AnnotationFornaxCoreNode] = util.Name(snode.Node)

	// call nodeagent to start pod
	err := ps.nodeAgentClient.CreatePod(nodeId, pod)
	if err != nil {
		klog.ErrorS(err, "Failed to bind pod, reschedule", "node", nodeId, "pod", podName)
		return err
	}
	snode.LastUsed = time.Now()

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
		klog.V(5).InfoS("Done schedule pod", "pod", util.Name(pod), "took-micro", et-st)
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
	}

	if len(availableNodes) == 0 {
		return InsufficientResourceError
	} else {
		// sort candidates to use first one,
		sortedNodes := &SortedNodes{
			nodes:    availableNodes,
			lessFunc: BuildNodeSortingFunc(NodeSortingMethodLessLastUse),
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

func (ps *podScheduler) updateNodePool(v1node *v1.Node, updateType ie.NodeEventType) *SchedulableNode {
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
					NodeName:                   nodeName,
					Node:                       v1node.DeepCopy(),
					LastSeen:                   time.Now(),
					LastUsed:                   time.Now(),
					Stat:                       ScheduleStat{},
					ResourceList:               GetNodeAllocatableResourceList(v1node),
					PodPreOccupiedResourceList: v1.ResourceList{},
				}
				ps.nodePool.AddNode(nodeName, snode)
			}
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
	klog.InfoS("Scheduler summary", "active queue length", activeNum, "backoff queue length", retryNum, "available nodes", ps.nodePool.size(), "node groups", len(ps.nodeGroups))
	// ps.nodePool.printSummary()
}

func (ps *podScheduler) initializeNodeGroups() {
	numOfNodesPerScheduler := ps.policy.NumOfEvaluatedNodes
	nodeGroups := []*nodeGroup{}
	allNodes := ps.nodePool.GetNodes() // maybe shuffle
	numOfSchedulers := int(math.Ceil(float64(len(allNodes)) / float64(numOfNodesPerScheduler)))
	for i := 0; i < numOfSchedulers; i++ {
		nodes := allNodes[i*numOfNodesPerScheduler : int(math.Min(float64((i+1)*numOfNodesPerScheduler), float64(len(allNodes))))]
		cs := &nodeGroup{
			name:          fmt.Sprintf("node_group_%d", i),
			mu:            sync.Mutex{},
			nodes:         nodes,
			scheduler:     ps,
			sortingMethod: ps.policy.NodeSortingMethod,
		}
		nodeGroups = append(nodeGroups, cs)
	}
	ps.nodeGroups = nodeGroups
}

// scheduleLoop poll pods from scheduler queue and loop node groups to find a available node for pods
// scheduleLoop poll multiple pods according num of node groups and assign a pod to a node group and schedule pod in parallel using  goroutines.
// and if a pod can not find node in a group for a pod, go to next node group until all groups are tried
// if pod is not scheduled, put it back to queue for backoff retry
func (ps *podScheduler) scheduleLoop() {
	for {
		if ps.stop {
			break
		} else {
			if len(ps.nodeGroups) == 0 {
				ps.initializeNodeGroups()
			}

			nodeGroups := ps.nodeGroups
			if len(nodeGroups) == 0 {
				// no nodes, do not poll pods
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// poll at most num of scheduler of pods, schdule them in parallel
			pods := []*v1.Pod{}
			numOfScheduler := len(nodeGroups)
			for i := 0; i < numOfScheduler; i++ {
				pod := ps.scheduleQueue.NextPod()
				pods = append(pods, pod)
				if !ps.scheduleQueue.HasMore() {
					break
				}
			}
			wg := sync.WaitGroup{}
			for _, pod := range pods {
				rand.Seed(time.Now().UnixNano())
				i := rand.Intn(len(nodeGroups))
				wg.Add(1)
				go func(pod *v1.Pod, index int) {
					var schedErr error
					for i := 0; i < numOfScheduler; i++ {
						nodeGroup := nodeGroups[(index+i)%numOfScheduler]
						schedErr = nodeGroup.schedulePod(pod)
						if schedErr == nil {
							break
						}
					}
					if schedErr != nil {
						klog.ErrorS(schedErr, "Can not find node for pod, come back later", "pod", util.Name(pod), "required resource", util.GetPodResourceList(pod))
						ps.scheduleQueue.BackoffPod(pod, ps.policy.BackoffDuration)
					}
					wg.Done()
				}(pod, i)
			}
			wg.Wait()
		}
	}
}

func (ps *podScheduler) handleNodeAndPodUpdates() {
	ticker := time.NewTicker(DefaultBackoffRetryDuration)
	defer func() {
		ps.stop = true
		close(ps.nodeUpdateCh)
		close(ps.podUpdateCh)
		ticker.Stop()
	}()

	// listen pod and node updates
	for {
		select {
		case <-ps.ctx.Done():
			ps.stop = true
			return
		case update := <-ps.podUpdateCh:
			nodeId := util.GetPodFornaxNodeIdAnnotation(update.Pod)
			if len(nodeId) != 0 {
				snode := ps.nodePool.GetNode(nodeId)
				if snode != nil {
					// update resource list only if pod event is from node
					ps.updatePodOccupiedResourceList(snode, update.Pod, update.Type)
				}
			}
		case update := <-ps.nodeUpdateCh:
			oldSize := ps.nodePool.size()
			ps.updateNodePool(update.Node.DeepCopy(), update.Type)
			newSize := ps.nodePool.size()
			if oldSize != newSize {
				// reinitialization is expensive, but it's ok for now since it only happen when node pool size changed
				ps.initializeNodeGroups()
			}
		case <-ticker.C:
			ps.printScheduleSummary()
			// move item from backoff queue to active queue if item exceed cool down time
			if ps.scheduleQueue.backoffRetryQueue.queue.Len() > 0 {
				ps.scheduleQueue.ReviveBackoffItem()
			}
		}
	}
}

func (ps *podScheduler) Run() {
	// get initial nodes
	nodes := ps.nodeInfoLW.List()
	for _, n := range nodes {
		ps.updateNodePool(n.Node, ie.NodeEventTypeCreate)
	}
	ps.initializeNodeGroups()

	// receive pod and node update to update scheduleable node resource and condition
	go ps.handleNodeAndPodUpdates()

	// schedule loop
	go ps.scheduleLoop()
}

func NewPodScheduler(ctx context.Context, nodeAgent nodeagent.NodeAgentClient, nodeInfoP ie.NodeInfoLWInterface, podInfoP ie.PodInfoLWInterface, policy *SchedulePolicy) *podScheduler {
	ps := &podScheduler{
		ctx:             ctx,
		stop:            false,
		nodeUpdateCh:    make(chan *ie.NodeEvent, 100),
		podUpdateCh:     make(chan *ie.PodEvent, 1000),
		nodeInfoLW:      nodeInfoP,
		podInfoLW:       podInfoP,
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
		nodeGroups: []*nodeGroup{},
	}
	nodeInfoP.Watch(ps.nodeUpdateCh)
	podInfoP.Watch(ps.podUpdateCh)
	return ps
}
