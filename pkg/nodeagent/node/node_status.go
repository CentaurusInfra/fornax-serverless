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
package node

import (
	"errors"
	"math"
	goruntime "runtime"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/cadvisor"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/network"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/resource"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/klog/v2"
)

type NodeStatusUpdater interface {
	UpdateNodeStatus(*v1.Node) error
}

func SetNodeStatus(node *FornaxNode) error {
	var errs = []error{}
	var condition *v1.NodeCondition
	var conditions = map[v1.NodeConditionType]*v1.NodeCondition{}
	var err error
	condition, err = UpdateNodeAddress(node.Dependencies.NetworkProvider, node.V1Node)
	if err != nil {
		errs = append(errs, errors.New("can not find network provider"))
	}
	conditions[condition.Type] = condition

	err = UpdateNodeCapacity(node.Dependencies.CAdvisor, node.NodeConfig, node.V1Node)
	if err != nil {
		errs = append(errs, errors.New("can not find cadvisor"))
	}

	condition, err = UpdateNodeReadyStatus(node.Dependencies.CRIRuntimeService, node.V1Node)
	if err != nil {
		errs = append(errs, errors.New("cand not find cri runtime"))
	}
	conditions[condition.Type] = condition

	condition, err = UpdateNodeMemoryStatus(node.Dependencies.MemoryManager, node.V1Node)
	if err != nil {
		errs = append(errs, errors.New("can not update memory resource status"))
	}
	conditions[condition.Type] = condition

	condition, err = UpdateNodeCPUStatus(node.Dependencies.CPUManager, node.V1Node)
	if err != nil {
		errs = append(errs, errors.New("can not update cpu resource status"))
	}
	conditions[condition.Type] = condition

	condition, err = UpdateNodeVolumeStatus(node.Dependencies.VolumeManager, node.V1Node)
	if err != nil {
		errs = append(errs, errors.New("can not update volume resource status"))
	}
	conditions[condition.Type] = condition

	currentTime := metav1.NewTime(time.Now())
	if len(errs) == 0 {
		conditions[v1.NodeReady] = &v1.NodeCondition{
			Type:               v1.NodeReady,
			Status:             v1.ConditionTrue,
			Reason:             "NodeRuntime Ready",
			Message:            "Node is ready to get pod",
			LastHeartbeatTime:  currentTime,
			LastTransitionTime: currentTime,
		}
	}

	mergeNodeConditions(node.V1Node, conditions)

	return nil
}

func mergeNodeConditions(node *v1.Node, conditions map[v1.NodeConditionType]*v1.NodeCondition) {
	for i := range node.Status.Conditions {
		oldCondition := &node.Status.Conditions[i]
		updatedCondition, ok := conditions[oldCondition.Type]
		if ok {
			oldCondition.LastHeartbeatTime = updatedCondition.LastHeartbeatTime
			if oldCondition.Status != updatedCondition.Status {
				node.Status.Conditions[i] = *updatedCondition.DeepCopy()
			}
			delete(conditions, oldCondition.Type)
		}
	}

	// append new conditions into node.status.Conditions
	for _, v := range conditions {
		node.Status.Conditions = append(node.Status.Conditions, *v.DeepCopy())
	}
}

func UpdateNodeAddress(networkProvider network.NetworkAddressProvider, node *v1.Node) (*v1.NodeCondition, error) {
	if addresses, err := networkProvider.GetNetAddress(); err != nil {
		klog.Errorf("failed to calculate node address: err %v", err)
		return nil, err
	} else {
		node.Status.Addresses = addresses
	}

	currentTime := metav1.NewTime(time.Now())
	condition := &v1.NodeCondition{
		Type:               v1.NodeNetworkUnavailable,
		Status:             v1.ConditionFalse,
		Reason:             "Node network initialized",
		Message:            "Node network initialized",
		LastHeartbeatTime:  currentTime,
		LastTransitionTime: currentTime,
	}
	return condition, nil
}

func UpdateNodeReadyStatus(criRuntime runtime.RuntimeService, node *v1.Node) (*v1.NodeCondition, error) {
	status, err := criRuntime.GetRuntimeStatus()
	if err != nil {
		return nil, err
	}

	currentTime := metav1.NewTime(time.Now())
	networkReady := false
	runtimeReady := false
	for _, v := range status.Conditions {
		if v.Type == "NetworkReady" && v.Status {
			networkReady = true
		}
		if v.Type == "RuntimeReady" && v.Status {
			runtimeReady = true
		}
	}

	var condition *v1.NodeCondition
	if runtimeReady && networkReady {
		condition = &v1.NodeCondition{
			Type:               v1.NodeReady,
			Status:             v1.ConditionTrue,
			Reason:             "Node runtime ready",
			Message:            "Node runtime ready",
			LastHeartbeatTime:  currentTime,
			LastTransitionTime: currentTime,
		}
	} else {
		condition = &v1.NodeCondition{
			Type:               v1.NodeReady,
			Status:             v1.ConditionFalse,
			Reason:             "Node runtime not ready",
			Message:            "Node runtime not ready",
			LastHeartbeatTime:  currentTime,
			LastTransitionTime: currentTime,
		}
	}

	return condition, nil
}

func UpdateNodeCPUStatus(cpuManager resource.CPUManager, node *v1.Node) (*v1.NodeCondition, error) {
	if node.Status.Allocatable == nil {
		node.Status.Allocatable = make(v1.ResourceList)
	}

	UpdateAllocatableResourceQuantity(v1.ResourceCPU, node, cpuManager.GetReservedResource().Resources)

	condition := &v1.NodeCondition{}
	return condition, nil
}

func UpdateNodeMemoryStatus(memoryManager resource.MemoryManager, node *v1.Node) (*v1.NodeCondition, error) {
	if node.Status.Allocatable == nil {
		node.Status.Allocatable = make(v1.ResourceList)
	}

	UpdateAllocatableResourceQuantity(v1.ResourceMemory, node, memoryManager.GetReservedResource().Resources)
	// TODO add condition
	condition := &v1.NodeCondition{}
	return condition, nil
}

func UpdateNodeVolumeStatus(volumeManager resource.VolumeManager, node *v1.Node) (*v1.NodeCondition, error) {
	if node.Status.Allocatable == nil {
		node.Status.Allocatable = make(v1.ResourceList)
	}

	UpdateAllocatableResourceQuantity(v1.ResourceStorage, node, volumeManager.GetReservedResource().Resources)

	// TODO add condition
	condition := &v1.NodeCondition{}
	return condition, nil
}

func UpdateNodeCapacity(cc cadvisor.CAdvisorInfoProvider, nodeConfig config.NodeConfiguration, node *v1.Node) error {
	info, err := cc.GetNodeCAdvisorInfo()
	if err != nil {
		return err
	}

	node.Status.NodeInfo.OperatingSystem = goruntime.GOOS
	node.Status.NodeInfo.Architecture = goruntime.GOARCH
	node.Status.NodeInfo.KernelVersion = info.VersionInfo.KernelVersion
	node.Status.NodeInfo.OSImage = info.VersionInfo.ContainerOsVersion

	if node.Status.Capacity == nil {
		node.Status.Capacity = v1.ResourceList{}
	}

	klog.Infof("cadvisor info %v", info.MachineInfo)
	if info == nil {
		node.Status.Capacity[v1.ResourceCPU] = *k8sresource.NewMilliQuantity(0, k8sresource.DecimalSI)
		node.Status.Capacity[v1.ResourceMemory] = k8sresource.MustParse("0Gi")
		node.Status.Capacity[v1.ResourcePods] = *k8sresource.NewQuantity(0, k8sresource.DecimalSI)
	} else {
		node.Status.NodeInfo.MachineID = info.MachineInfo.MachineID
		node.Status.NodeInfo.SystemUUID = info.MachineInfo.SystemUUID
		node.Status.NodeInfo.BootID = info.MachineInfo.BootID

		for rName, rCap := range resource.ResourceListFromMachineInfo(info.MachineInfo) {
			node.Status.Capacity[rName] = rCap
		}

		if nodeConfig.PodsPerCore > 0 {
			node.Status.Capacity[v1.ResourcePods] =
				util.ResourceQuantity(
					int64(math.Min(float64(info.MachineInfo.NumCores*nodeConfig.PodsPerCore), float64(nodeConfig.MaxPods))), v1.ResourcePods)
		} else {
			node.Status.Capacity[v1.ResourcePods] =
				util.ResourceQuantity(int64(nodeConfig.MaxPods), v1.ResourcePods)
		}
	}

	return nil

}

func UpdateAllocatableResourceQuantity(resourceName v1.ResourceName, node *v1.Node, reservedQuantity v1.ResourceList) {
	zeroQuanity := util.ResourceQuantity(0, resourceName)
	capacity, ok := node.Status.Capacity[resourceName]
	if ok {
		value := capacity.DeepCopy()
		var resValue k8sresource.Quantity
		resValue, ok = reservedQuantity[v1.ResourceCPU]
		if !ok {
			resValue = zeroQuanity
		}
		value.Sub(resValue)
		if value.Sign() < 0 {
			value.Set(0)
		}
		node.Status.Allocatable[resourceName] = value
	} else {
		node.Status.Allocatable[resourceName] = zeroQuanity
	}
}

func IsNodeStatusReady(myNode *FornaxNode) bool {
	// check node capacity and allocatable are set
	cpuReady := false
	memReady := false
	for k, v := range myNode.V1Node.Status.Allocatable {
		if k == v1.ResourceCPU && v.Sign() > 0 {
			cpuReady = true
		}
		if k == v1.ResourceMemory && v.Sign() > 0 {
			memReady = true
		}
	}

	// check node condition
	nodeConditionReady := util.IsNodeCondtionReady(myNode.V1Node)

	// check daemon pod status
	daemonReady := true
	for _, v := range myNode.Pods {
		if v.Daemon {
			daemonReady = daemonReady && v.FornaxPodState == fornaxtypes.PodStateRunning
		}
	}

	klog.InfoS("Node Ready status", "cpu", cpuReady, "mem", memReady, "daemon", daemonReady, "nodeCondition", nodeConditionReady)
	return (cpuReady && memReady && daemonReady && nodeConditionReady)
}
