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

package application

import (
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/collection"
	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ApplicationPodPoolSummary struct {
	totalCount    int32
	pendingCount  int32
	deletingCount int32
	idleCount     int32
	readyCount    int32
	deletedCount  int32
}

type ApplicationPodPool struct {
	appmu sync.RWMutex
	pods  collection.ConcurrentStringSet
}

func NewApplicationPodPool() *ApplicationPodPool {
	return &ApplicationPodPool{
		appmu: sync.RWMutex{},
		pods:  *collection.NewConcurrentSet(),
	}
}

func (pool *ApplicationPodPool) addItem(identifier string) {
	pool.pods.AddItem(identifier)
}

func (pool *ApplicationPodPool) deleteItem(identifier string) {
	pool.pods.DeleteItem(identifier)
}

func (pool *ApplicationPodPool) getPodNames() []string {
	return pool.pods.GetKeys()
}

// getPodApplicationPodTemplate will translate application spec to a pod spec, it also call node port manager to allocate node port for exported each container port
func (appc *ApplicationManager) getPodApplicationPodTemplate(application *fornaxv1.Application) *v1.Pod {
	containers := []v1.Container{}
	for _, v := range application.Spec.Containers {
		containers = append(containers, *v.DeepCopy())
	}

	enableServiceLinks := false
	setHostnameAsFQDN := false
	mountServiceAccount := false
	shareProcessNamespace := false
	preemptionPolicy := v1.PreemptNever
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "",
			GenerateName:    "",
			UID:             "",
			Namespace:       application.Namespace,
			ResourceVersion: "1",
			Generation:      1,
			CreationTimestamp: metav1.Time{
				Time: time.Now(),
			},
			DeletionTimestamp:          nil,
			DeletionGracePeriodSeconds: application.DeletionGracePeriodSeconds,
			Labels: map[string]string{
				fornaxv1.LabelFornaxCoreApplication: util.UniqueApplicationName(application),
			},
			Annotations: map[string]string{},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         application.APIVersion,
					Kind:               application.Kind,
					Name:               application.Name,
					UID:                application.UID,
					Controller:         nil,
					BlockOwnerDeletion: nil,
				},
			},
			Finalizers: []string{},
		},
		Spec: v1.PodSpec{
			Volumes:                       []v1.Volume{},
			InitContainers:                []v1.Container{},
			Containers:                    containers,
			EphemeralContainers:           []v1.EphemeralContainer{},
			RestartPolicy:                 v1.RestartPolicyNever,
			TerminationGracePeriodSeconds: nil,
			ActiveDeadlineSeconds:         nil,
			DNSPolicy:                     v1.DNSNone,
			NodeSelector:                  map[string]string{},
			ServiceAccountName:            "",
			DeprecatedServiceAccount:      "",
			AutomountServiceAccountToken:  &mountServiceAccount,
			NodeName:                      "",
			HostNetwork:                   false,
			HostPID:                       false,
			HostIPC:                       false,
			ShareProcessNamespace:         &shareProcessNamespace,
			SecurityContext:               &v1.PodSecurityContext{},
			ImagePullSecrets:              []v1.LocalObjectReference{},
			Hostname:                      "",
			Subdomain:                     default_config.DefaultDomainName,
			Affinity:                      &v1.Affinity{},
			Tolerations:                   []v1.Toleration{},
			HostAliases:                   []v1.HostAlias{},
			// PriorityClassName:             "",
			// Priority:                      nil,
			// DNSConfig:                     nil,
			ReadinessGates: []v1.PodReadinessGate{
				{
					ConditionType: v1.ContainersReady,
				},
			},
			// RuntimeClassName:          nil,
			EnableServiceLinks:        &enableServiceLinks,
			PreemptionPolicy:          &preemptionPolicy,
			Overhead:                  map[v1.ResourceName]resource.Quantity{},
			TopologySpreadConstraints: []v1.TopologySpreadConstraint{},
			SetHostnameAsFQDN:         &setHostnameAsFQDN,
			OS:                        &v1.PodOS{},
		},
		Status: v1.PodStatus{
			Phase:                      v1.PodPending,
			Conditions:                 []v1.PodCondition{},
			Message:                    "",
			Reason:                     "",
			NominatedNodeName:          "",
			HostIP:                     "",
			PodIP:                      "",
			PodIPs:                     []v1.PodIP{},
			StartTime:                  nil,
			InitContainerStatuses:      []v1.ContainerStatus{},
			ContainerStatuses:          []v1.ContainerStatus{},
			QOSClass:                   "",
			EphemeralContainerStatuses: []v1.ContainerStatus{},
		},
	}
	return pod
}

func (appc *ApplicationManager) getPodsToDelete(applicationKey string, activePods []*v1.Pod, numOfDesiredDelete int) []*v1.Pod {
	podsToDelete := []*v1.Pod{}
	candidates := 0
	// add not yet scheduled or still pending node agent return status
	for _, pod := range activePods {
		if len(pod.Status.HostIP) == 0 {
			podsToDelete = append(podsToDelete, pod)
			candidates += 1
			if candidates == numOfDesiredDelete {
				return podsToDelete
			}
		}
	}

	// add pod still pending node agent return status
	for _, pod := range activePods {
		if pod.Status.Phase == v1.PodPending {
			podsToDelete = append(podsToDelete, pod)
			candidates += 1
			if candidates == numOfDesiredDelete {
				return podsToDelete
			}
		}
	}

	// add pod status is unknown
	for _, pod := range activePods {
		if pod.Status.Phase == v1.PodUnknown {
			podsToDelete = append(podsToDelete, pod)
			candidates += 1
			if candidates == numOfDesiredDelete {
				return podsToDelete
			}
		}
	}

	// pick running pod
	// TODO, use application session usage to determine least used pod
	for _, pod := range activePods {
		if pod.Status.Phase == v1.PodRunning {
			podsToDelete = append(podsToDelete, pod)
			candidates += 1
			if candidates == numOfDesiredDelete {
				return podsToDelete
			}
		}
	}
	return podsToDelete
}
