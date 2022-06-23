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

package resource

import (
	"fmt"

	cadvisorinfov1 "github.com/google/cadvisor/info/v1"
	cadvisorinfov2 "github.com/google/cadvisor/info/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type ResoureManager interface {
	GetAvailableResource() NodeResource
	GetAllocatedResource() NodeResource
	GetReservedResource() NodeResource
	GetPodResource(v1.Pod) PodResource
	DryRunAdmit(v1.Pod) error
	Admit(v1.Pod) error
	Allocate(v1.Pod) error
	Deallocate(v1.Pod) error
}

type NodeResource struct {
	Resources v1.ResourceList
}

type PodResource struct {
	Resources v1.ResourceList
}

const (
	ResourcePID v1.ResourceName = "pid"
	MaxPID                      = 100
)

func ResourceQuantity(quantity int64, resourceName v1.ResourceName) resource.Quantity {
	switch resourceName {
	case v1.ResourceCPU:
		return *resource.NewMilliQuantity(quantity, resource.DecimalSI)
	case v1.ResourceMemory:
		return *resource.NewQuantity(quantity, resource.BinarySI)
	case v1.ResourcePods:
		return *resource.NewQuantity(quantity, resource.DecimalSI)
	case v1.ResourceStorage:
		return *resource.NewQuantity(quantity, resource.BinarySI)
	case v1.ResourceEphemeralStorage:
		return *resource.NewQuantity(quantity, resource.BinarySI)
	default:
		return *resource.NewQuantity(quantity, resource.DecimalSI)
	}
}

func ResourceListFromMachineInfo(info *cadvisorinfov1.MachineInfo) v1.ResourceList {
	resources := v1.ResourceList{
		v1.ResourceCPU:    ResourceQuantity(int64(info.NumCores*1000), v1.ResourceCPU),
		v1.ResourceMemory: ResourceQuantity(int64(info.MemoryCapacity), v1.ResourceMemory),
	}

	for _, hugepagesInfo := range info.HugePages {
		pageSizeBytes := int64(hugepagesInfo.PageSize * 1024)
		hugePagesBytes := pageSizeBytes * int64(hugepagesInfo.NumPages)
		pageSizeQuantity := ResourceQuantity(pageSizeBytes, v1.ResourceMemory)
		name := v1.ResourceName(fmt.Sprintf("%s%s", v1.ResourceHugePagesPrefix, pageSizeQuantity.String()))
		resources[name] = ResourceQuantity(hugePagesBytes, v1.ResourceMemory)
	}

	return resources
}

func EphemeralResourceListFromFsInfo(info *cadvisorinfov2.FsInfo) v1.ResourceList {
	resources := v1.ResourceList{
		v1.ResourceEphemeralStorage: ResourceQuantity(int64(info.Capacity), v1.ResourceEphemeralStorage),
	}
	return resources
}
