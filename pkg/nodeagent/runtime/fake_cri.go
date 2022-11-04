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

var _ RuntimeService = &FakeRuntimeService{}

type FakeRuntimeService struct{}

// HibernateContainer implements RuntimeService
func (*FakeRuntimeService) HibernateContainer(containerID string) error {
	panic("unimplemented")
}

// WakeupContainer implements RuntimeService
func (*FakeRuntimeService) WakeupContainer(containerID string) error {
	panic("unimplemented")
}

// StopContainer implements RuntimeService
func (*FakeRuntimeService) StopContainer(containerID string, gracePeriod time.Duration) error {
	panic("unimplemented")
}

// GetPodSandbox implements RuntimeService
func (*FakeRuntimeService) GetPodSandbox(podSandboxID string) (*criv1.PodSandbox, error) {
	panic("unimplemented")
}

// GetPodStatus implements RuntimeService
func (*FakeRuntimeService) GetPodStatus(podSandboxID string, containerIDs []string) (*PodStatus, error) {
	panic("unimplemented")
}

func (*FakeRuntimeService) ExecCommand(containerID string, cmd []string, timeout time.Duration) ([]byte, []byte, error) {
	panic("unimplemented")
}

// CreateContainer implements RuntimeService
func (*FakeRuntimeService) CreateContainer(podSandboxID string, containerConfig *criv1.ContainerConfig, podSandboxConfig *criv1.PodSandboxConfig) (*Container, error) {
	panic("unimplemented")
}

// CreateSandbox implements RuntimeService
func (*FakeRuntimeService) CreateSandbox(sandboxConfig *criv1.PodSandboxConfig, runtimeClassName string) (*Pod, error) {
	panic("unimplemented")
}

// GetCRIVersion implements RuntimeService
func (*FakeRuntimeService) GetCRIVersion() CRIVersion {
	panic("unimplemented")
}

// GetContainerStatus implements RuntimeService
func (*FakeRuntimeService) GetContainerStatus(containerID string) (*ContainerStatus, error) {
	panic("unimplemented")
}

// GetImageLabel implements RuntimeService
func (*FakeRuntimeService) GetImageLabel() (string, error) {
	panic("unimplemented")
}

// GetPods implements RuntimeService
func (*FakeRuntimeService) GetPods(includeTerminated bool) ([]*Pod, error) {
	panic("unimplemented")
}

// GetRuntimeStatus implements RuntimeService
func (*FakeRuntimeService) GetRuntimeStatus() (*criv1.RuntimeStatus, error) {
	panic("unimplemented")
}

// StartContainer implements RuntimeService
func (*FakeRuntimeService) StartContainer(containerID string) error {
	panic("unimplemented")
}

// TerminateContainer implements RuntimeService
func (*FakeRuntimeService) TerminateContainer(containerID string) error {
	panic("unimplemented")
}

// TerminatePod implements RuntimeService
func (*FakeRuntimeService) TerminatePod(podSandboxID string, containerIds []string) error {
	panic("unimplemented")
}
