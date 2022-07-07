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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ ResoureManager = &MemoryManager{}

type MemoryManager struct {
}

// GetReservedResource implements ResoureManager
func (*MemoryManager) GetReservedResource() NodeResource {
	return NodeResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// GetAllocatedResource implements ResoureManager
func (*MemoryManager) GetAllocatedResource() NodeResource {
	return NodeResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// GetAvailableResource implements ResoureManager
func (*MemoryManager) GetAvailableResource() NodeResource {
	return NodeResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// GetPodResource implements ResoureManager
func (*MemoryManager) GetPodResource(v1.Pod) PodResource {
	return PodResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// ReserveResource implements ResoureManager
func (*MemoryManager) ReserveResource(NodeResource) bool {
	panic("unimplemented")
}

// DryRunAdmit implements ResoureManager
func (*MemoryManager) DryRunAdmit(v1.Pod) error {
	panic("unimplemented")
}

// Admit implements ResoureManager
func (*MemoryManager) Admit(v1.Pod) error {
	panic("unimplemented")
}

// Allocate implements ResoureManager
func (*MemoryManager) Allocate(v1.Pod) error {
	panic("unimplemented")
}

// Deallocate implements ResoureManager
func (*MemoryManager) Deallocate(v1.Pod) error {
	panic("unimplemented")
}

// reference
// kubelet/memory/memorymanager.go
// kubelet/memory/memorymanager/policy_static.go
//
// // MemoryPressureCondition returns a Setter that updates the v1.NodeMemoryPressure condition on the node.
// func MemoryPressureCondition(nowFunc func() time.Time, // typically Kubelet.clock.Now
//  pressureFunc func() bool, // typically Kubelet.evictionManager.IsUnderMemoryPressure
//  recordEventFunc func(eventType, event string), // typically Kubelet.recordNodeStatusEvent
// ) Setter {
//  return func(node *v1.Node) error {
//    currentTime := metav1.NewTime(nowFunc())
//    var condition *v1.NodeCondition
//
//    // Check if NodeMemoryPressure condition already exists and if it does, just pick it up for update.
//    for i := range node.Status.Conditions {
//      if node.Status.Conditions[i].Type == v1.NodeMemoryPressure {
//        condition = &node.Status.Conditions[i]
//      }
//    }
//
//    newCondition := false
//    // If the NodeMemoryPressure condition doesn't exist, create one
//    if condition == nil {
//      condition = &v1.NodeCondition{
//        Type:   v1.NodeMemoryPressure,
//        Status: v1.ConditionUnknown,
//      }
//      // cannot be appended to node.Status.Conditions here because it gets
//      // copied to the slice. So if we append to the slice here none of the
//      // updates we make below are reflected in the slice.
//      newCondition = true
//    }
//
//    // Update the heartbeat time
//    condition.LastHeartbeatTime = currentTime
//
//    // Note: The conditions below take care of the case when a new NodeMemoryPressure condition is
//    // created and as well as the case when the condition already exists. When a new condition
//    // is created its status is set to v1.ConditionUnknown which matches either
//    // condition.Status != v1.ConditionTrue or
//    // condition.Status != v1.ConditionFalse in the conditions below depending on whether
//    // the kubelet is under memory pressure or not.
//    if pressureFunc() {
//      if condition.Status != v1.ConditionTrue {
//        condition.Status = v1.ConditionTrue
//        condition.Reason = "KubeletHasInsufficientMemory"
//        condition.Message = "kubelet has insufficient memory available"
//        condition.LastTransitionTime = currentTime
//        recordEventFunc(v1.EventTypeNormal, "NodeHasInsufficientMemory")
//      }
//    } else if condition.Status != v1.ConditionFalse {
//      condition.Status = v1.ConditionFalse
//      condition.Reason = "KubeletHasSufficientMemory"
