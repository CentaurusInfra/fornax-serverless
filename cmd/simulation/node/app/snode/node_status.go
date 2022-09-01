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
package snode

import (
	goruntime "runtime"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/node"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"

	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/klog/v2"
)

const (
	SIXTY_FOUR_G_MEM = 64 * 1024 * 1024 * 1024
)

type NodeStatusUpdater interface {
	UpdateNodeStatus(*v1.Node) error
}

func SetNodeStatus(node *node.FornaxNode) error {
	UpdateNodeCapacity(node.V1Node)

	var condition *v1.NodeCondition
	var conditions = map[v1.NodeConditionType]*v1.NodeCondition{}
	currentTime := metav1.NewTime(time.Now())
	condition = &v1.NodeCondition{
		Type:               v1.NodeNetworkUnavailable,
		Status:             v1.ConditionTrue,
		Reason:             "Node network initialized",
		Message:            "Node network initialized",
		LastHeartbeatTime:  currentTime,
		LastTransitionTime: currentTime,
	}
	conditions[condition.Type] = condition

	condition = &v1.NodeCondition{
		Type:               v1.NodeReady,
		Status:             v1.ConditionTrue,
		Reason:             "Node runtime ready",
		Message:            "Node runtime ready",
		LastHeartbeatTime:  currentTime,
		LastTransitionTime: currentTime,
	}
	conditions[condition.Type] = condition

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

func UpdateNodeCapacity(node *v1.Node) error {
	node.Status.NodeInfo.OperatingSystem = goruntime.GOOS
	node.Status.NodeInfo.Architecture = goruntime.GOARCH
	node.Status.NodeInfo.KernelVersion = "5.15.0-41-generic"
	node.Status.NodeInfo.OSImage = "Ubuntu 20.04.4 LTS"

	if node.Status.Capacity == nil {
		node.Status.Capacity = v1.ResourceList{}
	}

	node.Status.Capacity[v1.ResourceCPU] = util.ResourceQuantity(12000, v1.ResourceCPU)
	node.Status.Capacity[v1.ResourceMemory] = util.ResourceQuantity(SIXTY_FOUR_G_MEM, v1.ResourceMemory)
	node.Status.Capacity[v1.ResourcePods] = *k8sresource.NewQuantity(1000, k8sresource.DecimalSI)
	UpdateAllocatableResourceQuantity(v1.ResourceCPU, node, v1.ResourceList{})
	UpdateAllocatableResourceQuantity(v1.ResourceMemory, node, v1.ResourceList{})
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

func IsNodeStatusReady(myNode *node.FornaxNode) bool {
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
	for _, v := range myNode.Pods.List() {
		if v.Daemon {
			daemonReady = daemonReady && v.FornaxPodState == types.PodStateRunning
		}
	}

	klog.InfoS("Node Ready status", "cpu", cpuReady, "mem", memReady, "daemon", daemonReady, "nodeCondition", nodeConditionReady)
	return (cpuReady && memReady && daemonReady && nodeConditionReady)
}
