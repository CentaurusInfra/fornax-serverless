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

package util

import (
	"fmt"
	"reflect"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func BuildADummyTerminatedPod(metaNamespaceName string) *v1.Pod {
	namespace, name, err := cache.SplitMetaNamespaceKey(metaNamespaceName)
	if err != nil {
		return nil
	}

	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			GenerateName:    metaNamespaceName,
			Namespace:       namespace,
			SelfLink:        "",
			UID:             "",
			ResourceVersion: "0",
			Generation:      0,
			CreationTimestamp: metav1.Time{
				Time: time.Now(),
			},
			DeletionTimestamp: &metav1.Time{
				Time: time.Now(),
			},
			DeletionGracePeriodSeconds: new(int64),
			Labels:                     map[string]string{},
			Annotations:                map[string]string{},
			OwnerReferences:            []metav1.OwnerReference{},
			Finalizers:                 []string{},
			ZZZ_DeprecatedClusterName:  "",
			ManagedFields:              []metav1.ManagedFieldsEntry{},
		},
		Spec: v1.PodSpec{},
		Status: v1.PodStatus{
			Phase: v1.PodFailed,
			Conditions: []v1.PodCondition{{
				Type:   v1.PodReady,
				Status: v1.ConditionFalse,
				LastProbeTime: metav1.Time{
					Time: time.Now(),
				},
				LastTransitionTime: metav1.Time{
					Time: time.Now(),
				},
				Reason:  "unknown pod",
				Message: "unknown pod",
			}},
			Message:                    "unknown pod",
			Reason:                     "unknown pod",
			NominatedNodeName:          "",
			HostIP:                     "",
			PodIP:                      "",
			PodIPs:                     []v1.PodIP{},
			StartTime:                  &metav1.Time{},
			InitContainerStatuses:      []v1.ContainerStatus{},
			ContainerStatuses:          []v1.ContainerStatus{},
			QOSClass:                   "",
			EphemeralContainerStatuses: []v1.ContainerStatus{},
		},
	}
	return pod
}

func BuildContainer(name, image string, hostPort int32, containerPort int32, envs []v1.EnvVar) v1.Container {
	container := v1.Container{
		Name:          name,
		Image:         image,
		Command:       []string{},
		Args:          []string{},
		WorkingDir:    "",
		EnvFrom:       []v1.EnvFromSource{},
		Env:           envs,
		Resources:     v1.ResourceRequirements{},
		VolumeMounts:  []v1.VolumeMount{},
		VolumeDevices: []v1.VolumeDevice{},
		// LivenessProbe:            &v1.Probe{},
		// ReadinessProbe:           &v1.Probe{},
		// StartupProbe:             &v1.Probe{},
		// Lifecycle:                &v1.Lifecycle{},
		TerminationMessagePath:   "",
		TerminationMessagePolicy: "",
		ImagePullPolicy:          v1.PullIfNotPresent,
		// SecurityContext:          &v1.SecurityContext{},
		Stdin:     false,
		StdinOnce: false,
		TTY:       false,
	}

	if hostPort > 1000 && hostPort < 65535 {
		container.Ports = []v1.ContainerPort{
			{
				Name:          fmt.Sprintf("%s-port", name),
				HostPort:      hostPort,
				ContainerPort: containerPort,
				Protocol:      "TCP",
				// HostIP:        "",
			},
		}
	}

	return container
}

func GetPodResourceList(v1pod *v1.Pod) *v1.ResourceList {
	resourceList := v1.ResourceList{}
	for _, v := range v1pod.Spec.Containers {
		if v.Resources.Requests.Cpu().Sign() > 0 {
			cpu := resourceList.Cpu()
			cpu.Add(*v.Resources.Requests.Cpu())
			resourceList[v1.ResourceCPU] = *cpu
		}

		if v.Resources.Requests.Memory().Sign() > 0 {
			mem := resourceList.Memory()
			mem.Add(*v.Resources.Requests.Memory())
			resourceList[v1.ResourceMemory] = *mem
		}

		if v.Resources.Requests.StorageEphemeral().Sign() > 0 {
			storage := resourceList.StorageEphemeral()
			storage.Add(*v.Resources.Requests.StorageEphemeral())
			resourceList[v1.ResourceStorage] = *storage
		}

		if v.Resources.Requests.Storage().Sign() > 0 {
			storage := resourceList.Storage()
			storage.Add(*v.Resources.Requests.Storage())
			resourceList[v1.ResourceStorage] = *storage
		}

		if v.Resources.Requests.Pods().Sign() > 0 {
			pods := resourceList.Pods()
			pods.Add(*v.Resources.Requests.Pods())
			resourceList[v1.ResourcePods] = *pods
		}
	}

	return &resourceList
}

func MergePod(oldPod, newPod *v1.Pod) {
	oldPod.Status = *newPod.Status.DeepCopy()
	oldPod.ResourceVersion = newPod.ResourceVersion

	for k, v := range newPod.GetLabels() {
		oldPod.Labels[k] = v
	}

	for k, v := range newPod.GetAnnotations() {
		oldPod.Annotations[k] = v
	}

	if newPod.DeletionTimestamp != nil && oldPod.DeletionTimestamp == nil {
		oldPod.DeletionTimestamp = newPod.DeletionTimestamp
	}

	if newPod.DeletionGracePeriodSeconds != nil && oldPod.DeletionGracePeriodSeconds == nil {
		oldPod.DeletionGracePeriodSeconds = newPod.DeletionGracePeriodSeconds
	}

	// pod spec could be modified by NodeAgent, especially container port mapping
	if !reflect.DeepEqual(oldPod.Spec, newPod.Spec) {
		oldPod.Spec = newPod.Spec
	}
}

func PodIsRunning(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodRunning
}

func PodIsPending(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodPending
}

func PodNotTerminated(pod *v1.Pod) bool {
	return !PodIsTerminated(pod)
}

func PodIsTerminated(pod *v1.Pod) bool {
	return (pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed)
}

func PodNotInGracePeriod(pod *v1.Pod) bool {
	return !PodInGracePeriod(pod)
}

func PodInGracePeriod(pod *v1.Pod) bool {
	graceSeconds := pod.GetDeletionGracePeriodSeconds()
	deleteTimeStamp := pod.GetDeletionTimestamp()
	return graceSeconds != nil && deleteTimeStamp != nil && deleteTimeStamp.Add((time.Duration(*graceSeconds) * time.Second)).After(time.Now())
}
