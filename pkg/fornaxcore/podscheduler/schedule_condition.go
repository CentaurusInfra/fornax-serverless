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
	"fmt"

	podutil "centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	MinimumCpuRequestQuantity    = podutil.ResourceQuantity(10000, v1.ResourceCPU)
	MinimumMemoryRequestQuantity = podutil.ResourceQuantity(50000, v1.ResourceMemory)
)

type ScheduleCondition interface {
	Mandatory() bool
	Apply(node *SchedulableNode, allocatableResourceList *v1.ResourceList) bool
	Score(node *SchedulableNode, allocatableResourceList *v1.ResourceList) int64
}

type ConditionBuildFunc func(*v1.Pod) ScheduleCondition

func CalculateScheduleConditions(condBuildFuncs []ConditionBuildFunc, pod *v1.Pod) []ScheduleCondition {
	conditions := []ScheduleCondition{}
	for _, v := range condBuildFuncs {
		condition := v(pod)
		conditions = append(conditions, condition)
	}
	return conditions
}

var _ ScheduleCondition = &CPUCondition{}

type CPUCondition struct {
	Name             string
	ResourceQuantity resource.Quantity
}

// Mandatory implements ScheduleCondition
func (*CPUCondition) Mandatory() bool {
	return true
}

// check if node stastify cpu requirement
func (cond *CPUCondition) Apply(node *SchedulableNode, allocatableResourceList *v1.ResourceList) bool {
	cpu := allocatableResourceList.Cpu()
	fmt.Println(*cpu)
	fmt.Println(cond.ResourceQuantity)
	return cpu.Sign() > 0 && cpu.Cmp(cond.ResourceQuantity) > 0
}

// calc score of cpu condition
func (cond *CPUCondition) Score(node *SchedulableNode, allocatableResourceList *v1.ResourceList) int64 {
	cpu := *allocatableResourceList.Cpu()
	cpu.Sub(cond.ResourceQuantity)
	return cpu.ScaledValue(100)
}

func NewPodCPUCondition(pod *v1.Pod) ScheduleCondition {
	resource := podutil.GetPodResourceList(pod).Cpu()
	if resource.Sign() > 0 {
		return &CPUCondition{
			Name:             "CPU",
			ResourceQuantity: *resource,
		}
	} else {
		return &CPUCondition{
			Name:             "CPU",
			ResourceQuantity: MinimumCpuRequestQuantity,
		}
	}
}

type MemoryCondition struct {
	Name             string
	ResourceQuantity resource.Quantity
}

// Mandatory of memory condition, true always
func (*MemoryCondition) Mandatory() bool {
	return true
}

// check if node stastify memory requirement
func (cond *MemoryCondition) Apply(node *SchedulableNode, allocatableResourceList *v1.ResourceList) bool {
	memory := allocatableResourceList.Memory()
	fmt.Println(*memory)
	fmt.Println(cond.ResourceQuantity)
	return memory.Sign() > 0 && memory.Cmp(cond.ResourceQuantity) > 0
}

// calc score of memory condition
func (cond *MemoryCondition) Score(node *SchedulableNode, allocatableResourceList *v1.ResourceList) int64 {
	memory := *allocatableResourceList.Memory()
	memory.Sub(cond.ResourceQuantity)
	return memory.ScaledValue(100)
}

func NewPodMemoryCondition(pod *v1.Pod) ScheduleCondition {
	resource := podutil.GetPodResourceList(pod).Memory()
	if resource.Sign() > 0 {
		return &MemoryCondition{
			Name:             "Memory",
			ResourceQuantity: *resource,
		}
	} else {
		return &MemoryCondition{
			Name:             "Memory",
			ResourceQuantity: MinimumMemoryRequestQuantity,
		}
	}
}

type StorageCondition struct {
	Name             string
	ResourceQuantity resource.Quantity
}

// Mandatory implements ScheduleCondition
func (*StorageCondition) Mandatory() bool {
	return false
}

// check if node stastify storage requirement
func (cond *StorageCondition) Apply(node *SchedulableNode, allocatableResourceList *v1.ResourceList) bool {
	storage := allocatableResourceList.Storage()
	return storage.Sign() > 0 && storage.Cmp(cond.ResourceQuantity) > 0
}

// calc score of storage condition
func (cond *StorageCondition) Score(node *SchedulableNode, allocatableResourceList *v1.ResourceList) int64 {
	storage := *allocatableResourceList.Storage()
	storage.Sub(cond.ResourceQuantity)
	return storage.ScaledValue(100)
}

func NewStorageCondition(pod *v1.Pod) ScheduleCondition {
	resource := podutil.GetPodResourceList(pod).Storage()
	if resource.Sign() > 0 {
		return &StorageCondition{
			Name:             "Storage",
			ResourceQuantity: *resource,
		}
	} else {
		return nil
	}

}

type NodeNameCondition struct {
	Name             string
	ResourceQuantity resource.Quantity
}

func (*NodeNameCondition) Apply(node *SchedulableNode, allocatableResourceList *v1.ResourceList) bool {
	panic("unimplemented")
}

func (*NodeNameCondition) Mandatory() bool {
	panic("unimplemented")
}

func (*NodeNameCondition) Score(node *SchedulableNode, allocatableResourceList *v1.ResourceList) int64 {
	panic("unimplemented")
}

func NewNodeNameCondition(pod *v1.Pod) ScheduleCondition {
	resource := podutil.GetPodResourceList(pod).Cpu()
	if resource.Sign() > 0 {
		return &NodeNameCondition{
			Name:             "NodeName",
			ResourceQuantity: *resource,
		}
	} else {
		return nil
	}

}
