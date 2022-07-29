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
	"fmt"
	"net"
	"os"
	goruntime "runtime"
	"sort"
	"time"

	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/dependency"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/store"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/store/factory"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"

	// criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

type FornaxNode struct {
	NodeConfig   config.NodeConfiguration
	V1Node       *v1.Node
	Revision     int64
	Pods         map[string]*fornaxtypes.FornaxPod
	Dependencies *dependency.Dependencies
}

func (n *FornaxNode) initV1Node() (*v1.Node, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	node := &v1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            hostname,
			Namespace:       default_config.DefaultFornaxCoreNodeNameSpace,
			UID:             uuid.NewUUID(),
			ResourceVersion: "1",
			Generation:      0,
			CreationTimestamp: metav1.Time{
				Time: time.Now(),
			},
			DeletionTimestamp:          nil,
			DeletionGracePeriodSeconds: new(int64),
			Labels:                     map[string]string{v1.LabelHostname: hostname, v1.LabelOSStable: goruntime.GOOS, v1.LabelArchStable: goruntime.GOARCH},
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

	if n.Dependencies.NetworkProvider != nil {
		node.Status.Addresses, err = n.Dependencies.NetworkProvider.GetNetAddress()
		if err != nil {
			return nil, err
		}
	} else {
		node.Status.Addresses = []v1.NodeAddress{
			{Type: v1.NodeInternalIP, Address: n.NodeConfig.NodeIP},
			{Type: v1.NodeHostName, Address: hostname},
		}
	}

	return node, nil
}

type ContainerWorldSummary struct {
	runningPods    []*fornaxtypes.FornaxPod
	terminatedPods []*fornaxtypes.FornaxPod
}

func LoadPodsFromContainerRuntime(runtimeService runtime.RuntimeService, db *factory.PodStore) (ContainerWorldSummary, error) {
	world := ContainerWorldSummary{
		runningPods:    []*fornaxtypes.FornaxPod{},
		terminatedPods: []*fornaxtypes.FornaxPod{},
	}
	pods, err := db.ListObject()
	if err != nil {
		klog.Errorf("can not load runtime world, probably runtime is not ready yet if node is rebooted")
		return world, err
	}

	for _, obj := range pods {
		podObj := obj.(*fornaxtypes.FornaxPod)
		if podObj.RuntimePod == nil {
			world.terminatedPods = append(world.terminatedPods, podObj)
		} else {
			containerIDs := []string{}
			for _, v := range podObj.Containers {
				containerIDs = append(containerIDs, v.RuntimeContainer.Id)
			}

			status, err := runtimeService.GetPodStatus(podObj.RuntimePod.Id, containerIDs)
			if err == nil {
				// runtime service return nil if no such pod
				if status != nil {
					for _, v := range podObj.Containers {
						runtimeStatus, found := status.ContainerStatuses[v.RuntimeContainer.Id]
						if found {
							v.ContainerStatus.RuntimeStatus = runtimeStatus
						} else {
							// this container does not have runtime status, so, mark it as already terminated
							podObj.FornaxPodState = fornaxtypes.PodStateFailed
							v.State = fornaxtypes.ContainerStateTerminated
						}
					}
					world.runningPods = append(world.runningPods, podObj)
				} else {
					// store pod does not exist in container runtime, mark all containers as terminated
					podObj.FornaxPodState = fornaxtypes.PodStateTerminated
					for _, v := range podObj.Containers {
						v.State = fornaxtypes.ContainerStateTerminated
					}
					world.terminatedPods = append(world.terminatedPods, podObj)
				}
				db.PutObject(string(podObj.Identifier), podObj)
			} else {
				// got error, can not make decision, will retry
				return world, err
			}
		}
	}
	return world, nil
}

func NodeSpecPodCidrChanged(myNode *v1.Node, apiNode *v1.Node) bool {
	errs := ValidateNodeSpec(apiNode)
	if len(errs) > 0 {
		klog.Errorf("api node spec is not valid, errors %v", errs)
		// if apiNode spec is invalid, treat it not changed
		return false
	}

	// if PodCIDR changed or PodCIDRs changed
	if len(myNode.Spec.PodCIDR) == 0 || myNode.Spec.PodCIDR != apiNode.Spec.PodCIDR || len(myNode.Spec.PodCIDRs) != len(apiNode.Spec.PodCIDRs) {
		return true
	}

	sort.Strings(myNode.Spec.PodCIDRs)
	sort.Strings(apiNode.Spec.PodCIDRs)

	for i := 0; i < len(myNode.Spec.PodCIDRs); i++ {
		if myNode.Spec.PodCIDRs[i] != apiNode.Spec.PodCIDRs[i] {
			return true
		}
	}

	return false
}

// only validate pod cidr for now
func ValidateNodeSpec(apiNode *v1.Node) []error {
	errors := []error{}
	if len(apiNode.Spec.PodCIDR) == 0 {
		errors = append(errors, fmt.Errorf("api node spec pod cidr is nil"))
	} else if _, _, err := net.ParseCIDR(apiNode.Spec.PodCIDR); err != nil {
		errors = append(errors, fmt.Errorf("api node spec PodCIDR %s is invalid", apiNode.Spec.PodCIDR))
	}

	for i, v := range apiNode.Spec.PodCIDRs {
		if _, _, err := net.ParseCIDR(apiNode.Spec.PodCIDR); err != nil {
			errors = append(errors, fmt.Errorf("api node spec PodCIDRs[%d]: %s is invalid", i, v))
		}
	}

	if len(apiNode.Spec.PodCIDRs) > 0 && apiNode.Spec.PodCIDRs[0] != apiNode.Spec.PodCIDR {
		errors = append(errors, fmt.Errorf("api node spec podcidrs[0] %s does not match podcidr %s", apiNode.Spec.PodCIDRs[0], apiNode.Spec.PodCIDR))
	}
	return errors
}

func (n *FornaxNode) Init() error {
	return n.Dependencies.Complete(n.V1Node, n.NodeConfig, n.activePods)
}

func (n *FornaxNode) activePods() []*v1.Pod {
	v1Pods := []*v1.Pod{}
	for _, v := range n.Pods {
		v1Pods = append(v1Pods, v.Pod.DeepCopy())
	}
	return v1Pods
}

func NewFornaxNode(nodeConfig config.NodeConfiguration, dependencies *dependency.Dependencies) (*FornaxNode, error) {
	fornaxNode := FornaxNode{
		NodeConfig:   nodeConfig,
		V1Node:       nil,
		Revision:     0,
		Pods:         map[string]*fornaxtypes.FornaxPod{},
		Dependencies: dependencies,
	}
	v1node, err := fornaxNode.initV1Node()
	if err != nil {
		return nil, err
	} else {
		fornaxNode.V1Node = v1node
	}
	nodeWithRevision, err := dependencies.NodeStore.GetNode(util.UniqueNodeName(v1node))
	if err != nil {
		if err != store.StoreObjectNotFound {
			return nil, err
		}
	} else {
		if util.UniqueNodeName(nodeWithRevision.Node) != util.UniqueNodeName(v1node) {
			// if node name changed, it's a new node
			// TODO, check if spec changed, fornax core could send a completed spec back to node agent after it's registered
			fornaxNode.Revision = 0
		} else {
			fornaxNode.V1Node.UID = nodeWithRevision.Node.UID
			fornaxNode.V1Node.Generation = nodeWithRevision.Node.Generation
			fornaxNode.V1Node.ResourceVersion = nodeWithRevision.Node.ResourceVersion
			fornaxNode.V1Node.CreationTimestamp = nodeWithRevision.Node.CreationTimestamp
			fornaxNode.Revision = nodeWithRevision.Revision
		}
	}

	SetNodeStatus(&fornaxNode)
	return &fornaxNode, nil
}
