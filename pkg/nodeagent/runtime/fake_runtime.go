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
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

var _ RuntimeService = &FakeRuntimeService{}

type FakeRuntimeService struct{}

// CRIVersion implements RuntimeService
func (*FakeRuntimeService) CRIVersion() (string, error) {
	return "v1", nil
}

// CreateContainer implements RuntimeService
func (*FakeRuntimeService) CreateContainer(pod *v1.Pod, container *v1.Container, pullSecrets []v1.Secret) ContainerStatus {
	return ContainerStatus{
		ID:            "",
		Name:          "",
		WorkingStatus: "",
		ContainerStatuses: &criv1.ContainerStatus{
			Id: "",
			Metadata: &criv1.ContainerMetadata{
				Name:                 "",
				Attempt:              0,
				XXX_NoUnkeyedLiteral: struct{}{},
				XXX_sizecache:        0,
			},
			State:                0,
			CreatedAt:            0,
			StartedAt:            0,
			FinishedAt:           0,
			ExitCode:             0,
			Image:                &criv1.ImageSpec{},
			ImageRef:             "",
			Reason:               "",
			Message:              "",
			Labels:               map[string]string{},
			Annotations:          map[string]string{},
			Mounts:               []*criv1.Mount{},
			LogPath:              "",
			XXX_NoUnkeyedLiteral: struct{}{},
			XXX_sizecache:        0,
		},
	}
}

// CreateSandbox implements RuntimeService
func (*FakeRuntimeService) CreateSandbox(pod *v1.Pod, pullSecrets []v1.Secret) PodStatus {
	return PodStatus{
		ID:        "",
		Name:      "",
		Namespace: "",
		IPs:       []string{},
		SandboxStatus: &criv1.PodSandboxStatus{
			Id: "",
			Metadata: &criv1.PodSandboxMetadata{
				Name:                 "",
				Uid:                  "",
				Namespace:            "",
				Attempt:              0,
				XXX_NoUnkeyedLiteral: struct{}{},
				XXX_sizecache:        0,
			},
			State:                0,
			CreatedAt:            0,
			Network:              &criv1.PodSandboxNetworkStatus{},
			Linux:                &criv1.LinuxPodSandboxStatus{},
			Labels:               map[string]string{},
			Annotations:          map[string]string{},
			RuntimeHandler:       "",
			XXX_NoUnkeyedLiteral: struct{}{},
			XXX_sizecache:        0,
		},
		ContainerStatuses: []*criv1.ContainerStatus{},
	}
}

// GetContainerStatus implements RuntimeService
func (*FakeRuntimeService) GetContainerStatus(pod *v1.Pod, uid types.UID, name string, namespace string) (ContainerStatus, error) {
	return ContainerStatus{
		ID:            "",
		Name:          name,
		WorkingStatus: "",
		ContainerStatuses: &criv1.ContainerStatus{
			Id: "",
			Metadata: &criv1.ContainerMetadata{
				Name:                 name,
				Attempt:              0,
				XXX_NoUnkeyedLiteral: struct{}{},
				XXX_sizecache:        0,
			},
			State:                0,
			CreatedAt:            0,
			StartedAt:            0,
			FinishedAt:           0,
			ExitCode:             0,
			Image:                &criv1.ImageSpec{},
			ImageRef:             "",
			Reason:               "",
			Message:              "",
			Labels:               map[string]string{},
			Annotations:          map[string]string{},
			Mounts:               []*criv1.Mount{},
			LogPath:              "",
			XXX_NoUnkeyedLiteral: struct{}{},
			XXX_sizecache:        0,
		},
	}, nil
}

// GetImageLabel implements RuntimeService
func (*FakeRuntimeService) GetImageLabel() (string, error) {
	return "imagelabel", nil
}

// GetPodStatus implements RuntimeService
func (*FakeRuntimeService) GetPodStatus(podID types.UID, name string, namespace string) (PodStatus, error) {
	return PodStatus{}, nil
}

// GetPods implements RuntimeService
func (*FakeRuntimeService) GetPods(all bool) ([]*Pod, error) {
	return []*Pod{}, nil
}

// GetPodsCache implements RuntimeService
func (*FakeRuntimeService) GetPodsCache() ([]*Pod, error) {
	return []*Pod{}, nil
}

// Name implements RuntimeService
func (*FakeRuntimeService) Name() string {
	return "FakeRuntimeService"
}

// StartContainer implements RuntimeService
func (*FakeRuntimeService) StartContainer(podID types.UID, containerID ContainerID) error {
	return nil
}

// Status implements RuntimeService
func (*FakeRuntimeService) Status() (*criv1.RuntimeStatus, error) {
	return &criv1.RuntimeStatus{}, nil
}

// TerminateContainer implements RuntimeService
func (*FakeRuntimeService) TerminateContainer(podID types.UID, containerID ContainerID) error {
	return nil
}

// TerminatePod implements RuntimeService
func (*FakeRuntimeService) TerminatePod(podID types.UID, gracePeriodOverride *int64) error {
	return nil
}

// UpdateContainer implements RuntimeService
func (*FakeRuntimeService) UpdateContainer(podID types.UID, containerID ContainerID, container *v1.Container) error {
	return nil
}
