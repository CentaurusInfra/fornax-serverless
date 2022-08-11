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
	"time"

	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	podcontainer "centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod/container"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/kubelet/metrics"
)

func ValidatePodSpec(apiPod *v1.Pod) []error {

	errors := []error{}
	// scheme.Scheme.Convert(in interface{}, out interface{}, context interface{})
	// opts := validation.PodValidationOptions{}
	// out := &core.Pod{}
	// s := conversion.
	// corev1.Convert_v1_Pod_To_core_Pod(apiPod, out, s)
	// validation.ValidatePodCreate(corePod, opts)
	return errors
}

func ValidateConfigMapSpec(configMap *v1.ConfigMap) []error {

	errors := []error{}
	// opts := validation.PodValidationOptions{}
	// out := &core.Pod{}
	// s := conversion.
	// corev1.Convert_v1_Pod_To_core_Pod(apiPod, out, s)
	// validation.ValidatePodCreate(corePod, opts)
	return errors
}

func ValidateSecretSpec(secret *v1.Secret) []error {

	errors := []error{}
	// opts := validation.PodValidationOptions{}
	// out := &core.Pod{}
	// s := conversion.
	// corev1.Convert_v1_Pod_To_core_Pod(apiPod, out, s)
	// validation.ValidatePodCreate(corePod, opts)
	return errors
}

func (a *PodActor) CreatePod() (err error) {
	pod := a.pod.Pod

	var firstSeenTime time.Time = time.Now()
	metrics.PodWorkerStartDuration.Observe(metrics.SinceInSeconds(firstSeenTime))

	// Create Cgroups for the pod and apply resource parameters
	klog.InfoS("Create Pod Cgroup", "pod", types.UniquePodName(a.pod))
	pcm := a.dependencies.QosManager
	if pcm.IsPodCgroupExist(pod) {
		// daemon pod can be recreated, allow cgroup exist for daemon pod cgroup
		if !a.pod.Daemon {
			pcm.UpdateQOSCgroups()
			return fmt.Errorf("cgroup already exist for pod %s, possibly previous cgroup not cleaned", types.UniquePodName(a.pod))
		}
	} else {
		err := pcm.CreatePodCgroup(pod)
		if err != nil {
			klog.ErrorS(err, "Failed to create pod cgroup ", "pod", types.UniquePodName(a.pod))
			return err
		}
		// call update qos to make sure cgroup manager internal state update to date
		pcm.UpdateQOSCgroups()
	}

	// Make data directories for the pod
	klog.InfoS("Make Pod data dirs", "pod", types.UniquePodName(a.pod))
	if err := MakePodDataDirs(a.nodeConfig.RootPath, pod); err != nil {
		klog.ErrorS(err, "Unable to make pod data directories for pod", "pod", types.UniquePodName(a.pod))
		return err
	}

	// Make log directories for the pod
	klog.InfoS("Make Pod log dirs", "pod", types.UniquePodName(a.pod))
	if err := MakePodLogDir(a.nodeConfig.PodLogRootPath, pod); err != nil {
		klog.ErrorS(err, "Unable to make pod data directories for pod", "pod", types.UniquePodName(a.pod))
		return err
	}

	// TODO, Try to attach and mount volumes into pod, mounted vol will be mounted into container later, do not support volume for now
	klog.InfoS("Prepare pod volumes", "pod", types.UniquePodName(a.pod))
	if err := a.dependencies.VolumeManager.WaitForAttachAndMount(pod); err != nil {
		klog.ErrorS(err, "Unable to attach or mount volumes for pod; skipping pod", "pod", types.UniquePodName(a.pod))
		return err
	}

	// TODO, Fetch the pull secrets for the pod, for now assume no secrect required
	// pullSecrets := GetPullSecretsForPod(pod)
	klog.InfoS("Pull pod secret", "pod", types.UniquePodName(a.pod))
	pullSecrets := []*v1.Secret{}

	klog.InfoS("Create pod sandbox", "pod", types.UniquePodName(a.pod))
	var runtimePod *runtime.Pod
	runtimePod, err = a.createPodSandbox()
	if err != nil {
		klog.ErrorS(err, "Failed to create pod sandbox", "pod", types.UniquePodName(a.pod))
		return err
	}
	klog.InfoS("Get pod sandbox", "pod", types.UniquePodName(a.pod), "sandbox", runtimePod.Sandbox)

	if runtimePod.Sandbox != nil {
		a.pod.RuntimePod = runtimePod
	} else {
		klog.ErrorS(err, "Failed to get sandbox obj, but do get a sandbox id", "Pod", types.UniquePodName(a.pod), "SandboxId", runtimePod.Id)
		return err
	}

	klog.InfoS("Start pod init containers", "pod", types.UniquePodName(a.pod))
	var runtimeContainer *runtime.Container
	for _, v1InitContainer := range pod.Spec.InitContainers {
		runtimeContainer, err = a.createContainer(runtimePod.SandboxConfig, &v1InitContainer, pullSecrets)
		if err != nil {
			klog.ErrorS(err, "Cannot create init container", "Pod", types.UniquePodName(a.pod), "Container", v1InitContainer.Name)
			return err
		}
		container := &types.FornaxContainer{
			State:            types.ContainerStateCreating,
			InitContainer:    true,
			ContainerSpec:    v1InitContainer.DeepCopy(),
			RuntimeContainer: runtimeContainer,
			ContainerStatus:  &runtime.ContainerStatus{},
		}
		a.pod.Containers[v1InitContainer.Name] = container
		runtimePod.Containers[v1InitContainer.Name] = runtimeContainer.Container

		klog.InfoS("New pod container actor", "pod", types.UniquePodName(a.pod), "container", container.ContainerSpec.Name)
		// start container actor, container actor will start runtime container, and start to probe it
		containerActor := podcontainer.NewPodContainerActor(a.Reference(), a.pod, container, a.dependencies)
		a.ContainerActors[v1InitContainer.Name] = containerActor
		containerActor.Start()
	}

	klog.InfoS("Start pod containers", "podName", types.UniquePodName(a.pod))
	for _, v1Container := range pod.Spec.Containers {
		runtimeContainer, err = a.createContainer(runtimePod.SandboxConfig, &v1Container, []*v1.Secret{})
		if err != nil {
			klog.ErrorS(err, "cannot create container", "Pod", types.UniquePodName(a.pod), "Container", v1Container.Name)
			return err
		}
		container := &types.FornaxContainer{
			State:            types.ContainerStateCreating,
			InitContainer:    false,
			ContainerSpec:    v1Container.DeepCopy(),
			RuntimeContainer: runtimeContainer,
			ContainerStatus:  &runtime.ContainerStatus{},
		}
		a.pod.Containers[v1Container.Name] = container
		runtimePod.Containers[v1Container.Name] = runtimeContainer.Container

		// start container actor, container actor will start runtime container, and start to probe it
		containerActor := podcontainer.NewPodContainerActor(a.Reference(), a.pod, container, a.dependencies)
		a.ContainerActors[v1Container.Name] = containerActor
		containerActor.Start()
	}

	// TODO
	// update resource manager about resource usage
	return nil
}

func (a *PodActor) TerminatePod(gracefulPeriod time.Duration, force bool) (bool, error) {
	pod := a.pod
	if pod.FornaxPodState != types.PodStateTerminating && pod.FornaxPodState != types.PodStateFailed {
		return false, fmt.Errorf("Pod %s is not terminatable since it's still in state %s", types.UniquePodName(a.pod), a.pod.FornaxPodState)
	}

	terminatedContainers := []string{}
	allContainerTerminated := true
	for n, c := range pod.Containers {
		if podcontainer.ContainerExit(c.ContainerStatus) || force {
			klog.InfoS("Terminating stopped container", "pod", types.UniquePodName(pod), "container", n)
			if err := a.terminateContainer(c); err == nil {
				terminatedContainers = append(terminatedContainers, c.ContainerSpec.Name)
			} else {
				return false, err
			}
		} else {
			klog.InfoS("Notify running container to stop", "pod", types.UniquePodName(pod), "container", n)
			a.notifyContainer(c.ContainerSpec.Name, internal.PodContainerStopping{
				Pod:         pod,
				Container:   c,
				GracePeriod: gracefulPeriod,
			})
			allContainerTerminated = false
		}
	}

	// remove terminated container and actor
	for _, v := range terminatedContainers {
		actor, found := a.ContainerActors[v]
		if found {
			actor.Stop()
			delete(a.ContainerActors, v)
		}
		delete(pod.Containers, v)
	}

	return allContainerTerminated, nil
}

func (a *PodActor) CleanupPod() (err error) {
	// cleanup podsandbox
	klog.InfoS("Remove Pod sandbox", "pod", types.UniquePodName(a.pod))
	pod := a.pod.Pod
	if a.pod.RuntimePod != nil && a.pod.RuntimePod.Sandbox != nil {
		err = a.removePodSandbox(a.pod.RuntimePod.Sandbox.Id, a.pod.RuntimePod.SandboxConfig)
		if err != nil {
			klog.ErrorS(err, "Failed to remove pod sandbox")
			return err
		}
	}

	// TODO, Try to unmount volumes into pod, mounted vol will be detached by volumemanager if volume not required anymore
	klog.InfoS("Unmount Pod volume", "pod", types.UniquePodName(a.pod))
	if err := a.dependencies.VolumeManager.UnmountPodVolume(pod); err != nil {
		klog.ErrorS(err, "Unable to unmount volumes for pod", "pod", types.UniquePodName(a.pod))
		return err
	}

	// Remove data directories for the pod
	klog.InfoS("Remove Pod Data dirs", "pod", types.UniquePodName(a.pod))
	if err := CleanupPodDataDirs(a.nodeConfig.RootPath, pod); err != nil {
		klog.ErrorS(err, "Unable to remove pod data directories for pod", "pod", types.UniquePodName(a.pod))
		return err
	}

	// Remove log directories for the pod
	klog.InfoS("Remove Pod log dirs", "pod", types.UniquePodName(a.pod))
	if err := CleanupPodLogDir(a.nodeConfig.PodLogRootPath, pod); err != nil {
		klog.ErrorS(err, "Unable to remove pod log directories for pod", "pod", types.UniquePodName(a.pod))
		return err
	}

	// remove cgroups for the pod and apply resource parameters
	klog.InfoS("Remove Pod Cgroup", "pod", types.UniquePodName(a.pod))
	pcm := a.dependencies.QosManager
	if pcm.IsPodCgroupExist(pod) {
		err := pcm.DeletePodCgroup(pod)
		if err != nil {
			return fmt.Errorf("Cgroup deletion failed for pod %v, err %v", pod.UID, err)
		}
		// call update qos to make sure cgroup manager internal state update to date
		pcm.UpdateQOSCgroups()
	} else {
		// call update qos to make sure cgroup manager internal state update to date
		pcm.UpdateQOSCgroups()
	}

	// TODO
	// update resource manager about resource usage
	return nil
}
