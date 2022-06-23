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

var _ ResoureManager = &VolumeManager{}

type VolumeManager struct {
}

func (*VolumeManager) UnmountPodVolume(*v1.Pod) error {
	return nil
}

func (*VolumeManager) WaitForAttachAndMount(*v1.Pod) error {
	return nil
}

// GetReservedResource implements ResoureManager
func (*VolumeManager) GetReservedResource() NodeResource {
	return NodeResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// GetAllocatedResource implements ResoureManager
func (*VolumeManager) GetAllocatedResource() NodeResource {
	return NodeResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// GetAvailableResource implements ResoureManager
func (*VolumeManager) GetAvailableResource() NodeResource {
	return NodeResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// GetPodResource implements ResoureManager
func (*VolumeManager) GetPodResource(v1.Pod) PodResource {
	return PodResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// ReserveResource implements ResoureManager
func (*VolumeManager) ReserveResource(NodeResource) bool {
	panic("unimplemented")
}

// DryRunAdmit implements ResoureManager
func (*VolumeManager) DryRunAdmit(v1.Pod) error {
	panic("unimplemented")
}

// Admit implements ResoureManager
func (*VolumeManager) Admit(v1.Pod) error {
	panic("unimplemented")
}

// Allocate implements ResoureManager
func (*VolumeManager) Allocate(v1.Pod) error {
	panic("unimplemented")
}

// Deallocate implements ResoureManager
func (*VolumeManager) Deallocate(v1.Pod) error {
	panic("unimplemented")
}

//
// // DiskPressureCondition returns a Setter that updates the v1.NodeDiskPressure condition on the node.
// func DiskPressureCondition(nowFunc func() time.Time, // typically Kubelet.clock.Now
//  pressureFunc func() bool, // typically Kubelet.evictionManager.IsUnderDiskPressure
//  recordEventFunc func(eventType, event string), // typically Kubelet.recordNodeStatusEvent
// ) Setter {
//  return func(node *v1.Node) error {
//    currentTime := metav1.NewTime(nowFunc())
//    var condition *v1.NodeCondition
//
//    // Check if NodeDiskPressure condition already exists and if it does, just pick it up for update.
//    for i := range node.Status.Conditions {
//      if node.Status.Conditions[i].Type == v1.NodeDiskPressure {
//        condition = &node.Status.Conditions[i]
//      }
//    }
//
//    newCondition := false
//    // If the NodeDiskPressure condition doesn't exist, create one
//    if condition == nil {
//      condition = &v1.NodeCondition{
//        Type:   v1.NodeDiskPressure,
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
//    // Note: The conditions below take care of the case when a new NodeDiskPressure condition is
//    // created and as well as the case when the condition already exists. When a new condition
//    // is created its status is set to v1.ConditionUnknown which matches either
//    // condition.Status != v1.ConditionTrue or
//    // condition.Status != v1.ConditionFalse in the conditions below depending on whether
//    // the kubelet is under disk pressure or not.
//    if pressureFunc() {
//      if condition.Status != v1.ConditionTrue {
//        condition.Status = v1.ConditionTrue
//        condition.Reason = "KubeletHasDiskPressure"
//        condition.Message = "kubelet has disk pressure"
//        condition.LastTransitionTime = currentTime
//        recordEventFunc(v1.EventTypeNormal, "NodeHasDiskPressure")
//      }
//    } else if condition.Status != v1.ConditionFalse {
//      condition.Status = v1.ConditionFalse
//      condition.Reason = "KubeletHasNoDiskPressure"
// // VolumesInUse returns a Setter that updates the volumes in use on the node.
// func VolumesInUse(syncedFunc func() bool, // typically Kubelet.volumeManager.ReconcilerStatesHasBeenSynced
//  volumesInUseFunc func() []v1.UniqueVolumeName, // typically Kubelet.volumeManager.GetVolumesInUse
// ) Setter {
//  return func(node *v1.Node) error {
//    // Make sure to only update node status after reconciler starts syncing up states
//    if syncedFunc() {
//      node.Status.VolumesInUse = volumesInUseFunc()
//    }
//    return nil
//  }
// }
//
// // VolumeLimits returns a Setter that updates the volume limits on the node.
// func VolumeLimits(volumePluginListFunc func() []volume.VolumePluginWithAttachLimits, // typically Kubelet.volumePluginMgr.ListVolumePluginWithLimits
// ) Setter {
//  return func(node *v1.Node) error {
//    if node.Status.Capacity == nil {
//      node.Status.Capacity = v1.ResourceList{}
//    }
//    if node.Status.Allocatable == nil {
//      node.Status.Allocatable = v1.ResourceList{}
//    }
//
//    pluginWithLimits := volumePluginListFunc()
//    for _, volumePlugin := range pluginWithLimits {
//      attachLimits, err := volumePlugin.GetVolumeLimits()
//      if err != nil {
//        klog.V(4).InfoS("Skipping volume limits for volume plugin", "plugin", volumePlugin.GetPluginName())
//        continue
//      }
//      for limitKey, value := range attachLimits {
//        node.Status.Capacity[v1.ResourceName(limitKey)] = *resource.NewQuantity(value, resource.DecimalSI)
//        node.Status.Allocatable[v1.ResourceName(limitKey)] = *resource.NewQuantity(value, resource.DecimalSI)
//      }
//    }
//    return nil
//  }
// }
