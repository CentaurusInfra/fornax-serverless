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

	"centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
)

type SchedulableNode struct {
	Node                       *v1.Node
	LastSeen                   time.Time
	LastUsed                   *time.Time
	Usage                      ScheduleUsage
	ResourceList               v1.ResourceList
	PodPreOccupiedResourceList map[string]v1.ResourceList
}

type NodePool map[string]*SchedulableNode

type ScheduleUsage struct {
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
