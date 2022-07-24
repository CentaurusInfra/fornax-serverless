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

package qos

import (
	"fmt"

	"k8s.io/mount-utils"

	v1 "k8s.io/api/core/v1"

	kubeletcm "k8s.io/kubernetes/pkg/kubelet/cm"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/cadvisor"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/resource"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	kubeletconfig "k8s.io/kubernetes/pkg/kubelet/apis/config"
)

const (
	// systemdSuffix is the cgroup name suffix for systemd
	systemdSuffix string = ".slice"
	// MemoryMin is memory.min for cgroup v2
	MemoryMin string = "memory.min"
	// MemoryHigh is memory.high for cgroup v2
	MemoryHigh string = "memory.high"
)

type QoSManager interface {
	IsSystemPod(*kubeletcm.CgroupConfig) bool
	IsPodCgroupExist(*v1.Pod) bool
	CreatePodCgroup(*v1.Pod) error
	DeletePodCgroup(*v1.Pod) error
	UpdateQOSCgroups() error
	GetPodCgroupParent(pod *v1.Pod) string
}

var _ QoSManager = &QoSManagerImpl{}

type QoSManagerImpl struct {
	KubeletCM        kubeletcm.ContainerManager
	PodCgroupManager kubeletcm.PodContainerManager
}

// DeletePodCgroup implements QoSManager
func (qm *QoSManagerImpl) DeletePodCgroup(pod *v1.Pod) error {
	cgroupName, _ := qm.PodCgroupManager.GetPodContainerName(pod)
	return qm.PodCgroupManager.Destroy(cgroupName)
}

// GetPodCgroupParent gets pod cgroup parent from container manager.
func (qm *QoSManagerImpl) GetPodCgroupParent(pod *v1.Pod) string {
	_, cgroupParent := qm.PodCgroupManager.GetPodContainerName(pod)
	return cgroupParent

}

// IsPodCgroupExist implements QoSManager
func (qm *QoSManagerImpl) IsPodCgroupExist(pod *v1.Pod) bool {
	return qm.PodCgroupManager.Exists(pod)
}

// IsSystemPod implements QoSManager
func (qm *QoSManagerImpl) IsSystemPod(config *kubeletcm.CgroupConfig) bool {
	yes := false
	if config.Name[0] == qm.KubeletCM.GetNodeConfig().KubeletCgroupsName {
		return true
	}
	if config.Name[0] == qm.KubeletCM.GetNodeConfig().SystemCgroupsName {
		return true
	}
	return yes
}

// CreateQOSCgroup implements QoSManager
func (qm *QoSManagerImpl) CreatePodCgroup(pod *v1.Pod) error {
	return qm.PodCgroupManager.EnsureExists(pod)
}

// UpdateQOSCgroups implements QoSManager
func (qos *QoSManagerImpl) UpdateQOSCgroups() error {
	return qos.KubeletCM.UpdateQOSCgroups()
}

func NewQoSManager(
	node *v1.Node,
	activePods kubeletcm.ActivePodsFunc,
	mountUtil mount.Interface,
	cadvisor cadvisor.CAdvisorInfoProvider,
	nodeConfig config.NodeConfiguration) (QoSManager, error) {

	kubletCMNodeConfig := buildKubeletCMNodeConfig(nodeConfig)
	capacity := v1.ResourceList{}
	nodeCAdvisorInfo, err := cadvisor.GetNodeCAdvisorInfo()
	if err != nil {
		return nil, fmt.Errorf("Failed to get node cadvisor info: %v", err)
	}

	machineInfo := nodeCAdvisorInfo.MachineInfo
	for name, res := range resource.ResourceListFromMachineInfo(machineInfo) {
		capacity[name] = res
	}

	rootFs := nodeCAdvisorInfo.RootFsInfo
	for name, res := range resource.EphemeralResourceListFromFsInfo(rootFs) {
		capacity[name] = res
	}
	cm, err := kubeletcm.NewContainerManager(mountUtil, node, capacity, kubletCMNodeConfig, nodeConfig.DisableSwap, false, util.NewNoopEventRecorder())
	if err != nil {
		return nil, fmt.Errorf("Failed to new a kubelet container manager for qos management, %v", err)
	}

	err = cm.Start(activePods, capacity)
	if err != nil {
		return nil, fmt.Errorf("failed to start kubelet container manager for qos management, %v", err)
	}
	return &QoSManagerImpl{
		KubeletCM:        cm,
		PodCgroupManager: cm.NewPodContainerManager(),
	}, nil
}

func buildKubeletCMNodeConfig(nodeConfig config.NodeConfiguration) kubeletcm.NodeConfig {
	return kubeletcm.NodeConfig{
		RuntimeCgroupsName:    nodeConfig.PodCgroupName,
		SystemCgroupsName:     nodeConfig.SystemCgroupName,
		KubeletCgroupsName:    nodeConfig.NodeAgentCgroupName,
		KubeletOOMScoreAdj:    nodeConfig.OOMScoreAdj,
		ContainerRuntime:      nodeConfig.ContainerRuntime,
		CgroupsPerQOS:         true,
		CgroupRoot:            nodeConfig.CgroupRoot,
		CgroupDriver:          nodeConfig.CgroupDriver,
		KubeletRootDir:        nodeConfig.RootPath,
		ProtectKernelDefaults: true,
		NodeAllocatableConfig: kubeletcm.NodeAllocatableConfig{
			KubeReservedCgroupName:   nodeConfig.NodeAgentCgroupName,
			SystemReservedCgroupName: nodeConfig.SystemCgroupName,
			KubeReserved:             nodeConfig.NodeAgentReserved.DeepCopy(),
			SystemReserved:           nodeConfig.SystemReserved.DeepCopy(),
			EvictionReservation:      v1.ResourceList{},
			ReservedSystemCPUs:       cpuset.CPUSet{},
			EnforceNodeAllocatable:   nodeConfig.EnforceNodeAllocatable},
		QOSReserved:                             nodeConfig.QOSReserved,
		ExperimentalCPUManagerPolicy:            "none",
		ExperimentalCPUManagerPolicyOptions:     map[string]string{},
		ExperimentalTopologyManagerScope:        "container",
		ExperimentalCPUManagerReconcilePeriod:   0,
		ExperimentalMemoryManagerPolicy:         "none",
		ExperimentalMemoryManagerReservedMemory: []kubeletconfig.MemoryReservation{},
		ExperimentalPodPidsLimit:                int64(nodeConfig.PodPidLimits),
		EnforceCPULimits:                        true,
		CPUCFSQuotaPeriod:                       nodeConfig.CPUCFSQuotaPeriod,
		ExperimentalTopologyManagerPolicy:       "none",
	}
}
