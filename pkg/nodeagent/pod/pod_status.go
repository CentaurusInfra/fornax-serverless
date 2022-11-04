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

package pod

import (
	"errors"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	ErrCreateSandboxConfig   = errors.New("CreateSandboxConfigError")
	ErrCreateSandbox         = errors.New("CreateSandboxError")
	ErrCreateContainerConfig = errors.New("CreateContainerConfigError")
	ErrCreateContainer       = errors.New("CreateContainerError")
	ErrRunContainer          = errors.New("RunContainerError")
	ErrRecoverPod            = errors.New("RecoverPodError")
)

func SetPodStatus(fppod *types.FornaxPod, node *v1.Node) {

	// pod phase
	podStatus := fppod.Pod.Status.DeepCopy()
	podStatus.Phase = ToV1PodPhase(fppod)
	if node != nil {
		podStatus.HostIP = node.Status.Addresses[0].Address
	}

	// DeletionTimestamp
	if (podStatus.Phase == v1.PodSucceeded || podStatus.Phase == v1.PodFailed) && fppod.Pod.DeletionTimestamp == nil {
		fppod.Pod.DeletionTimestamp = &metav1.Time{
			Time: time.Now(),
		}
	}

	// pod condition
	if podStatus.Conditions == nil {
		podStatus.Conditions = []v1.PodCondition{}
	}
	podStatus.Conditions = GetPodConditions(fppod)

	// pod ip
	if fppod.RuntimePod != nil && len(fppod.RuntimePod.IPs) > 0 {
		podStatus.PodIP = fppod.RuntimePod.IPs[0]
		podIps := []v1.PodIP{}
		for _, v := range fppod.RuntimePod.IPs {
			podIps = append(podIps, v1.PodIP{
				IP: v,
			})
		}
		podStatus.PodIPs = podIps
	}
	if podStatus.StartTime == nil {
		podStatus.StartTime = &metav1.Time{
			Time: time.Now(),
		}
		// post metrics pod start time - pod create time
	}

	//TODO
	// add resource status

	fppod.Pod.Status = *podStatus
}

func ToV1PodPhase(fppod *types.FornaxPod) v1.PodPhase {
	var podPhase v1.PodPhase

	switch fppod.FornaxPodState {
	case types.PodStateCreating:
		podPhase = v1.PodPending
	case types.PodStateCreated:
		podPhase = v1.PodPending
	case types.PodStateRunning:
		podPhase = v1.PodRunning
	case types.PodStateHibernated:
		podPhase = v1.PodRunning
	case types.PodStateTerminating:
		podPhase = v1.PodUnknown
	case types.PodStateTerminated:
		if fppod.RuntimePod == nil || fppod.RuntimePod.Sandbox == nil {
			podPhase = v1.PodFailed
		} else {
			podPhase = v1.PodSucceeded
		}
	case types.PodStateFailed:
		podPhase = v1.PodFailed
	default:
		podPhase = v1.PodUnknown
	}

	// check container runtime state
	for _, v := range fppod.Containers {
		if runtime.ContainerExitAbnormal(v.ContainerStatus) {
			podPhase = v1.PodFailed
			break
		}
	}

	return podPhase
}

func GetPodConditions(fppod *types.FornaxPod) []v1.PodCondition {
	conditions := map[v1.PodConditionType]*v1.PodCondition{}

	initReadyCondition := v1.PodCondition{
		Type:          v1.PodInitialized,
		Status:        v1.ConditionUnknown,
		LastProbeTime: metav1.Time{Time: time.Now()},
	}
	conditions[v1.PodInitialized] = &initReadyCondition

	containerReadyCondition := v1.PodCondition{
		Type:          v1.ContainersReady,
		Status:        v1.ConditionUnknown,
		LastProbeTime: metav1.Time{Time: time.Now()},
	}
	conditions[v1.ContainersReady] = &containerReadyCondition

	podReadyCondition := v1.PodCondition{
		Type:          v1.PodReady,
		Status:        v1.ConditionUnknown,
		LastProbeTime: metav1.Time{Time: time.Now()},
	}
	conditions[v1.PodReady] = &podReadyCondition

	podScheduledCondition := v1.PodCondition{
		Type:          v1.PodScheduled,
		Status:        v1.ConditionTrue,
		LastProbeTime: metav1.Time{Time: time.Now()},
	}
	conditions[v1.PodScheduled] = &podScheduledCondition

	// check init container runtime status
	allInitContainerNormal := true
	for _, v := range fppod.Containers {
		if v.InitContainer {
			if !runtime.ContainerExit(v.ContainerStatus) {
				containerReadyCondition.Status = v1.ConditionFalse
				containerReadyCondition.Message = "init container not finished yet"
				containerReadyCondition.Reason = "init container not finished yet"
				allInitContainerNormal = false
				break
			}
			if runtime.ContainerExitAbnormal(v.ContainerStatus) {
				containerReadyCondition.Status = v1.ConditionFalse
				containerReadyCondition.Message = "init container exit abnormally"
				containerReadyCondition.Reason = "init container exit abnormally"
				allInitContainerNormal = false
				break
			}
			allInitContainerNormal = allInitContainerNormal && runtime.ContainerExitNormal(v.ContainerStatus)
		}
	}
	if allInitContainerNormal {
		initReadyCondition.Status = v1.ConditionTrue
		initReadyCondition.Message = "init container exit normally"
		initReadyCondition.Reason = "init container exit normally"
	}

	// check container ready status
	allContainerNormal := true
	allContainerReady := true
	if fppod.RuntimePod == nil || (len(fppod.Pod.Spec.Containers)+len(fppod.Pod.Spec.InitContainers) != len(fppod.RuntimePod.Containers)) {
		containerReadyCondition.Status = v1.ConditionFalse
		containerReadyCondition.Message = "missing some containers"
		containerReadyCondition.Reason = "missing some containers"
	} else {
		// check runtime status
		for _, v := range fppod.Containers {
			if !v.InitContainer {
				if !runtime.ContainerRunning(v.ContainerStatus) {
					containerReadyCondition.Status = v1.ConditionFalse
					containerReadyCondition.Message = "one container is not running"
					containerReadyCondition.Reason = "one container is not running"
					allContainerNormal = false
					allContainerReady = false
					break
				}

				allContainerNormal = allContainerNormal && runtime.ContainerRunning(v.ContainerStatus)
				allContainerReady = allContainerReady && (v.State == types.ContainerStateRunning || v.State == types.ContainerStateHibernated)
			}
		}
	}
	if allContainerNormal {
		containerReadyCondition.Status = v1.ConditionTrue
		containerReadyCondition.Message = "all pod containers are running"
		containerReadyCondition.Reason = "all pod containers are running"
	}

	// check pod ready status
	if containerReadyCondition.Status == v1.ConditionTrue && initReadyCondition.Status == v1.ConditionTrue {
		if fppod.FornaxPodState == types.PodStateRunning {
			podReadyCondition.Status = v1.ConditionTrue
			podReadyCondition.Message = "all pod containers are ready"
			podReadyCondition.Reason = "all pod containers are ready"
		}
	} else {
		podReadyCondition.Status = v1.ConditionFalse
		podReadyCondition.Message = "some pod containers are not running"
		podReadyCondition.Reason = "some pod containers are not running"
	}

	// merg old condition with new condtion and delete merged new condition
	for _, oldCondition := range fppod.Pod.Status.Conditions {
		newCondtion, found := conditions[oldCondition.Type]
		if found {
			if oldCondition.Status != newCondtion.Status {
				newCondtion.LastTransitionTime = oldCondition.LastProbeTime
			}
		} else {
			newCondtion = &oldCondition
		}
	}

	// if there are still not merged new condition, append them into apiPodConditions
	newConditions := []v1.PodCondition{}
	for _, v := range conditions {
		newConditions = append(newConditions, *v)
	}

	return newConditions
}
