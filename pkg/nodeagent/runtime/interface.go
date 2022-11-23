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

package runtime

import (
	"time"

	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const (
	QuarkRuntime   = "quark"
	QuarkRuntime_D = "quark_d"
	RuncRuntime    = "runc"
)

type RuntimeService interface {
	GetRuntimeStatus() (*criv1.RuntimeStatus, error)

	GetPods(includeContainers bool) ([]*Pod, error)

	GetPodSandbox(podSandboxID string) (*criv1.PodSandbox, error)

	GetPodStatus(podSandboxID string, containerIDs []string) (*PodStatus, error)

	GetContainerStatus(containerID string) (*ContainerStatus, error)

	CreateSandbox(sandboxConfig *criv1.PodSandboxConfig, runtimeClassName string) (*Pod, error)

	CreateContainer(podSandboxID string, containerConfig *criv1.ContainerConfig, podSandboxConfig *criv1.PodSandboxConfig) (*Container, error)

	StartContainer(containerID string) error

	StopContainer(containerID string, gracePeriod time.Duration) error

	TerminatePod(podSandboxID string, containerIDs []string) error

	TerminateContainer(containerID string) error

	ExecCommand(containerID string, cmd []string, timeout time.Duration) ([]byte, []byte, error)

	GetImageLabel() (string, error)

	HibernateContainer(containerID string) error

	WakeupContainer(containerID string) error
}

type CRIVersion struct {
	Version           string
	RuntimeName       string
	RuntimeVersion    string
	RuntimeApiVersion string
}

// Pod is a sandbox container and a group of containers.
type Pod struct {
	Id            string
	IPs           []string
	SandboxConfig *criv1.PodSandboxConfig
	Sandbox       *criv1.PodSandbox
	Containers    map[string]*criv1.Container
}

// Pod is a sandbox container and a group of containers.
type Container struct {
	Id              string
	ContainerConfig *criv1.ContainerConfig
	Container       *criv1.Container
}

// PodStatus represents the status of the pod and its containers.
type PodStatus struct {
	SandboxStatus     *criv1.PodSandboxStatus
	ContainerStatuses map[string]*criv1.ContainerStatus
}

// ContainerStatus represents the cri status of a container and fornax status.
type ContainerStatus struct {
	RuntimeStatus *criv1.ContainerStatus
}
