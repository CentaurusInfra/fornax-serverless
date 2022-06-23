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
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/cadvisor"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	cadvisorinfov1 "github.com/google/cadvisor/info/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ ResoureManager = &CPUManager{}

type CPUManager struct {
	channel               chan cadvisor.NodeCAdvisorInfo
	advisor               cadvisor.CAdvisorInfoProvider
	MachineInfo           cadvisorinfov1.MachineInfo
	ReservedSystemCPUs    resource.Quantity
	ReservedNodeAgentCPUs resource.Quantity
}

func NewCpuManager(nodeConfig config.NodeConfiguration, advisor cadvisor.CAdvisorInfoProvider) *CPUManager {
	manager := &CPUManager{
		channel: make(chan cadvisor.NodeCAdvisorInfo),
		advisor: advisor,
	}

	nodeCAdvisorInfo, err := advisor.GetNodeCAdvisorInfo()
	if err != nil {
		return nil
	} else {
		manager.MachineInfo = *nodeCAdvisorInfo.MachineInfo.Clone()
	}

	advisor.ReceiveCAdvisorInfo("CPUManager", &manager.channel)
	return manager
}

func (m *CPUManager) Start() error {
	go func() {
		// update memory information with received machineInfo

	}()
	return nil
}

// GetReservedResource implements ResoureManager
func (*CPUManager) GetReservedResource() NodeResource {
	return NodeResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// GetAllocatedResource implements ResoureManager
func (*CPUManager) GetAllocatedResource() NodeResource {
	return NodeResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// GetAvailableResource implements ResoureManager
func (*CPUManager) GetAvailableResource() NodeResource {
	return NodeResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// GetPodResource implements ResoureManager
func (*CPUManager) GetPodResource(v1.Pod) PodResource {
	return PodResource{
		Resources: map[v1.ResourceName]resource.Quantity{},
	}
}

// ReserveResource implements ResoureManager
func (*CPUManager) ReserveResource(NodeResource) bool {
	panic("unimplemented")
}

// Admit implements ResoureManager
func (*CPUManager) DryRunAdmit(v1.Pod) error {
	panic("unimplemented")
}

// Admit implements ResoureManager
func (*CPUManager) Admit(v1.Pod) error {
	panic("unimplemented")
}

// Allocate implements ResoureManager
func (*CPUManager) Allocate(v1.Pod) error {
	panic("unimplemented")
}

// Deallocate implements ResoureManager
func (*CPUManager) Deallocate(v1.Pod) error {
	panic("unimplemented")
}

// reference
// kubelet/cm/cpumanager.go
// kubelet/cm/cpumanager/policy_static.go

// if nodeAllocatable != nil && nodeAllocatableReservation != nil {
//   if node.Status.Allocatable == nil {
//     node.Status.Allocatable = make(v1.ResourceList)
//   }
//
//   for k, v := range *nodeAllocatable {
//     node.Status.Allocatable[k] = v
//   }
//   for k, v := range *nodeAllocatableReservation {
//     allocatableValue := node.Status.Allocatable[k]
//     allocatableValue.Sub(v)
//     if allocatableValue.Sign() < 0 {
//       allocatableValue.Set(0)
//     }
//     node.Status.Allocatable[k] = allocatableValue
//   }
//
// for every huge page reservation, we need to remove it from allocatable memory
// for k, v := range node.Status.Capacity {
//  if v1helper.IsHugePageResourceName(k) {
//    allocatableMemory := node.Status.Allocatable[v1.ResourceMemory]
//    value := v.DeepCopy()
//    allocatableMemory.Sub(value)
//    if allocatableMemory.Sign() < 0 {
//      // Negative Allocatable resources don't make sense.
//      allocatableMemory.Set(0)
//    }
//    node.Status.Allocatable[v1.ResourceMemory] = allocatableMemory
//  }
// }
// }

// // DaemonEndpoints returns a Setter that updates the daemon endpoints on the node.
// func DaemonEndpoints(daemonEndpoints *v1.NodeDaemonEndpoints) Setter {
//  return func(node *v1.Node) error {
//    node.Status.DaemonEndpoints = *daemonEndpoints
//    return nil
//  }
// }

// does fornax pod use qos always, what's qos policy, BestEffort or Guaranteed

// type NodeAllocatableConfig struct {
//  KubeReservedCgroupName   string
//  SystemReservedCgroupName string
//  ReservedSystemCPUs       cpuset.CPUSet
//  EnforceNodeAllocatable   sets.String
//  KubeReserved             v1.ResourceList
//  SystemReserved           v1.ResourceList
//  HardEvictionThresholds   []evictionapi.Threshold
// }

// ExperimentalTopologyManagerPolicy       string
// get machine resource info from cadvisor
// ref, kubelet/cm/container_manager_linux.go
// func NewContainerManager(mountUtil mount.Interface, cadvisorInterface cadvisor.Interface, nodeConfig NodeConfig, failSwapOn bool, devicePluginEnabled bool, recorder record.EventRecorder) (ContainerManager, error)
// var internalCapacity = v1.ResourceList{}
// // It is safe to invoke `MachineInfo` on cAdvisor before logically initializing cAdvisor here because
// // machine info is computed and cached once as part of cAdvisor object creation.
// // But `RootFsInfo` and `ImagesFsInfo` are not available at this moment so they will be called later during manager starts
// machineInfo, err := cadvisorInterface.MachineInfo()
// if err != nil {
//   return nil, err
//
// }
// capacity := cadvisor.CapacityFromMachineInfo(machineInfo)
// for k, v := range capacity {
//   internalCapacity[k] = v
//
// }
// pidlimits, err := pidlimit.Stats()
// if err == nil && pidlimits != nil && pidlimits.MaxPID != nil {
//   internalCapacity[pidlimit.PIDs] = *resource.NewQuantity(
//     int64(*pidlimits.MaxPID),
//     resource.DecimalSI
//   )
// }
