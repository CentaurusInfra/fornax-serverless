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
package pod

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"

	podcontainer "centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod/container"
	cruntime "centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
)

// createContainer starts a container and returns a message indicates why it is failed on error.
// It starts the container through the following steps:
// * pull the image
// * create the container
// * start the container
func (a *PodActor) createContainer(podSandboxConfig *criv1.PodSandboxConfig, containerSpec *v1.Container, pullSecrets []*v1.Secret,
) (*cruntime.Container, error) {

	klog.InfoS("Pull image for container", "pod", types.UniquePodName(a.pod), "container", containerSpec.Name)
	pod := a.pod.Pod
	// pull the image.
	imageRef, err := a.dependencies.ImageManager.PullImageForContainer(containerSpec, podSandboxConfig)
	if err != nil {
		klog.ErrorS(err, "Failed to pull image", "pod", types.UniquePodName(a.pod), "container", containerSpec.Name)
		return nil, err
	}

	// create the container log dir
	klog.InfoS("Create container log dir", "pod", types.UniquePodName(a.pod), "container", containerSpec.Name)
	logDir, err := BuildContainerLogsDirectory(pod.Namespace, pod.Name, pod.UID, containerSpec.Name)
	if err != nil {
		klog.ErrorS(err, "Failed to create container log", "pod", types.UniquePodName(a.pod), "container", containerSpec.Name, "logDir", logDir)
		return nil, ErrCreateContainerConfig
	}

	// target, err := spec.getTargetID(podStatus)
	// if err != nil {
	//  s, _ := grpcstatus.FromError(err)
	//  return s.Message(), ErrCreateContainerConfig
	// }

	// create the container runtime configuration
	klog.InfoS("Generate container runtime config", "pod", types.UniquePodName(a.pod), "container", containerSpec.Name)
	containerConfig, err := a.generateContainerConfig(containerSpec, imageRef)
	if err != nil {
		klog.ErrorS(err, "Failed to generate container runtime config", "pod", types.UniquePodName(a.pod), "container", containerSpec.Name)
		return nil, ErrCreateContainerConfig
	}

	// call runtime to create the container
	klog.InfoS("Call runtime to create container", "pod", types.UniquePodName(a.pod), "container", containerSpec.Name)
	runtimeContainer, err := a.dependencies.CRIRuntimeService.CreateContainer(a.pod.RuntimePod.Sandbox.Id, containerConfig, podSandboxConfig)
	if err != nil {
		klog.ErrorS(err, "Failed to call runtime to create container", "pod", types.UniquePodName(a.pod), "container", containerSpec.Name)
		return nil, ErrCreateContainer
	}
	return runtimeContainer, nil
}

func (m *PodActor) generateContainerConfig(container *v1.Container, imageRef *criv1.Image) (*criv1.ContainerConfig, error) {
	pod := m.pod.Pod
	// TODO, comment it out until we have volume supported
	// opts, cleanupAction, err := runtime.GenerateRunContainerOptions(pod, container, m.pod.RuntimePod.IPs[0], m.pod.RuntimePod.IPs)
	// if err != nil {
	//  return nil, nil, err
	// }

	_, err := BuildContainerLogsDirectory(pod.Namespace, pod.Name, pod.UID, container.Name)
	if err != nil {
		return nil, fmt.Errorf("create log directory for container %s failed: %v", container.Name, err)
	}
	containerLogsPath := ContainerLogFileName(container.Name, 0)
	podIP := ""
	if len(m.pod.RuntimePod.IPs) > 0 {
		podIP = m.pod.RuntimePod.IPs[0]
	}
	envs, err := cruntime.MakeEnvironmentVariables(pod, container, []*v1.ConfigMap{}, []*v1.Secret{}, podIP, m.pod.RuntimePod.IPs)
	if err != nil {
		return nil, err
	}

	commands := []string{}
	for _, v := range container.Command {
		cmd := v
		for _, e := range envs {
			oldv := fmt.Sprintf("$(%s)", e.Name)
			newv := e.Value
			cmd = strings.ReplaceAll(cmd, oldv, newv)
		}
		commands = append(commands, cmd)
	}

	args := []string{}
	for _, v := range container.Command {
		arg := v
		for _, e := range envs {
			oldv := fmt.Sprintf("$(%s)", e.Name)
			newv := e.Value
			arg = strings.ReplaceAll(arg, oldv, newv)
		}
		args = append(args, arg)
	}

	config := &criv1.ContainerConfig{
		Metadata: &criv1.ContainerMetadata{
			Name: container.Name,
		},
		Image:       &criv1.ImageSpec{Image: imageRef.Id},
		Command:     commands,
		Args:        args,
		WorkingDir:  container.WorkingDir,
		Labels:      newContainerLabels(container, pod),
		Annotations: newContainerAnnotations(container, pod, 0, map[string]string{}),
		// Devices:     makeDevices(opts),
		// Mounts:      makeMounts(opts, container),
		LogPath:   containerLogsPath,
		Stdin:     container.Stdin,
		StdinOnce: container.StdinOnce,
		Tty:       container.TTY,
	}

	// set platform specific configurations.
	var uid *int64
	if imageRef.Uid != nil {
		value := imageRef.GetUid().Value
		uid = &value
	}
	username := imageRef.GetUsername()
	generateLinuxContainerConfig(m.nodeConfig, container, pod, uid, username, true)

	// set environment variables
	criEnvs := make([]*criv1.KeyValue, len(envs))
	for idx := range envs {
		e := envs[idx]
		criEnvs[idx] = &criv1.KeyValue{
			Key:   e.Name,
			Value: e.Value,
		}
	}
	config.Envs = criEnvs

	return config, nil
}

func (a *PodActor) terminateContainer(container *types.Container) error {
	pod := a.pod
	klog.InfoS("Terminate container and remove it",
		"Pod", types.UniquePodName(pod),
		"ContainerName", container.ContainerSpec.Name,
	)

	// 1/ verify container is stopped in runtime
	if !podcontainer.ContainerExit(container.ContainerStatus) {
		return fmt.Errorf("container %s is not stopped yet", container.ContainerSpec.Name)
	}

	err := a.dependencies.CRIRuntimeService.TerminateContainer(container.RuntimeContainer.Id)
	if err != nil {
		klog.ErrorS(err, "stop pod container failed",
			"Pod", types.UniquePodName(pod),
			"containerName", container.ContainerSpec.Name)
		return err
	}

	return nil
}
