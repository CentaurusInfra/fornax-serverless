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
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"

	"github.com/google/uuid"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func BuildATestDaemonPod() *v1.Pod {
	uid := uuid.New()
	daemonPod := BuildV1Pod(types.UID(uid.String()),
		default_config.DefaultFornaxCoreNodeNameSpace,
		"quark_daemon",
		true,
		[]v1.Container{
			{
				Name:      "busybox",
				Image:     "busybox:latest",
				Command:   []string{},
				Args:      []string{},
				Stdin:     false,
				StdinOnce: false,
				TTY:       false,
			},
		},
		[]v1.Container{BuildContainer("webserver", "nginx:latest", 0, 0, []v1.EnvVar{{
			Name:  "NGINX_PORT",
			Value: "8081",
			// ValueFrom: &v1.EnvVarSource{
			//  FieldRef:         &v1.ObjectFieldSelector{},
			//  ResourceFieldRef: &v1.ResourceFieldSelector{},
			//  ConfigMapKeyRef:  &v1.ConfigMapKeySelector{},
			//  SecretKeyRef:     &v1.SecretKeySelector{},
			// },
		}})},
	)

	return daemonPod
}

func BuildATestPodCreate(appId string) *grpc.FornaxCoreMessage {
	uid := uuid.New()
	testPod := BuildV1Pod(types.UID(uid.String()),
		"test_namespace",
		"test_nginx",
		false,
		[]v1.Container{},
		[]v1.Container{BuildContainer("webserver", "nginx:latest", 8082, 80, []v1.EnvVar{})},
	)

	// update node, send node configuration
	podId := uuid.New().String()
	mode := grpc.PodCreate_Active
	body := grpc.FornaxCoreMessage_PodCreate{
		PodCreate: &grpc.PodCreate{
			PodIdentifier: &podId,
			AppIdentifier: &appId,
			Mode:          &mode,
			Pod:           testPod,
			ConfigMap:     &v1.ConfigMap{},
		},
	}
	messageType := grpc.MessageType_POD_CREATE
	msg := &grpc.FornaxCoreMessage{
		MessageType: &messageType,
		MessageBody: &body,
	}
	return msg
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

func BuildV1Pod(uid types.UID, namespace, name string, hostnetwork bool, initContainers []v1.Container, containers []v1.Container) *v1.Pod {
	enableServiceLinks := false
	setHostnameAsFQDN := false
	mountServiceAccount := false
	shareProcessNamespace := false
	deletionGracePeriod := int64(120)
	activeGracePeriod := int64(120)
	daemonPod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			UID:             uid,
			ResourceVersion: "1",
			CreationTimestamp: metav1.Time{
				Time: time.Now(),
			},
			DeletionGracePeriodSeconds: &deletionGracePeriod,
			Labels:                     map[string]string{fornaxv1.LabelFornaxCoreApplication: namespace},
			Annotations:                map[string]string{},
			OwnerReferences:            []metav1.OwnerReference{},
			Finalizers:                 []string{},
			ManagedFields:              []metav1.ManagedFieldsEntry{},
		},
		Spec: v1.PodSpec{
			Volumes:                       []v1.Volume{},
			InitContainers:                initContainers,
			Containers:                    containers,
			EphemeralContainers:           []v1.EphemeralContainer{},
			RestartPolicy:                 v1.RestartPolicyAlways,
			TerminationGracePeriodSeconds: &deletionGracePeriod,
			ActiveDeadlineSeconds:         &activeGracePeriod,
			DNSPolicy:                     v1.DNSNone,
			NodeSelector:                  map[string]string{},
			AutomountServiceAccountToken:  &mountServiceAccount,
			// ServiceAccountName:            "",
			// DeprecatedServiceAccount:      "",
			// NodeName:                     "",
			HostNetwork:           hostnetwork,
			HostPID:               false,
			HostIPC:               false,
			ShareProcessNamespace: &shareProcessNamespace,
			SecurityContext:       &v1.PodSecurityContext{
				// SELinuxOptions: &v1.SELinuxOptions{
				//  User:  "",
				//  Role:  "",
				//  Type:  "",
				//  Level: "",
				// },
				// WindowsOptions:      &v1.WindowsSecurityContextOptions{},
				// RunAsUser:           new(int64),
				// RunAsGroup:          new(int64),
				// RunAsNonRoot:        new(bool),
				// SupplementalGroups:  []int64{},
				// FSGroup:             new(int64),
				// Sysctls:             []v1.Sysctl{},
				// FSGroupChangePolicy: &"",
				// SeccompProfile:      &v1.SeccompProfile{},
			},
			ImagePullSecrets: []v1.LocalObjectReference{},
			Hostname:         "",
			Subdomain:        default_config.DefaultDomainName,
			Affinity:         &v1.Affinity{},
			// SchedulerName:             "",
			Tolerations: []v1.Toleration{},
			HostAliases: []v1.HostAlias{},
			// PriorityClassName:         "",
			// Priority:       new(int32),
			// DNSConfig:      &v1.PodDNSConfig{},
			ReadinessGates: []v1.PodReadinessGate{
				{
					ConditionType: v1.ContainersReady,
				},
			},
			// RuntimeClassName:          new(string),
			EnableServiceLinks:        &enableServiceLinks,
			Overhead:                  map[v1.ResourceName]resource.Quantity{},
			TopologySpreadConstraints: []v1.TopologySpreadConstraint{},
			SetHostnameAsFQDN:         &setHostnameAsFQDN,
			OS:                        &v1.PodOS{},
		},
		Status: v1.PodStatus{},
	}
	return daemonPod
}

func UniquePodName(pod *v1.Pod) string {
	return fmt.Sprintf("Namespace:%s,Name:%s", pod.Namespace, pod.Name)
}

func GetPodResource(v1pod *v1.Pod) *v1.ResourceList {
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

func MergePodStatus(oldPod, newPod *v1.Pod) {
	oldPod.Status = *newPod.Status.DeepCopy()
}

func IsInGracePeriod(pod *v1.Pod) bool {
	graceSeconds := pod.GetDeletionGracePeriodSeconds()
	deleteTimeStamp := pod.GetDeletionTimestamp()
	return graceSeconds != nil && deleteTimeStamp != nil && deleteTimeStamp.Add((time.Duration(*graceSeconds) * time.Second)).After(time.Now())
}
