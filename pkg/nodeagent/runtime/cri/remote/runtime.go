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

package remote

import (
	"context"
	"errors"
	"fmt"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime/cri"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const (
	DefaultTimeout = 5 * time.Second
)

var _ cri.RuntimeService = &remoteRuntimeService{}

type remoteRuntimeService struct {
	CRIVersion    cri.CRIVersion
	timeout       time.Duration
	conn          *grpc.ClientConn
	runtimeClient criv1.RuntimeServiceClient
}

// CRIVersion implements cri.RuntimeService
func (r *remoteRuntimeService) GetCRIVersion() cri.CRIVersion {
	return r.CRIVersion
}

// Status implements cri.RuntimeService
func (r *remoteRuntimeService) GetRuntimeStatus() (*criv1.RuntimeStatus, error) {
	klog.InfoS("[RemoteRuntimeService] Status", "timeout", r.timeout)
	ctx, cancel := getContextWithTimeout(r.timeout)
	defer cancel()

	resp, err := r.runtimeClient.Status(ctx, &criv1.StatusRequest{
		Verbose: true,
	})
	if err != nil {
		klog.ErrorS(err, "Status from runtime service failed")
		return nil, err
	}

	klog.V(10).InfoS("[RemoteRuntimeService] Status Response", "status", resp.Status)

	if resp.Status == nil || len(resp.Status.Conditions) < 2 {
		errorMessage := "RuntimeReady or NetworkReady condition are not set"
		err := errors.New(errorMessage)
		klog.ErrorS(err, "Status failed")
		return nil, err
	}

	return resp.GetStatus(), nil
}

// CreateContainer implements cri.RuntimeService
func (r *remoteRuntimeService) CreateContainer(pod *v1.Pod, container *v1.Container, pullSecrets []v1.Secret) (*cri.ContainerStatus, error) {
	panic("unimplemented")
}

// CreateSandbox implements cri.RuntimeService
func (r *remoteRuntimeService) CreateSandbox(pod *v1.Pod, pullSecrets []v1.Secret) (*cri.Pod, error) {
	panic("unimplemented")
}

// GetImageLabel implements cri.RuntimeService
func (r *remoteRuntimeService) GetImageLabel() (string, error) {
	panic("unimplemented")
}

// GetContainerStatus implements cri.RuntimeService
func (r *remoteRuntimeService) GetContainerStatus(containerId string) (*cri.ContainerStatus, error) {
	ctx, cancel := getContextWithCancel()
	defer cancel()
	req := &criv1.ContainerStatusRequest{
		ContainerId: containerId,
	}
	response, err := r.runtimeClient.ContainerStatus(ctx, req)
	if err != nil {
		return nil, err
	}

	containerStatus := &cri.ContainerStatus{
		ID:              containerId,
		Name:            response.GetStatus().GetMetadata().GetName(),
		ContainerStatus: response.GetStatus(),
	}
	return containerStatus, nil
}

// GetPodStatus implements cri.RuntimeService
func (r *remoteRuntimeService) GetPodStatus(podSandboxID string, containerIDs []string) (*cri.PodStatus, error) {
	klog.InfoS("Get pod status include sandbox and container status", "podSandboxID", podSandboxID, "containerIDs", containerIDs)
	sandboxStatus, err := r.getPodSandboxStatus(podSandboxID)
	if err != nil {
		return nil, err
	}

	podStatus := &cri.PodStatus{
		SandboxStatus:     sandboxStatus,
		ContainerStatuses: map[string]*criv1.ContainerStatus{},
	}

	for _, v := range containerIDs {
		containerStatus, errr := r.GetContainerStatus(v)
		if errr != nil {
			return nil, err
		}
		podStatus.ContainerStatuses[v] = containerStatus.ContainerStatus

	}
	return podStatus, nil
}

// GetPods implements cri.RuntimeService
func (r *remoteRuntimeService) GetPods(includeContainers bool) ([]*cri.Pod, error) {
	klog.InfoS("ListPod Sandbox and its containers", "includeContainers", includeContainers)
	podsMap := map[string]*cri.Pod{}
	ctx, cancel := getContextWithCancel()
	defer cancel()

	req := &criv1.ListPodSandboxRequest{}
	response, err := r.runtimeClient.ListPodSandbox(ctx, req)
	if err != nil {
		return nil, err
	}

	for _, v := range response.Items {
		pod := cri.Pod{
			ID:           types.UID(v.GetMetadata().Uid),
			SandboxID:    v.GetId(),
			SandboxState: v.GetState(),
			Name:         v.Metadata.GetName(),
			Namespace:    v.Metadata.GetNamespace(),
			IPs:          []string{},
			Containers:   map[string]*criv1.Container{},
		}

		podStatus, err := r.getPodSandboxStatus(pod.SandboxID)
		if err != nil {
			return nil, err
		}
		pod.IPs = []string{}
		if len(podStatus.GetNetwork().GetIp()) > 0 {
			pod.IPs = append(pod.IPs, podStatus.GetNetwork().GetIp())
		}
		for _, v := range podStatus.GetNetwork().GetAdditionalIps() {
			if len(v.GetIp()) > 0 {
				pod.IPs = append(pod.IPs, v.GetIp())
			}
		}
		podsMap[pod.SandboxID] = &pod
	}

	if includeContainers {
		containers, err := r.getPodContainers("")
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

	// return it as a array
	pods := []*cri.Pod{}
	for _, v := range podsMap {
		pods = append(pods, v)
	}
	return pods, nil
}

// StartContainer implements cri.RuntimeService
func (r *remoteRuntimeService) StartContainer(containerID string) error {
	klog.InfoS("start container", "containerID", containerID)
	ctx, cancel := getContextWithCancel()
	defer cancel()
	req := &criv1.StartContainerRequest{
		ContainerId:          containerID,
		XXX_NoUnkeyedLiteral: struct{}{},
		XXX_sizecache:        0,
	}

	_, err := r.runtimeClient.StartContainer(ctx, req)
	if err != nil {
		return err
	}
	return nil
}

// TerminateContainer implements cri.RuntimeService
func (r *remoteRuntimeService) TerminateContainer(containerID string, gracePeriod time.Duration) error {
	klog.InfoS("terminate container", "containerID", containerID, "gracePeriod", gracePeriod)
	ctx, cancel := getContextWithCancel()
	defer cancel()
	req := &criv1.StopContainerRequest{
		ContainerId:          containerID,
		Timeout:              int64(gracePeriod.Seconds()),
		XXX_NoUnkeyedLiteral: struct{}{},
		XXX_sizecache:        0,
	}

	_, err := r.runtimeClient.StopContainer(ctx, req)
	if err != nil {
		return err
	}
	return nil
}

// TerminatePod implements cri.RuntimeService
func (r *remoteRuntimeService) TerminatePod(podSandboxID string) error {
	klog.InfoS("terminate pod", "podSandboxID", podSandboxID)
	containers, err := r.getPodContainers(podSandboxID)
	if err != nil {
		return err
	}
	for _, v := range containers {
		if v.State != criv1.ContainerState_CONTAINER_EXITED {
			klog.InfoS("terminate container in pod", "podSandboxID", podSandboxID, "containerID", v.GetId())
			err = r.TerminateContainer(v.GetId(), 0*time.Second)
			if err != nil {
				return err
			}
		}
	}

	ctx, cancel := getContextWithCancel()
	defer cancel()
	in := &criv1.StopPodSandboxRequest{
		PodSandboxId:         podSandboxID,
		XXX_NoUnkeyedLiteral: struct{}{},
		XXX_sizecache:        0,
	}

	_, err = r.runtimeClient.StopPodSandbox(ctx, in)
	if err != nil {
		return err
	}
	return nil

}

func (r *remoteRuntimeService) determineAPIVersion(conn *grpc.ClientConn) error {
	ctx, cancel := getContextWithTimeout(r.timeout)
	defer cancel()

	klog.V(4).InfoS("Finding the CRI API runtime version")
	r.runtimeClient = criv1.NewRuntimeServiceClient(conn)

	version, err := r.runtimeClient.Version(ctx, &criv1.VersionRequest{})
	if err == nil {
		r.CRIVersion = cri.CRIVersion{
			Version:           version.GetVersion(),
			RuntimeApiVersion: version.GetRuntimeApiVersion(),
			RuntimeName:       version.GetRuntimeName(),
			RuntimeVersion:    version.GetRuntimeVersion(),
		}
		klog.V(2).InfoS("Using CRI Runtime", "Version", r.CRIVersion)
	} else {
		return fmt.Errorf("unable to determine runtime API version: %w", err)
	}

	return nil
}

func (r *remoteRuntimeService) getPodSandboxStatus(podSandboxID string) (*criv1.PodSandboxStatus, error) {
	ctx, cancel := getContextWithCancel()
	defer cancel()
	req := &criv1.PodSandboxStatusRequest{
		PodSandboxId: podSandboxID,
	}
	response, err := r.runtimeClient.PodSandboxStatus(ctx, req)
	if err != nil {
		return nil, err
	}

	return response.GetStatus(), nil
}

func (r *remoteRuntimeService) getPodContainers(podSandboxID string) ([]*criv1.Container, error) {
	// get all containers and add them into pod map by podSandboxID
	ctx, cancel := getContextWithCancel()
	defer cancel()
	req := &criv1.ListContainersRequest{}
	if len(podSandboxID) == 0 {
		req.Filter = nil
	} else {
		req.Filter = &criv1.ContainerFilter{
			PodSandboxId: podSandboxID,
		}
	}

	response2, err := r.runtimeClient.ListContainers(ctx, req)
	if err != nil {
		return nil, err
	}

	return response2.GetContainers(), nil
}

func NewRemoteRuntimeService(endpoint string, connectionTimeout time.Duration) (*remoteRuntimeService, error) {
	klog.V(3).InfoS("Connecting to runtime service", "endpoint", endpoint)
	ctx, cancel := context.WithTimeout(context.Background(), connectionTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, endpoint, grpc.WithInsecure(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMsgSize)))
	if err != nil {
		klog.ErrorS(err, "Connect remote runtime failed", "address", endpoint)
		return nil, err
	}

	service := &remoteRuntimeService{
		conn:    conn,
		timeout: connectionTimeout,
	}

	if err := service.determineAPIVersion(conn); err != nil {
		return nil, err
	}

	return service, nil
}
