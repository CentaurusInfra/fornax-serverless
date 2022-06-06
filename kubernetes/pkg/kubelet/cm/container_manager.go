/*
Copyright 2015 The Kubernetes Authors.

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

package cm

import (
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	kubeletconfig "k8s.io/kubernetes/pkg/kubelet/apis/config"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

// Manages the containers running on a machine.
type ContainerManager interface {
	// Runs the container manager's housekeeping.
	// - Ensures that the Docker daemon is in a container.
	// - Creates the system container where all non-containerized processes run.
	Start(activePods ActivePodsFunc, capacities v1.ResourceList) error

	// SystemCgroupsLimit returns resources allocated to system cgroups in the machine.
	// These cgroups include the system and Kubernetes services.
	SystemCgroupsLimit() v1.ResourceList

	// NewPodContainerManager is a factory method which returns a podContainerManager object
	// Returns a noop implementation if qos cgroup hierarchy is not enabled
	NewPodContainerManager() PodContainerManager

	// GetMountedSubsystems returns the mounted cgroup subsystems on the node
	GetMountedSubsystems() *CgroupSubsystems

	// GetQOSContainersInfo returns the names of top level QoS containers
	GetQOSContainersInfo() QOSContainersInfo

	// GetNodeAllocatableReservation returns the amount of compute resources that have to be reserved from scheduling.
	GetNodeAllocatableReservation() v1.ResourceList

	// GetCapacity returns the amount of compute resources tracked by container manager available on the node.
	GetCapacity() v1.ResourceList

	// GetDevicePluginResourceCapacity returns the node capacity (amount of total device plugin resources),
	// node allocatable (amount of total healthy resources reported by device plugin),
	// and inactive device plugin resources previously registered on the node.
	// GetDevicePluginResourceCapacity() (v1.ResourceList, v1.ResourceList, []string)

	// UpdateQOSCgroups performs housekeeping updates to ensure that the top
	// level QoS containers have their desired state in a thread-safe way
	UpdateQOSCgroups() error

	// GetResources returns RunContainerOptions with devices, mounts, and env fields populated for
	// extended resources required by container.
	// GetResources(pod *v1.Pod, container *v1.Container) (*kubecontainer.RunContainerOptions, error)

	// GetPodCgroupRoot returns the cgroup which contains all pods.
	GetPodCgroupRoot() string

	// ShouldResetExtendedResourceCapacity returns whether or not the extended resources should be zeroed,
	// due to node recreation.
	// ShouldResetExtendedResourceCapacity() bool

	// GetNodeAllocatableAbsolute returns the absolute value of Node Allocatable which is primarily useful for enforcement.
	GetNodeAllocatableAbsolute() v1.ResourceList

	// GetNodeAllocatableAbsolute returns the absolute value of Node Allocatable which is primarily useful for enforcement.
	GetNodeConfig() *NodeConfig
}

type NodeConfig struct {
	RuntimeCgroupsName    string
	SystemCgroupsName     string
	KubeletCgroupsName    string
	KubeletOOMScoreAdj    int32
	ContainerRuntime      string
	CgroupsPerQOS         bool
	CgroupRoot            string
	CgroupDriver          string
	KubeletRootDir        string
	ProtectKernelDefaults bool
	NodeAllocatableConfig
	QOSReserved                             map[v1.ResourceName]int64
	ExperimentalCPUManagerPolicy            string
	ExperimentalCPUManagerPolicyOptions     map[string]string
	ExperimentalTopologyManagerScope        string
	ExperimentalCPUManagerReconcilePeriod   time.Duration
	ExperimentalMemoryManagerPolicy         string
	ExperimentalMemoryManagerReservedMemory []kubeletconfig.MemoryReservation
	ExperimentalPodPidsLimit                int64
	EnforceCPULimits                        bool
	CPUCFSQuotaPeriod                       time.Duration
	ExperimentalTopologyManagerPolicy       string
}

type NodeAllocatableConfig struct {
	KubeReservedCgroupName   string
	SystemReservedCgroupName string
	ReservedSystemCPUs       cpuset.CPUSet
	EnforceNodeAllocatable   sets.String
	KubeReserved             v1.ResourceList
	SystemReserved           v1.ResourceList
	EvictionReservation      v1.ResourceList
}

type Status struct {
	// Any soft requirements that were unsatisfied.
	SoftRequirements error
}

// func containerDevicesFromResourceDeviceInstances(devs devicemanager.ResourceDeviceInstances) []*podresourcesapi.ContainerDevices {
// 	var respDevs []*podresourcesapi.ContainerDevices
//
// 	for resourceName, resourceDevs := range devs {
// 		for devID, dev := range resourceDevs {
// 			topo := dev.GetTopology()
// 			if topo == nil {
// 				// Some device plugin do not report the topology information.
// 				// This is legal, so we report the devices anyway,
// 				// let the client decide what to do.
// 				respDevs = append(respDevs, &podresourcesapi.ContainerDevices{
// 					ResourceName: resourceName,
// 					DeviceIds:    []string{devID},
// 				})
// 				continue
// 			}
//
// 			for _, node := range topo.GetNodes() {
// 				respDevs = append(respDevs, &podresourcesapi.ContainerDevices{
// 					ResourceName: resourceName,
// 					DeviceIds:    []string{devID},
// 					Topology: &podresourcesapi.TopologyInfo{
// 						Nodes: []*podresourcesapi.NUMANode{
// 							{
// 								ID: node.GetID(),
// 							},
// 						},
// 					},
// 				})
// 			}
// 		}
// 	}
//
// 	return respDevs
// }
