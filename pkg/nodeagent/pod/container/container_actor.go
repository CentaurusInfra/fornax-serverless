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
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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

	a.innerActor.Stop()
	for _, v := range a.probers {
		v.Stop()
	}
}

func (a *PodContainerActor) Start() {
	klog.InfoS("Starting container actor", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name, "init container", a.container.InitContainer)

	a.innerActor.Start()

	if a.container.ContainerStatus.RuntimeStatus != nil {
		// recovered container actor from node agent db already has a container status loaded, only need to startup probers
		a.startStartupProbers()
	} else {
		// newly created container does not have runtime status yet, it's populated by container actor's runtime status prober
		a.container.State = types.ContainerStateCreated
		a.notify(internal.PodContainerCreated{Pod: a.pod, Container: a.container})

		if a.container.InitContainer || types.PodIsNotStandBy(a.pod) {
			a.startContainer()
		}
	}
}

func (a *PodContainerActor) notify(msg interface{}) {
	message.Send(a.innerActor.Reference(), a.supervisor, msg)
}

func (a *PodContainerActor) messageProcess(msg message.ActorMessage) (interface{}, error) {
	var err error
	var reply interface{}
	switch msg.Body.(type) {
	case internal.PodContainerStopping:
		reply, err = a.stopContainer(msg.Body.(internal.PodContainerStopping).GracePeriod)
	case internal.PodContainerStarting:
		err = a.startContainer()
	case internal.PodOOM:
	default:
	}
	return reply, err
}

func (a *PodContainerActor) startContainer() error {
	klog.InfoS("Starting container", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name)
	err := a.dependencies.CRIRuntimeService.StartContainer(a.container.RuntimeContainer.Id)
	if err != nil {
		klog.ErrorS(err, "Failed to start container", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name)
		a.notify(internal.PodContainerFailed{Pod: a.pod, Container: a.container})
		return err
	}

	a.startStartupProbers()
	return nil
}

func (a *PodContainerActor) startStartupProbers() {
	klog.InfoS("Starting container startup probers", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name)
	if !a.container.InitContainer && a.container.ContainerSpec.StartupProbe != nil {
		startupProber := NewContainerProber(a.onContainerProbeResult,
			a.pod.Pod.DeepCopy(),
			a.container.RuntimeContainer.Id,
			a.container.ContainerSpec.StartupProbe.DeepCopy(),
			StartupProbe,
			a.dependencies.CRIRuntimeService,
		)
		a.probers[StartupProbe] = startupProber
		startupProber.Start()
	} else {
		// no startup probe, assume it started
		a.onContainerStarted()
	}

	// start runtime status prober
	runtimeStatusProber := NewContainerProber(a.onContainerProbeResult,
		a.pod.Pod.DeepCopy(),
		a.container.RuntimeContainer.Id,
		NewRuntimeStatusProbeSpec(),
		RuntimeStatusProbe,
		a.dependencies.CRIRuntimeService,
	)
	a.probers[RuntimeStatusProbe] = runtimeStatusProber
	runtimeStatusProber.Start()
}

// handle probe result, notify pod container status change
func (a *PodContainerActor) onContainerProbeResult(result PodContainerProbeResult, probeStatus interface{}) {
	// klog.InfoS("Received container prober result", "pod", types.UniquePodName(a.pod), "container", a.container.ContainerSpec.Name, "result", result)
	// notify pod container status
	switch result.ProbeType {
	case StartupProbe:
		if result.Result == ProbeResultFailed {
			// startup failure is treated as container failed, need pod to restart or terminate
			a.notify(internal.PodContainerFailed{
				Pod:       a.pod,
				Container: a.container,
			})
		} else if result.Result == ProbeResultSuccess {
			// run container post started hook
			a.onContainerStarted()
		}
		// remove startup prober after received result
		a.probers[StartupProbe].Stop()
		delete(a.probers, StartupProbe)
	case LivenessProbe:
		// liveness failure is treated as container failed, need pod to restart or terminate
		if result.Result == ProbeResultFailed {
			a.onContainerFailed()
		}
	case ReadinessProbe:
		// readiness failure is treated as container unhealthy, pod can not take new traffic
		if result.Result == ProbeResultFailed {
			a.notify(internal.PodContainerUnhealthy{
				Pod:       a.pod,
				Container: a.container,
			})
		} else if result.Result == ProbeResultSuccess {
			a.onContainerReady()
		}
	case RuntimeStatusProbe:
		if result.Result == ProbeResultFailed || probeStatus == nil {
			// no status found, hesitate to make decision
			break
		}

		containerStatus := probeStatus.(*runtime.ContainerStatus)
		// if runtime container exit or disapper, check it as failed
		if containerStatus.RuntimeStatus == nil {
			a.notify(internal.PodContainerFailed{
				Pod:       a.pod,
				Container: a.container,
			})
		} else {
			// klog.InfoS("Container Runtime Status", "status", containerStatus.RuntimeStatus)
			if a.container.ContainerStatus == nil {
				a.container.ContainerStatus = containerStatus
			} else {
				a.container.ContainerStatus.RuntimeStatus = containerStatus.RuntimeStatus
			}

			// when container exit, report it
			if ContainerExit(a.container.ContainerStatus) && !a.container.InitContainer {
				a.onContainerFailed()
			}

			// use runtime status probe as readiness probe if it does not have it
			if a.container.ContainerSpec.ReadinessProbe == nil && ContainerRunning(a.container.ContainerStatus) {
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
	a.notify(internal.PodContainerFailed{Pod: a.pod, Container: a.container})

	klog.InfoS("Container failed",
		"pod", pod.Identifier,
		"podName", pod.Pod.Name,
		"containerName", container.ContainerSpec.Name,
	)

	return nil, nil
}

func (a *PodContainerActor) onContainerReady() (interface{}, error) {
	pod := a.pod
	container := a.container
	a.container.State = types.ContainerStateReady

	klog.InfoS("Container ready",
		"pod", pod.Identifier,
		"podName", pod.Pod.Name,
		"containerName", container.ContainerSpec.Name,
	)

	a.notify(internal.PodContainerReady{Pod: a.pod, Container: a.container})
	return nil, nil
}

func (a *PodContainerActor) onContainerStarted() (interface{}, error) {
	pod := a.pod
	container := a.container
	if a.container.State == types.ContainerStateCreated {
		a.container.State = types.ContainerStateStarted
		a.notify(internal.PodContainerStarted{Pod: a.pod, Container: a.container})
	}

	if a.inStoppingProcess() {
		klog.InfoS("container is being terminated, ignore startup probe message",
			"pod", pod.Identifier,
			"podName", pod.Pod.Name,
			"containerName", container.ContainerSpec.Name,
		)
		return nil, nil
	}

	// start liveness and readiness prober for non init container
	if !a.container.InitContainer {
		// start pod liveness and readiness probe after startup
		klog.InfoS("start pod liveness and readiness prober",
			"pod", pod.Identifier,
			"podName", pod.Pod.Name,
			"containerName", container.ContainerSpec.Name,
		)
		if a.container.ContainerSpec.LivenessProbe != nil {
			prober := NewContainerProber(a.onContainerProbeResult,
				a.pod.Pod.DeepCopy(),
				a.container.RuntimeContainer.Id,
				a.container.ContainerSpec.LivenessProbe.DeepCopy(),
				LivenessProbe,
				a.dependencies.CRIRuntimeService,
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
				a.dependencies.CRIRuntimeService,
			)
			a.probers[ReadinessProbe] = prober
			prober.Start()
		}

		klog.InfoS("run post start lifecycle handler",
			"pod", pod.Identifier,
			"podName", pod.Pod.Name,
			"containerName", container.ContainerSpec.Name,
		)
		if container.ContainerSpec.Lifecycle != nil && container.ContainerSpec.Lifecycle.PostStart != nil {
			errMsg, err := a.runLifecycleHandler(pod, container, container.ContainerSpec.Lifecycle.PostStart)
			if err != nil {
				klog.ErrorS(err, "post start lifecycle handler failed",
					"pod", pod.Identifier,
					"podName", pod.Pod.Name,
					"containerName", container.ContainerSpec.Name,
					"errMsg", errMsg)
				return nil, err
			}
		}
	}

	return nil, nil
}

func (a *PodContainerActor) inStoppingProcess() bool {
	return a.container.State == types.ContainerStateStopping || a.container.State == types.ContainerStateStopped || a.container.State == types.ContainerStateTerminating || a.container.State == types.ContainerStateTerminated
}

func (a *PodContainerActor) stopContainer(timeout time.Duration) (interface{}, error) {
	a.container.State = types.ContainerStateStopping
	pod := a.pod
	container := a.container

	// execute prestop lifecycle handler
	if container != nil && container.RuntimeContainer != nil {
		klog.InfoS("Running pre stop lifecycle handler",
			"pod", pod.Identifier,
			"podName", pod.Pod.Name,
			"containerName", container.ContainerSpec.Name)

		var err error
		var errMsg string
		if container.ContainerSpec.Lifecycle != nil && container.ContainerSpec.Lifecycle.PreStop != nil {
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer utilruntime.HandleCrash()
				if errMsg, err = a.runLifecycleHandler(pod, container, container.ContainerSpec.Lifecycle.PreStop); err != nil {
					klog.ErrorS(err, "pre stop lifecycle handler failed",
						"pod", pod.Identifier,
						"podName", pod.Pod.Name,
						"containerName", container.ContainerSpec.Name,
						"errMsg", errMsg)
				}
			}()

			select {
			case <-time.After(time.Duration(timeout) * time.Second):
				klog.InfoS("pre stop lifecycle handler not completed in grace period",
					"pod", pod.Identifier,
					"podName", pod.Pod.Name,
					"containerName", container.ContainerSpec.Name,
					"timeout", timeout)
			case <-done:
				klog.InfoS("pre stop lifecycle handler done",
					"pod", pod.Identifier,
					"podName", pod.Pod.Name,
					"containerName", container.ContainerSpec.Name)
			}
		}

		// call runtime to stop container
		err = a.dependencies.CRIRuntimeService.StopContainer(container.RuntimeContainer.Id, timeout)
		if err != nil {
			klog.ErrorS(err, "stop pod container failed",
				"pod", pod.Identifier,
				"podName", pod.Pod.Name,
				"containerName", container.ContainerSpec.Name)
			return nil, err
		}

		// stop probers except runtime status prober, let it to update runtime status
		for _, prober := range a.probers {
			prober.Stop()
		}

		// pull stopped container status immediately to speed up state report and metrics
		var status *runtime.ContainerStatus
		status, err = a.dependencies.CRIRuntimeService.GetContainerStatus(container.RuntimeContainer.Id)
		if err != nil {
			klog.ErrorS(err, "stop pod container failed",
				"pod", pod.Identifier,
				"podName", pod.Pod.Name,
				"containerName", container.ContainerSpec.Name)
			return nil, err
		} else {
			a.container.ContainerStatus = status
		}
	}

	a.container.State = types.ContainerStateStopped
	// notify pod container stopped
	a.notify(internal.PodContainerStopped{
		Pod:       pod,
		Container: container,
	})
	return nil, nil
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
	pca.innerActor = *message.NewLocalChannelActor(id, pca.messageProcess)
	return pca
}
