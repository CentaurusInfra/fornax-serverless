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
	"time"

	v1 "k8s.io/api/core/v1"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

var _ RuntimeService = &FakeRuntimeService{}

type FakeRuntimeService struct{}

// GetPodStatus implements RuntimeService
func (*FakeRuntimeService) GetPodStatus(podSandboxID string, containerIDs []string) (*PodStatus, error) {
	panic("unimplemented")
}

// CreateContainer implements RuntimeService
func (*FakeRuntimeService) CreateContainer(pod *v1.Pod, container *v1.Container, pullSecrets []v1.Secret) (*ContainerStatus, error) {
	panic("unimplemented")
}

// CreateSandbox implements RuntimeService
func (*FakeRuntimeService) CreateSandbox(pod *v1.Pod, pullSecrets []v1.Secret) (*Pod, error) {
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
func (*FakeRuntimeService) TerminateContainer(containerID string, gracePeriod time.Duration) error {
	panic("unimplemented")
}

// TerminatePod implements RuntimeService
func (*FakeRuntimeService) TerminatePod(podSandboxID string) error {
	panic("unimplemented")
}
