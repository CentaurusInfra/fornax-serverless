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
	"sort"
	"sync"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/collection"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type SchedulableNode struct {
	mu                         sync.Mutex
	NodeId                     string
	Node                       *v1.Node
	LastSeen                   time.Time
	LastUsed                   time.Time
	Stat                       ScheduleStat
	ResourceList               v1.ResourceList
	PodPreOccupiedResourceList v1.ResourceList
}

func (snode *SchedulableNode) AdmitPodOccupiedResourceList(resourceList *v1.ResourceList) {
	snode.mu.Lock()
	defer snode.mu.Unlock()
	nodeCpu := snode.PodPreOccupiedResourceList.Cpu().DeepCopy()
	cpu := resourceList.Cpu().DeepCopy()
	nodeCpu.Add(cpu)
	if nodeCpu.Sign() <= 0 {
		nodeCpu.Set(0)
	}
	snode.PodPreOccupiedResourceList[v1.ResourceCPU] = nodeCpu.DeepCopy()

	nodeMemory := snode.PodPreOccupiedResourceList.Memory().DeepCopy()
	memory := resourceList.Memory().DeepCopy()
	nodeMemory.Add(memory)
	if nodeMemory.Sign() <= 0 {
		nodeMemory.Set(0)
	}
	snode.PodPreOccupiedResourceList[v1.ResourceMemory] = nodeMemory

	nodeStorage := snode.PodPreOccupiedResourceList.Storage().DeepCopy()
	storage := resourceList.Storage().DeepCopy()
	nodeStorage.Add(storage)
	if nodeStorage.Sign() <= 0 {
		nodeStorage.Set(0)
	}
	snode.PodPreOccupiedResourceList[v1.ResourceStorage] = nodeStorage
}

func (snode *SchedulableNode) GetAllocatableResources() v1.ResourceList {
	snode.mu.Lock()
	defer snode.mu.Unlock()
	allocatedResources := v1.ResourceList{}

	nodeCpu := snode.ResourceList.Cpu().DeepCopy()
	cpu := snode.PodPreOccupiedResourceList.Cpu().DeepCopy()
	nodeCpu.Sub(cpu)
	if nodeCpu.Sign() <= 0 {
		nodeCpu.Set(0)
	}
	allocatedResources[v1.ResourceCPU] = nodeCpu.DeepCopy()

	nodeMemory := snode.ResourceList.Memory().DeepCopy()
	memory := snode.PodPreOccupiedResourceList.Memory().DeepCopy()
	nodeMemory.Sub(memory)
	if nodeMemory.Sign() <= 0 {
		nodeMemory.Set(0)
	}
	allocatedResources[v1.ResourceMemory] = nodeMemory

	nodeStorage := snode.ResourceList.Storage().DeepCopy()
	storage := snode.PodPreOccupiedResourceList.Storage().DeepCopy()
	nodeStorage.Sub(storage)
	if nodeStorage.Sign() <= 0 {
		nodeStorage.Set(0)
	}
	allocatedResources[v1.ResourceStorage] = nodeStorage

	return allocatedResources
}

func (snode *SchedulableNode) ReleasePodOccupiedResourceList(resourceList *v1.ResourceList) {
	snode.mu.Lock()
	defer snode.mu.Unlock()
	nodeCpu := snode.PodPreOccupiedResourceList.Cpu().DeepCopy()
	cpu := resourceList.Cpu().DeepCopy()
	nodeCpu.Sub(cpu)
	if nodeCpu.Sign() <= 0 {
		nodeCpu.Set(0)
	}
	snode.PodPreOccupiedResourceList[v1.ResourceCPU] = nodeCpu.DeepCopy()

	nodeMemory := snode.PodPreOccupiedResourceList.Memory().DeepCopy()
	memory := resourceList.Memory().DeepCopy()
	nodeMemory.Sub(memory)
	if nodeMemory.Sign() <= 0 {
		nodeMemory.Set(0)
	}
	snode.PodPreOccupiedResourceList[v1.ResourceMemory] = nodeMemory

	nodeStorage := snode.PodPreOccupiedResourceList.Storage().DeepCopy()
	storage := resourceList.Storage().DeepCopy()
	nodeStorage.Sub(storage)
	if nodeStorage.Sign() <= 0 {
		nodeStorage.Set(0)
	}
	snode.PodPreOccupiedResourceList[v1.ResourceStorage] = nodeStorage
}

type SortedNodes struct {
	nodes    []*SchedulableNode
	lessFunc collection.LessFunc
}
type SchedulableNodePool struct {
	mu            sync.Mutex
	nodes         map[string]*SchedulableNode
	sortedNodes   []*SchedulableNode
	sortingMethod NodeSortingMethod
}

func (sn *SortedNodes) Len() int {
	return len(sn.nodes)
}

func (sn *SortedNodes) Less(i, j int) bool {
	return sn.lessFunc(sn.nodes[i], sn.nodes[j])
}

func (sn *SortedNodes) Swap(i, j int) {
	sn.nodes[i], sn.nodes[j] = sn.nodes[j], sn.nodes[i]
}

func (pool *SchedulableNodePool) GetNode(name string) *SchedulableNode {
	if n, f := pool.nodes[name]; f {
		return n
	}
	return nil
}

func (pool *SchedulableNodePool) DeleteNode(name string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.nodes, name)
}

func (pool *SchedulableNodePool) AddNode(name string, node *SchedulableNode) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.nodes[name] = node
}

func (pool *SchedulableNodePool) GetNodes() []*SchedulableNode {
	return pool.sortedNodes
}

func (pool *SchedulableNodePool) SortNodes() {
	klog.InfoS("Resort Scheduleable Node")
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.sort()
	klog.InfoS("Done Resort Scheduleable Node")
}

// use copy on write to build pool.sortedNodes to avoid concurrent map iteration and modification,
// sorting method are like free memory or pods on node
// scheduler use this list to find a number nodes from this list to schedule pods,
// when pods are assigned to nodes or terminated from nodes, pool.sortedNodes are not sorted again,
// this list need to resort, but do not want to resort every time since cow is expensive,
// scheduler control how offen a resort is needed
func (pool *SchedulableNodePool) sort() {
	nodes := []*SchedulableNode{}
	for _, v := range pool.nodes {
		nodes = append(nodes, v)
	}
	sortedNodes := &SortedNodes{
		nodes:    nodes,
		lessFunc: BuildNodeSortingFunc(pool.sortingMethod),
	}
	sort.Sort(sortedNodes)
	pool.sortedNodes = sortedNodes.nodes
}

func (pool *SchedulableNodePool) size() int {
	return len(pool.nodes)
}

func (pool *SchedulableNodePool) printSummary() {
	nodes := pool.nodes
	for _, v := range nodes {
		allocatableList := v.GetAllocatableResources()
		klog.InfoS("Scheduleable node reource", "node", v.NodeId, "capacity", v.ResourceList, "allocatable", allocatableList)
	}
}

type ScheduleStat struct {
	podCreatedIn10Min    int
	podTerminatedIn10Min int
	podFailedIn10Min     int
}

func GetNodeAllocatableResourceList(node *v1.Node) v1.ResourceList {
	resourceList := v1.ResourceList{}
	res := node.Status.Allocatable.DeepCopy()
	if res.Cpu().Sign() > 0 {
		resourceList[v1.ResourceCPU] = *res.Cpu()
	} else {
		resourceList[v1.ResourceCPU] = util.ResourceQuantity(0, v1.ResourceCPU)
	}

	if res.Memory().Sign() > 0 {
		resourceList[v1.ResourceMemory] = *res.Memory()
	} else {
		resourceList[v1.ResourceMemory] = util.ResourceQuantity(0, v1.ResourceMemory)
	}

	if res.Storage().Sign() > 0 {
		resourceList[v1.ResourceStorage] = *res.Storage()
	} else {
		resourceList[v1.ResourceStorage] = util.ResourceQuantity(0, v1.ResourceStorage)
	}

	return resourceList
}
