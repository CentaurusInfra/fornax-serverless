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

package cri

import (
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type RuntimeService interface {
	runtime.RuntimeDependency
	Name() string

	CRIVersion() (string, error)

	Status() (*criv1.RuntimeStatus, error)

	GetPods(all bool) ([]*Pod, error)

	GetPodsCache() ([]*Pod, error)

	CreateSandbox(pod *v1.Pod, pullSecrets []v1.Secret) PodStatus

	CreateContainer(pod *v1.Pod, container *v1.Container, pullSecrets []v1.Secret) ContainerStatus

	StartContainer(podID types.UID, containerID ContainerID) error

	TerminatePod(podID types.UID, gracePeriodOverride *int64) error

	TerminateContainer(podID types.UID, containerID ContainerID) error

	GetPodStatus(podID types.UID, name, namespace string) (PodStatus, error)

	GetContainerStatus(pod *v1.Pod, uid types.UID, name, namespace string) (ContainerStatus, error)

	UpdateContainer(podID types.UID, containerID ContainerID, container *v1.Container) error

	GetImageLabel() (string, error)
}

// Pod is a sandbox container and a group of containers.
type Pod struct {
	ID         types.UID
	Name       string
	Namespace  string
	IPs        []string
	Sandbox    *criv1.Container
	Containers []*criv1.Container
}

type ContainerID string

// PodStatus represents the status of the pod and its containers.
type PodStatus struct {
	ID                types.UID
	Name              string
	Namespace         string
	IPs               []string
	SandboxStatus     *criv1.PodSandboxStatus
	ContainerStatuses []*criv1.ContainerStatus
}

// ContainerWorkingStatus is a fornax container status which can put a container in standby
type ContainerWorkingStatus string

const (
	// container is in ContainerActive status when it's fullly running
	ContainerActive ContainerWorkingStatus = "Active"
	// container is in ContainerStandby status when it's pause at entry point
	ContainerStandby ContainerWorkingStatus = "Standby"
)

// ContainerWorkingStatus represents the cri status of a container and fornax status.
type ContainerStatus struct {
	ID              ContainerID
	Name            string
	WorkingStatus   ContainerWorkingStatus
	ContainerStatus *criv1.ContainerStatus
}
