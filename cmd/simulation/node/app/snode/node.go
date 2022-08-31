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

	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/node"

	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
)

func InitV1Node(hostIp, hostName string) (*v1.Node, error) {
	node := &v1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            hostName,
			Namespace:       default_config.DefaultFornaxCoreNodeNameSpace,
			UID:             uuid.NewUUID(),
			ResourceVersion: "0",
			Generation:      0,
			CreationTimestamp: metav1.Time{
				Time: time.Now(),
			},
			DeletionTimestamp:          nil,
			DeletionGracePeriodSeconds: new(int64),
			Labels:                     map[string]string{v1.LabelHostname: hostName, v1.LabelOSStable: goruntime.GOOS, v1.LabelArchStable: goruntime.GOARCH},
			Annotations:                map[string]string{},
			OwnerReferences:            []metav1.OwnerReference{},
			Finalizers:                 []string{},
			ZZZ_DeprecatedClusterName:  "",
			ManagedFields:              []metav1.ManagedFieldsEntry{},
		},
		Spec: v1.NodeSpec{},
		Status: v1.NodeStatus{
			Capacity:        map[v1.ResourceName]resource.Quantity{},
			Allocatable:     map[v1.ResourceName]resource.Quantity{},
			Phase:           v1.NodePending,
			Conditions:      []v1.NodeCondition{},
			Addresses:       []v1.NodeAddress{},
			DaemonEndpoints: v1.NodeDaemonEndpoints{},
			NodeInfo:        v1.NodeSystemInfo{},
			Images:          []v1.ContainerImage{},
			VolumesInUse:    []v1.UniqueVolumeName{},
			VolumesAttached: []v1.AttachedVolume{},
			Config:          &v1.NodeConfigStatus{},
		},
	}

	node.Status.Conditions = append(node.Status.Conditions, v1.NodeCondition{
		Type:               v1.NodeReady,
		Status:             v1.ConditionFalse,
		Reason:             "Node Initialiazing",
		Message:            "Node Initialiazing",
		LastTransitionTime: metav1.NewTime(time.Now()),
	})

	node.Status.Conditions = append(node.Status.Conditions, v1.NodeCondition{
		Type:               v1.NodeNetworkUnavailable,
		Status:             v1.ConditionTrue,
		Reason:             "Node Initialiazing",
		Message:            "Node Initialiazing",
		LastTransitionTime: metav1.NewTime(time.Now()),
	})

	node.Status.Addresses = []v1.NodeAddress{
		{Type: v1.NodeInternalIP, Address: hostIp},
		{Type: v1.NodeHostName, Address: hostName},
	}

	return node, nil
}

type ContainerWorldSummary struct {
	runningPods    []*fornaxtypes.FornaxPod
	terminatedPods []*fornaxtypes.FornaxPod
}

func NewFornaxNode(hostIp, hostName string, nodeConfig config.NodeConfiguration) (*node.FornaxNode, error) {
	fornaxNode := node.FornaxNode{
		NodeConfig:   nodeConfig,
		V1Node:       nil,
		Revision:     0,
		Pods:         node.NewPodPool(),
		Dependencies: nil,
	}
	v1node, err := InitV1Node(hostIp, hostName)
	if err != nil {
		return nil, err
	} else {
		fornaxNode.V1Node = v1node
	}

	SetNodeStatus(&fornaxNode)
	return &fornaxNode, nil
}
