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
	"sync"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
)

type SchedulableNode struct {
	mu                         sync.Mutex
	Node                       *v1.Node
	LastSeen                   time.Time
	LastUsed                   *time.Time
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

func (snode *SchedulableNode) getAllocatableResources() v1.ResourceList {
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

type SchedulableNodePool struct {
	mu          sync.Mutex
	nodes       map[string]*SchedulableNode
	sortedNodes []*SchedulableNode
}

func (pool *SchedulableNodePool) getNode(name string) *SchedulableNode {
	if n, f := pool.nodes[name]; f {
		return n
	}
	return nil
}

func (pool *SchedulableNodePool) deleteNode(name string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.nodes, name)
	pool.cow()
}

func (pool *SchedulableNodePool) addNode(name string, node *SchedulableNode) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.nodes[name] = node
	pool.cow()
}

// cow us copy on write to avoid concurrent map iteration and modification
func (pool *SchedulableNodePool) cow() {
	// TODO add real sorting logic
	sortedNodes := []*SchedulableNode{}
	for _, v := range pool.nodes {
		sortedNodes = append(sortedNodes, v)
	}
	pool.sortedNodes = sortedNodes
}

func (pool *SchedulableNodePool) size() int {
	return len(pool.nodes)
}

func (pool *SchedulableNodePool) GetNodes() []*SchedulableNode {
	return pool.sortedNodes
}

func (pool *SchedulableNodePool) printSummary() {
	// debug node pools
	// klog.InfoS("Scheduleable Node summary")
	// nodes := pool.nodes
	// for _, v := range nodes {
	//  allocatableList := v.getAllocatableResources()
	//  klog.InfoS("node reource list", "capacity", v.ResourceList, "allocatable", allocatableList)
	// }
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
