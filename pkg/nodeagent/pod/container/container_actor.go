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

package container

import (
	"fmt"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/dependency"
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"k8s.io/klog/v2"
)

const (
	stateReevaluatingPeriod = 10 * time.Second
)

type PodContainerActor struct {
	stop         bool
	innerActor   message.LocalChannelActor
	pod          *types.FornaxPod
	container    *types.FornaxContainer
	dependencies *dependency.Dependencies
	supervisor   message.ActorRef
	probers      map[ProbeType]*ContainerProber
}

func (a *PodContainerActor) Reference() message.ActorRef {
	return a.innerActor.Reference()
}

func (a *PodContainerActor) Stop() {
	klog.InfoS("Stopping container actor", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name)

	for _, v := range a.probers {
		v.Stop()
	}
	a.innerActor.Stop()
}

func (a *PodContainerActor) Start() {
	klog.InfoS("Starting container actor", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name, "init container", a.container.InitContainer)
	a.innerActor.Start()

	if a.container.ContainerStatus.RuntimeStatus != nil {
		// container recovered from containerd already has a container status loaded, only need to startup probers, do not restart
		if !a.inStoppingProcess() {
			a.startStartupProber()
		}
		a.startRuntimeProber()
	} else {
		// newly created container does not have runtime status yet, it's populated by container actor's runtime status prober
		a.container.State = types.ContainerStateCreated
		a.notify(internal.PodContainerCreated{Pod: a.pod, Container: a.container})

		a.startContainer()
	}
}

func (a *PodContainerActor) notify(msg interface{}) {
	message.Send(a.innerActor.Reference(), a.supervisor, msg)
}

func (a *PodContainerActor) containerHandler(msg message.ActorMessage) (interface{}, error) {
	var err error
	var reply interface{}
	switch msg.Body.(type) {
	case internal.PodContainerStopping:
		err = a.stopContainer(msg.Body.(internal.PodContainerStopping).GracePeriod)
	case internal.PodContainerStarting:
		err = a.startContainer()
	case internal.PodOOM:
	default:
	}
	return reply, err
}

func (a *PodContainerActor) startContainer() error {
	klog.InfoS("Starting container", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name)
	err := a.dependencies.RuntimeService.StartContainer(a.container.RuntimeContainer.Id)
	if err != nil {
		klog.ErrorS(err, "Failed to start container", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name)
		a.notify(internal.PodContainerFailed{Pod: a.pod, Container: a.container})
		return err
	}

	a.startStartupProber()
	a.startRuntimeProber()
	return nil
}

// start container spec startup status prober
func (a *PodContainerActor) startStartupProber() {
	klog.InfoS("Starting container startup probers", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name)
	if !a.container.InitContainer && a.container.ContainerSpec.StartupProbe != nil {
		startupProber := NewContainerProber(a.onContainerProbeResult,
			a.pod.Pod.DeepCopy(),
			a.container.RuntimeContainer.Id,
			a.container.ContainerSpec.StartupProbe.DeepCopy(),
			StartupProbe,
			a.dependencies.RuntimeService,
		)
		a.probers[StartupProbe] = startupProber
		startupProber.Start()
	} else {
		// no startup probe, assume it started, run container post started hook
		a.onContainerStarted()
	}
}

// start container runtime status prober
func (a *PodContainerActor) startRuntimeProber() {
	runtimeStatusProber := NewContainerProber(a.onContainerProbeResult,
		a.pod.Pod.DeepCopy(),
		a.container.RuntimeContainer.Id,
		NewRuntimeStatusProbeSpec(),
		RuntimeStatusProbe,
		a.dependencies.RuntimeService,
	)
	a.probers[RuntimeStatusProbe] = runtimeStatusProber
	runtimeStatusProber.Start()
}

// handle probe result, notify pod container status change
func (a *PodContainerActor) onContainerProbeResult(result PodContainerProbeResult, probeStatus interface{}) {
	// notify pod container probe status
	switch result.ProbeType {
	case StartupProbe:
		if result.Result == ProbeResultFailed {
			// startup failure is treated as container failed, stop probe
			a.notify(internal.PodContainerFailed{Pod: a.pod, Container: a.container})
			a.probers[StartupProbe].Stop()
		} else if result.Result == ProbeResultSuccess {
			// run container post started hook, stop probe
			a.onContainerStarted()
			a.probers[StartupProbe].Stop()
		}
	case LivenessProbe:
		// liveness failure is treated as container failed, need pod to restart or terminate
		if result.Result == ProbeResultFailed {
			a.onContainerFailed()
		}
	case ReadinessProbe:
		// readiness failure is treated as container unhealthy, stop probe
		if result.Result == ProbeResultFailed {
			a.notify(internal.PodContainerUnhealthy{Pod: a.pod, Container: a.container})
			a.probers[ReadinessProbe].Stop()
		} else if result.Result == ProbeResultSuccess {
			a.onContainerReady()
			a.probers[ReadinessProbe].Stop()
		}
	case RuntimeStatusProbe:
		if result.Result == ProbeResultFailed || probeStatus == nil {
			// no status found, hesitate to make decision
			break
		}

		containerStatus := probeStatus.(*runtime.ContainerStatus)
		// if runtime container exit or disapper, check it as failed
		if containerStatus.RuntimeStatus == nil {
			klog.InfoS("Container runtime status is nil", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name)
			a.notify(internal.PodContainerFailed{Pod: a.pod, Container: a.container})
		} else {
			if a.container.ContainerStatus == nil {
				a.container.ContainerStatus = containerStatus
			} else {
				a.container.ContainerStatus.RuntimeStatus = containerStatus.RuntimeStatus
			}

			// when container exit, report it
			if runtime.ContainerExit(a.container.ContainerStatus) && !a.container.InitContainer {
				klog.InfoS("Container exit", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name, "exit code", a.container.ContainerStatus.RuntimeStatus.ExitCode, "finished at", a.container.ContainerStatus.RuntimeStatus.FinishedAt)
				a.onContainerFailed()
			}

			// use runtime status probe as readiness probe if it does not have it
			if a.container.ContainerSpec.ReadinessProbe == nil && runtime.ContainerRunning(a.container.ContainerStatus) {
				if a.container.State == types.ContainerStateStarted {
					a.onContainerReady()
				}
			}
		}
	}
}

func (a *PodContainerActor) onContainerFailed() (interface{}, error) {
	pod := a.pod
	container := a.container
	klog.InfoS("Container failed", "pod", pod.Identifier, "containerName", container.ContainerSpec.Name)
	a.notify(internal.PodContainerFailed{Pod: a.pod, Container: a.container})
	return nil, nil
}

func (a *PodContainerActor) onContainerReady() (interface{}, error) {
	pod := a.pod
	container := a.container
	// could be requested to stop when waiting for probe result
	if !a.inStoppingProcess() {
		a.container.State = types.ContainerStateRunning
		klog.InfoS("Container ready", "pod", pod.Identifier, "containerName", container.ContainerSpec.Name)
		a.notify(internal.PodContainerReady{Pod: a.pod, Container: a.container})
	}
	return nil, nil
}

// start liveness and readiness prober when container runtime status is ready
func (a *PodContainerActor) onContainerStarted() error {
	pod := a.pod
	container := a.container
	if a.container.State == types.ContainerStateCreated {
		a.container.State = types.ContainerStateStarted
		a.notify(internal.PodContainerStarted{Pod: a.pod, Container: a.container})
	}

	if a.inStoppingProcess() {
		klog.InfoS("Container is being terminated, ignore startup probe message", "pod", pod.Identifier, "containerName", container.ContainerSpec.Name)
		return nil
	}

	// start liveness and readiness prober for non init container
	if !a.container.InitContainer {
		// start pod liveness and readiness probe after startup
		klog.InfoS("Start pod liveness and readiness prober", "pod", pod.Identifier, "containerName", container.ContainerSpec.Name)
		if a.container.ContainerSpec.LivenessProbe != nil {
			prober := NewContainerProber(a.onContainerProbeResult,
				a.pod.Pod.DeepCopy(),
				a.container.RuntimeContainer.Id,
				a.container.ContainerSpec.LivenessProbe.DeepCopy(),
				LivenessProbe,
				a.dependencies.RuntimeService,
			)
			a.probers[LivenessProbe] = prober
			prober.Start()
		}
		if a.container.ContainerSpec.ReadinessProbe != nil {
			prober := NewContainerProber(a.onContainerProbeResult,
				a.pod.Pod.DeepCopy(),
				a.container.RuntimeContainer.Id,
				a.container.ContainerSpec.ReadinessProbe.DeepCopy(),
				ReadinessProbe,
				a.dependencies.RuntimeService,
			)
			a.probers[ReadinessProbe] = prober
			prober.Start()
		}

		klog.InfoS("Run post start lifecycle handler", "pod", pod.Identifier, "containerName", container.ContainerSpec.Name)
		if container.ContainerSpec.Lifecycle != nil && container.ContainerSpec.Lifecycle.PostStart != nil {
			errMsg, err := a.runLifecycleHandler(pod, container, container.ContainerSpec.Lifecycle.PostStart)
			if err != nil {
				klog.ErrorS(err, "Post start lifecycle handler failed", "pod", pod.Identifier, "containerName", container.ContainerSpec.Name, "errMsg", errMsg)
				return err
			}
		}
	}

	return nil
}

func (a *PodContainerActor) inStoppingProcess() bool {
	return a.container.State == types.ContainerStateStopping || a.container.State == types.ContainerStateStopped || a.container.State == types.ContainerStateTerminating || a.container.State == types.ContainerStateTerminated
}

func (a *PodContainerActor) stopContainer(timeout time.Duration) (err error) {
	pod := a.pod
	container := a.container
	// quark container can not be stopped when it's hibernated, wake it up, TODO, remove it after quark able to stop without waking up
	if container != nil && container.State == types.ContainerStateHibernated {
		a.dependencies.RuntimeService.WakeupContainer(a.container.ContainerStatus.RuntimeStatus.Id)
	}

	// execute prestop lifecycle handler when container is in running state
	if a.container.State == types.ContainerStateRunning && container != nil && container.RuntimeContainer != nil {
		var errMsg string
		if container.ContainerSpec.Lifecycle != nil && container.ContainerSpec.Lifecycle.PreStop != nil {
			klog.InfoS("Running pre stop lifecycle handler", "pod", pod.Identifier, "containerName", container.ContainerSpec.Name)
			done := make(chan struct{})
			go func() {
				if errMsg, err = a.runLifecycleHandler(pod, container, container.ContainerSpec.Lifecycle.PreStop); err != nil {
					klog.ErrorS(err, "Pre stop lifecycle handler failed", "pod", pod.Identifier, "containerName", container.ContainerSpec.Name, "errMsg", errMsg)
				}
			}()

			select {
			case <-time.After(time.Duration(timeout) * time.Second):
				klog.InfoS("Pre stop lifecycle handler not completed in grace period", "pod", pod.Identifier, "containerName", container.ContainerSpec.Name, "timeout", timeout)
			case <-done:
				klog.InfoS("Pre stop lifecycle handler done", "pod", pod.Identifier, "containerName", container.ContainerSpec.Name)
			}
			close(done)
		}
	}
	a.container.State = types.ContainerStateStopping

	// stop probers except runtime status prober, let it to update runtime status
	for n, prober := range a.probers {
		if n != RuntimeStatusProbe {
			prober.Stop()
		}
	}

	err = a.stopRuntimeContainer(timeout)
	if err != nil {
		klog.ErrorS(err, "Stop pod container failed", "pod", a.pod.Identifier, "containerName", a.container.ContainerSpec.Name)
		return err
	}

	if prober, found := a.probers[RuntimeStatusProbe]; found {
		prober.Stop()
	}

	a.container.State = types.ContainerStateStopped
	a.notify(internal.PodContainerStopped{Pod: pod, Container: container})
	return nil
}

func (a *PodContainerActor) stopRuntimeContainer(timeout time.Duration) (err error) {
	// pull stopped container status immediately to speed up state report and metrics
	var status *runtime.ContainerStatus
	status, err = a.dependencies.RuntimeService.GetContainerStatus(a.container.RuntimeContainer.Id)
	if err != nil {
		return err
	} else {
		if status == nil || runtime.ContainerExit(status) {
			a.container.ContainerStatus = status
			return nil
		}
		a.container.ContainerStatus = status
	}

	// call runtime to stop container
	err = a.dependencies.RuntimeService.StopContainer(a.container.RuntimeContainer.Id, timeout)
	if err != nil {
		return err
	}

	// pull stopped container status immediately to speed up state report and metrics
	status, err = a.dependencies.RuntimeService.GetContainerStatus(a.container.RuntimeContainer.Id)
	if err != nil {
		return err
	} else {
		a.container.ContainerStatus = status
	}

	return nil
}

func NewPodContainerActor(supervisor message.ActorRef, pod *types.FornaxPod, container *types.FornaxContainer, dependencies *dependency.Dependencies) *PodContainerActor {
	id := fmt.Sprintf("%s:%s", types.UniquePodName(pod), string(container.ContainerSpec.Name))
	pca := &PodContainerActor{
		stop:         false,
		pod:          pod,
		container:    container,
		dependencies: dependencies,
		supervisor:   supervisor,
		probers:      map[ProbeType]*ContainerProber{},
	}
	pca.innerActor = *message.NewLocalChannelActor(id, pca.containerHandler)
	return pca
}
