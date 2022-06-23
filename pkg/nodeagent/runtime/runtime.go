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
	"errors"
	"fmt"
	"time"

	criapi "k8s.io/cri-api/pkg/apis"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/kubelet/cri/remote"

	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const (
	DefaultTimeout = 5 * time.Second
)

var _ RuntimeService = &remoteRuntimeManager{}

type remoteRuntimeManager struct {
	runtimeService criapi.RuntimeService
}

// GetPodSandbox implements RuntimeService
func (r *remoteRuntimeManager) GetPodSandbox(podSandboxID string) (*criv1.PodSandbox, error) {
	klog.Infof(" get sandbox by podSandboxID %s", podSandboxID)
	sandboxes, err := r.runtimeService.ListPodSandbox(&criv1.PodSandboxFilter{
		Id: podSandboxID,
	})

	if len(sandboxes) > 0 {
		return sandboxes[0], err
	} else {
		return nil, err
	}
}

// Status implements cri.RuntimeService
func (r *remoteRuntimeManager) GetRuntimeStatus() (*criv1.RuntimeStatus, error) {
	klog.InfoS("Get RemoteRuntimeService Status")

	resp, err := r.runtimeService.Status(false)
	if err != nil {
		klog.ErrorS(err, "Status from runtime service failed")
		return nil, err
	}

	klog.InfoS("RemoteRuntimeService Status Response", "status", resp.Status)

	if resp.Status == nil || len(resp.Status.Conditions) < 2 {
		errorMessage := "RuntimeReady or NetworkReady condition are not set"
		err := errors.New(errorMessage)
		klog.ErrorS(err, "Status failed")
		return nil, err
	}

	return resp.GetStatus(), nil
}

func (m *remoteRuntimeManager) CreateContainer(podSandboxID string, containerConfig *criv1.ContainerConfig, podSandboxConfig *criv1.PodSandboxConfig) (*Container, error) {
	klog.Infof("Create container in sandbox %s using container config %v", podSandboxID, containerConfig)

	var containerId string
	var err error
	containerId, err = m.runtimeService.CreateContainer(podSandboxID, containerConfig, podSandboxConfig)
	if err != nil {
		return nil, err
	}
	container := &Container{
		Id:              containerId,
		ContainerConfig: containerConfig,
		Container:       nil,
	}

	var containers []*criv1.Container
	containers, err = m.runtimeService.ListContainers(&criv1.ContainerFilter{
		Id:           containerId,
		PodSandboxId: podSandboxID,
	})
	if err != nil {
		klog.ErrorS(err, "Failed to get pod container obj;", "container name", containerConfig.Metadata.Name, "pod container id", containerId)
		return container, err
	}
	if len(containers) == 1 {
		container.Container = containers[0]
	} else {
		return container, errors.New(fmt.Sprintf("do not get exact one pod container with id: %s", containerId))
	}

	return container, nil
}

// CreateSandbox implements cri.RuntimeService
// if RunPodSandbox succeeded but failed in following steps, return it still, it will be cleaned by node
func (r *remoteRuntimeManager) CreateSandbox(sandboxConfig *criv1.PodSandboxConfig, runtimeClassName string) (*Pod, error) {
	klog.Infof("Run pod sandbox using sandbox config: %v", sandboxConfig)
	podSandBoxID, err := r.runtimeService.RunPodSandbox(sandboxConfig, runtimeClassName)
	if err != nil {
		message := fmt.Sprintf("Failed to create pod sandbox for pod %s, error: %v", sandboxConfig.Metadata.Name, err)
		klog.ErrorS(err, message)
		return nil, err
	}

	pod := &Pod{
		Id:            podSandBoxID,
		IPs:           []string{},
		SandboxConfig: sandboxConfig,
		Sandbox:       nil,
		Containers:    map[string]*criv1.Container{},
	}

	// get sandbox obj
	sandboxes, err := r.runtimeService.ListPodSandbox(&criv1.PodSandboxFilter{
		Id: podSandBoxID,
	})
	if err != nil {
		klog.ErrorS(err, "Failed to get pod sandbox obj;", "pod name", sandboxConfig.Metadata.Name, "pod sandbox id", podSandBoxID)
		return pod, err
	}
	if len(sandboxes) == 1 {
		pod.Sandbox = sandboxes[0]
	} else {
		return pod, errors.New(fmt.Sprintf("do not get exact one pod sandbox with id: %s", podSandBoxID))
	}

	// get sandbox status and ip
	sandboxStatus, err := r.getPodSandboxStatus(podSandBoxID)
	if err != nil {
		klog.ErrorS(err, "Failed to get pod sandbox status;", "pod", sandboxConfig.Metadata.Name)
		return pod, err
	}
	klog.Infof("Get pod sandbox status: %v", sandboxStatus)
	if len(sandboxStatus.GetNetwork().GetIp()) > 0 {
		pod.IPs = append(pod.IPs, sandboxStatus.GetNetwork().GetIp())
	}
	for _, v := range sandboxStatus.GetNetwork().GetAdditionalIps() {
		if len(v.GetIp()) > 0 {
			pod.IPs = append(pod.IPs, v.GetIp())
		}
	}

	return pod, nil
}

// GetImageLabel implements cri.RuntimeService
func (r *remoteRuntimeManager) GetImageLabel() (string, error) {
	panic("unimplemented")
}

// GetContainerStatus implements cri.RuntimeService
func (r *remoteRuntimeManager) GetContainerStatus(containerId string) (*ContainerStatus, error) {
	response, err := r.runtimeService.ContainerStatus(containerId, false)
	if err != nil {
		return nil, err
	}

	containerStatus := &ContainerStatus{
		RuntimeStatus: response.GetStatus(),
	}
	return containerStatus, nil
}

// GetPodStatus implements cri.RuntimeService
func (r *remoteRuntimeManager) GetPodStatus(podSandboxID string, containerIDs []string) (*PodStatus, error) {
	klog.InfoS("Get pod status include sandbox and container status", "podSandboxID", podSandboxID, "containerIDs", containerIDs)
	sandboxStatus, err := r.getPodSandboxStatus(podSandboxID)
	if err != nil {
		return nil, err
	}

	podStatus := &PodStatus{
		SandboxStatus:     sandboxStatus,
		ContainerStatuses: map[string]*criv1.ContainerStatus{},
	}

	for _, v := range containerIDs {
		containerStatus, errr := r.GetContainerStatus(v)
		if errr != nil {
			return nil, err
		}
		podStatus.ContainerStatuses[v] = containerStatus.RuntimeStatus

	}
	return podStatus, nil
}

// GetPods implements cri.RuntimeService
func (r *remoteRuntimeManager) GetPods(includeContainers bool) ([]*Pod, error) {
	klog.InfoS("ListPod Sandbox and its containers", "includeContainers", includeContainers)
	podsMap := map[string]*Pod{}

	req := &criv1.PodSandboxFilter{}
	response, err := r.runtimeService.ListPodSandbox(req)
	if err != nil {
		return nil, err
	}

	for _, v := range response {
		pod := Pod{
			Id:      v.GetId(),
			IPs:     []string{},
			Sandbox: v,
		}

		sandboxStatus, err := r.getPodSandboxStatus(pod.Id)
		if err != nil {
			return nil, err
		}
		pod.IPs = []string{}
		if len(sandboxStatus.GetNetwork().GetIp()) > 0 {
			pod.IPs = append(pod.IPs, sandboxStatus.GetNetwork().GetIp())
		}
		for _, v := range sandboxStatus.GetNetwork().GetAdditionalIps() {
			if len(v.GetIp()) > 0 {
				pod.IPs = append(pod.IPs, v.GetIp())
			}
		}
		podsMap[pod.Id] = &pod
	}

	// return it as a array
	pods := []*Pod{}
	for _, v := range podsMap {
		pods = append(pods, v)
	}

	if includeContainers {
		containers, err := r.getAllContainers()
		if err != nil {
			return nil, err
		}
		for _, v := range containers {
			sandboxid := v.GetPodSandboxId()
			pod, found := podsMap[sandboxid]
			if found {
				pod.Containers[v.GetMetadata().GetName()] = v
			}
		}
	}

	return pods, nil
}

// StopContainer implements RuntimeService
func (r *remoteRuntimeManager) StopContainer(containerID string, timeout time.Duration) error {
	klog.InfoS("Stop container", "containerID", containerID)

	err := r.runtimeService.StopContainer(containerID, int64(timeout.Seconds()))
	if err != nil {
		return err
	}
	return nil

}

// StartContainer implements cri.RuntimeService
func (r *remoteRuntimeManager) StartContainer(containerID string) error {
	klog.InfoS("Start container", "containerID", containerID)

	err := r.runtimeService.StartContainer(containerID)
	if err != nil {
		return err
	}
	return nil
}

// TerminateContainer implements cri.RuntimeService
func (r *remoteRuntimeManager) TerminateContainer(containerID string) error {
	klog.InfoS("terminate container, stop immediately without gracePeriod", "containerID", containerID)

	status, err := r.GetContainerStatus(containerID)
	if err != nil {
		return err
	}

	if status.RuntimeStatus.State == criv1.ContainerState_CONTAINER_RUNNING {
		err = r.runtimeService.StopContainer(containerID, 0)
		if err != nil {
			return err
		}
	}

	err = r.runtimeService.RemoveContainer(containerID)
	if err != nil {
		return err
	}
	return nil
}

// TerminatePod implements cri.RuntimeService
func (r *remoteRuntimeManager) TerminatePod(podSandboxID string, containerIDs []string) error {
	klog.InfoS("terminate pod", "podSandboxID", podSandboxID)
	var err error
	if len(containerIDs) == 0 {
		containers, err := r.getPodContainers(podSandboxID)
		if err != nil {
			return err
		}
		for _, v := range containers {
			if v.State != criv1.ContainerState_CONTAINER_EXITED {
				klog.InfoS("terminate container in pod", "podSandboxID", podSandboxID, "containerID", v)
				err = r.TerminateContainer(v.GetId())
				if err != nil {
					return err
				}
				containerIDs = append(containerIDs, v.GetId())
			}
			r.runtimeService.RemoveContainer(v.GetId())
		}
	}

	err = r.runtimeService.StopPodSandbox(podSandboxID)
	if err != nil {
		return err
	}

	err = r.runtimeService.RemovePodSandbox(podSandboxID)
	if err != nil {
		return err
	}
	return nil
}

func (r *remoteRuntimeManager) getPodSandboxStatus(podSandboxID string) (*criv1.PodSandboxStatus, error) {
	response, err := r.runtimeService.PodSandboxStatus(podSandboxID, false)
	if err != nil {
		return nil, err
	}

	return response.GetStatus(), nil
}

func (r *remoteRuntimeManager) getAllContainers() ([]*criv1.Container, error) {
	klog.Infof(" get all containers  %s")
	filter := &criv1.ContainerFilter{}

	containers, err := r.runtimeService.ListContainers(filter)
	if err != nil {
		return nil, err
	}

	return containers, nil
}

func (r *remoteRuntimeManager) getPodContainers(podSandboxID string) ([]*criv1.Container, error) {
	klog.Infof(" get all containers by podSandboxID %s", podSandboxID)
	filter := &criv1.ContainerFilter{
		PodSandboxId: podSandboxID,
	}

	containers, err := r.runtimeService.ListContainers(filter)
	if err != nil {
		return nil, err
	}

	return containers, nil
}

func NewRemoteRuntimeService(endpoint string, connectionTimeout time.Duration) (*remoteRuntimeManager, error) {
	klog.InfoS("Connecting to runtime service", "endpoint", endpoint)
	remoteService, err := remote.NewRemoteRuntimeService(endpoint, connectionTimeout)
	if err != nil {
		klog.ErrorS(err, "Connect remote runtime failed", "address", endpoint)
		return nil, err
	}

	service := &remoteRuntimeManager{
		runtimeService: remoteService,
	}

	return service, nil
}

func (r *remoteRuntimeManager) ExecCommand(containerID string, cmd []string, timeout time.Duration) ([]byte, []byte, error) {
	return r.runtimeService.ExecSync(containerID, cmd, timeout)
}
