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

	"centaurusinfra.io/fornax-serverless/pkg/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/dependency"
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	podcontainer "centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod/container"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

const (
	houseKeepingPeriod = 5 * time.Second
)

type HouseKeeping struct{}

type PodActor struct {
	supervisor      message.ActorRef
	stop            bool
	pod             *fornaxtypes.FornaxPod
	innerActor      *message.LocalChannelActor
	SessionActors   map[string]message.ActorRef
	ContainerActors map[string]*podcontainer.PodContainerActor
	dependencies    *dependency.Dependencies
	nodeConfig      *config.NodeConfiguration
	lastError       error
}

func (n *PodActor) Stop() error {
	n.stop = true
	for _, v := range n.ContainerActors {
		n.notify(v.Reference(), message.ActorStop{})
	}
	for _, v := range n.SessionActors {
		n.notify(v, message.ActorStop{})
	}
	n.innerActor.Stop()
	return nil
}

func (n *PodActor) Start() {
	klog.InfoS("Pod actor started", "pod", types.UniquePodName(n.pod))
	n.innerActor.Start()
	// pod actor could be restored from nodeagent db, need to recreate container actors from stored state
	if n.pod.FornaxPodState != types.PodStateTerminated {
		for k, v := range n.pod.Containers {
			if v.State != types.ContainerStateTerminated {
				// recreate container actors if it does not have
				_, found := n.ContainerActors[k]
				if !found {
					actor := podcontainer.NewPodContainerActor(n.Reference(), n.pod, v, n.dependencies)
					actor.Start()
					n.ContainerActors[v.ContainerSpec.Name] = actor
				}
			}
		}
	}

	// start house keeping loop to make sure pod reach its final state, either failed or revive from temporary runtime error
	go func() {
		ticker := time.NewTicker(houseKeepingPeriod)
		for {
			if n.stop {
				ticker.Stop()
				break
			}

			select {
			case _ = <-ticker.C:
				if n.lastError != nil {
					n.notify(n.Reference(), HouseKeeping{})
				}
			}
		}
	}()
}

func (n *PodActor) Reference() message.ActorRef {
	return n.innerActor.Reference()
}

func (n *PodActor) notify(receiver message.ActorRef, msg interface{}) error {
	return message.Send(n.Reference(), receiver, msg)
}

func (n *PodActor) notifySession(sessionName string, msg interface{}) error {
	ca, found := n.SessionActors[sessionName]
	if !found {
		return fmt.Errorf("Session with name %s not found", sessionName)
	}

	return n.notify(ca, msg)
}

func (n *PodActor) notifyContainer(containerName string, msg interface{}) error {
	ca, found := n.ContainerActors[containerName]
	if !found {
		return fmt.Errorf("Container with name %s not found", containerName)
	}

	return n.notify(ca.Reference(), msg)
}

func (a *PodActor) actorMessageProcess(msg message.ActorMessage) (interface{}, error) {
	oldPodState := a.pod.FornaxPodState
	var err error
	switch msg.Body.(type) {
	case internal.PodCreate:
		err = a.create()
	case internal.PodActive:
		err = a.active()
	case internal.PodTerminate:
		err = a.terminate(false)
	case internal.PodContainerCreated:
		err = a.onPodContainerCreated(msg.Body.(internal.PodContainerCreated))
	case internal.PodContainerStarted:
		err = a.onPodContainerStarted(msg.Body.(internal.PodContainerStarted))
	case internal.PodContainerReady:
		err = a.onPodContainerReady(msg.Body.(internal.PodContainerReady))
	case internal.PodContainerStopped:
		err = a.onPodContainerStopped(msg.Body.(internal.PodContainerStopped))
	case internal.PodContainerFailed:
		err = a.onPodContainerFailed(msg.Body.(internal.PodContainerFailed))
	case HouseKeeping:
		// retry to calibarate error and cleanup, return if cleanup failed, do not change previous error state
		if a.lastError != nil {
			err = a.handlePodError()
		}
	default:
	}

	SetPodStatus(a.pod, nil)
	a.dependencies.PodStore.PutPod(a.pod)
	if err != nil {
		a.lastError = err
		return nil, err
	} else {
		a.lastError = nil
		if oldPodState != a.pod.FornaxPodState {
			klog.InfoS("PodState changed", "pod", types.UniquePodName(a.pod), "old state", oldPodState, "new state", a.pod.FornaxPodState)
			a.notify(a.supervisor, internal.PodStatusChange{Pod: a.pod})
		}

		if a.pod.FornaxPodState == types.PodStateTerminated {
			a.lastError = a.cleanup()
			if a.lastError != nil {
				return nil, a.lastError
			} else {
				a.notify(a.supervisor, internal.PodCleanup{Pod: a.pod})
			}
		}

		return nil, nil
	}
}

func (a *PodActor) create() error {
	pod := a.pod
	klog.InfoS("Creating pod", "pod", types.UniquePodName(pod))
	if PodInTerminating(pod) {
		return fmt.Errorf("Pod %s is being terminated or already terminated", types.UniquePodName(pod))
	}

	if PodCreated(pod) {
		return fmt.Errorf("Pod %s is already created, fornaxcore may not in sync,", types.UniquePodName(pod))
	}

	a.pod.FornaxPodState = types.PodStateCreating
	err := a.CreatePod()
	if err != nil {
		return err
	}

	// set pod state created when all containers are started
	a.pod.FornaxPodState = types.PodStateCreated

	return nil
}

// terminate evacute session if there are live sessions, and terminate pod containers,
// containers are notified to exit itself, and use onPodContainerStopped call back to get container status,
// and recall this method to check if all container are finished, and finally set pod to terminted state
// termiante method do no-op if pod state is already in terminating state unless forece retry is true
func (a *PodActor) terminate(force bool) error {
	pod := a.pod
	if len(a.SessionActors) != 0 {
		pod.FornaxPodState = types.PodStateEvacuating
		// TODO evacuate session
	} else {
		if pod.FornaxPodState == types.PodStateTerminated {
			// pod is already in terminated state, idempotently retry
			return nil
		}

		klog.InfoS("Stopping container and terminating pod", "pod", types.UniquePodName(pod), "state", pod.FornaxPodState)
		pod.FornaxPodState = types.PodStateTerminating

		gracefulPeriodSeconds := int64(0)
		if pod.Pod.DeletionGracePeriodSeconds != nil {
			gracefulPeriodSeconds = *pod.Pod.DeletionGracePeriodSeconds
		}
		terminated, err := a.TerminatePod(time.Duration(gracefulPeriodSeconds), force)
		if err != nil {
			klog.ErrorS(err, "Pod termination failed, state is left in terminating to retry later,", "pod", types.UniquePodName(pod))
			return err
		}

		if terminated {
			pod.FornaxPodState = types.PodStateTerminated
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (a *PodActor) cleanup() error {
	klog.InfoS("Cleanup pod", "pod", types.UniquePodName(a.pod))
	err := a.CleanupPod()
	if err != nil {
		klog.ErrorS(err, "Cleanup pod failed", "Pod", types.UniquePodName(a.pod))
		return err
	}
	return nil
}

func (a *PodActor) active() error {
	// TODO, when container runtime support standby mode
	return nil
}

func (a *PodActor) handlePodError() (err error) {
	pod := a.pod
	klog.InfoS("Handle Pod error", "pod", types.UniquePodName(pod), "err", a.lastError)
	switch {
	case pod.FornaxPodState == types.PodStateTerminating:
		err = a.terminate(true)
	case pod.FornaxPodState != types.PodStateTerminated && pod.RuntimePod == nil:
		// pod create failed create a sandbox
		pod.FornaxPodState = types.PodStateTerminated
	case pod.FornaxPodState != types.PodStateTerminated && pod.RuntimePod != nil && pod.RuntimePod.Sandbox == nil:
		// pod create failed to get sandbox details
		var sandbox *criv1.PodSandbox
		sandbox, err = a.dependencies.CRIRuntimeService.GetPodSandbox(pod.RuntimePod.Id)
		if err == nil && sandbox == nil {
			// sandbox not found, failed
			pod.FornaxPodState = types.PodStateTerminated
		}
		if err == nil && sandbox != nil {
			// sandbox found, but anyway terminate, do not continue create containers
			pod.RuntimePod.Sandbox = sandbox
			err = a.terminate(true)
		}
	case pod.FornaxPodState != types.PodStateTerminated && pod.RuntimePod != nil && pod.RuntimePod.Sandbox != nil:
		if pod.FornaxPodState == types.PodStateRunning {
			if len(a.ContainerActors) == 0 {
				// a running pod should have at a least container
				err = a.terminate(true)
			}
		} else {
			err = a.terminate(true)
		}
	default:
	}

	return err
}

func (n *PodActor) onPodContainerCreated(msg internal.PodContainerCreated) error {
	pod := msg.Pod
	container := msg.Container
	klog.InfoS("Pod Container created", "Pod", types.UniquePodName(pod), "ContainerName", container.ContainerSpec.Name)

	if n.pod.FornaxPodState == types.PodStateTerminating || n.pod.FornaxPodState == types.PodStateTerminated {
		klog.InfoS("Pod Container created after when pod is in terminating state",
			"Pod", types.UniquePodName(pod),
			"ContainerName", container.ContainerSpec.Name,
		)
		n.terminateContainer(container)
	}

	// TODO, update pod cpu, memory resource usage
	return nil
}

func (a *PodActor) onPodContainerStarted(msg internal.PodContainerStarted) error {
	pod := msg.Pod
	container := msg.Container
	klog.InfoS("Pod Container started", "Pod", types.UniquePodName(pod), "ContainerName", container.ContainerSpec.Name)
	// TODO, update pod cpu, memory resource usage
	return nil
}

func (a *PodActor) onPodContainerStopped(msg internal.PodContainerStopped) error {
	pod := msg.Pod
	container := msg.Container
	klog.InfoS("Pod Container stopped", "Pod", types.UniquePodName(pod), "ContainerName", container.ContainerSpec.Name)

	// TODO, release cpu, memory resource stat usage
	a.handlePodContainerExit(pod, container)
	return nil
}

func (a *PodActor) onPodContainerFailed(msg internal.PodContainerFailed) error {
	pod := msg.Pod
	container := msg.Container
	klog.InfoS("Pod Container Failed", "Pod", types.UniquePodName(pod), "ContainerName", container.ContainerSpec.Name)
	a.handlePodContainerExit(pod, container)
	return nil
}

func (a *PodActor) handlePodContainerExit(pod *types.FornaxPod, container *types.FornaxContainer) {
	if container.InitContainer {
		if podcontainer.ContainerExitNormal(container.ContainerStatus) {
			// init container is expected to run to end
		} else if podcontainer.ContainerExitAbnormal(container.ContainerStatus) {
			// init container failed, terminate pod
			a.terminate(true)
		}
	} else {
		a.terminate(true)
	}
}

// when a container report it's ready, set pod to running state if all container are ready and init containers exit normally
func (a *PodActor) onPodContainerReady(msg internal.PodContainerReady) error {
	pod := a.pod
	container := msg.Container
	klog.InfoS("Pod Container is ready",
		"Pod", types.UniquePodName(pod),
		"ContainerName", container.ContainerSpec.Name,
	)
	allContainerReady := true
	for _, v := range a.pod.Containers {
		if v.InitContainer {
			allContainerReady = allContainerReady && podcontainer.ContainerExit(v.ContainerStatus)
		} else {
			allContainerReady = allContainerReady && podcontainer.ContainerRunning(v.ContainerStatus)
		}
	}

	if allContainerReady {
		pod.FornaxPodState = types.PodStateRunning
	}
	return nil
}

func NewPodActor(supervisor message.ActorRef, pod *fornaxtypes.FornaxPod, nodeConfig *config.NodeConfiguration, dependencies *dependency.Dependencies, err error) *PodActor {
	actor := &PodActor{
		supervisor:      supervisor,
		stop:            false,
		pod:             pod,
		innerActor:      nil,
		lastError:       err,
		dependencies:    dependencies,
		nodeConfig:      nodeConfig,
		SessionActors:   map[string]message.ActorRef{},
		ContainerActors: map[string]*podcontainer.PodContainerActor{},
	}
	actor.innerActor = message.NewLocalChannelActor(types.UniquePodName(pod), actor.actorMessageProcess)
	return actor
}
