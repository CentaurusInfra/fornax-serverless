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
		n.innerActor.Reference().Send(v.Reference(), message.ActorStop{})
	}
	for _, v := range n.SessionActors {
		n.innerActor.Reference().Send(v, message.ActorStop{})
	}
	n.innerActor.Stop()
	return nil
}

func (n *PodActor) Start() {
	n.innerActor.Start()
	// pod actor could be restored from nodeagent db, need to recreate container actors from stored state
	if n.pod.PodState != types.PodStateTerminated {
		for k, v := range n.pod.Containers {
			if v.State != types.ContainerStateTerminated {
				// recreate container actors if it does not have
				_, found := n.ContainerActors[k]
				if !found {
					actor := podcontainer.NewPodContainerActor(n.Reference(), n.pod, v, n.dependencies)
					actor.Start(true)
					n.ContainerActors[v.ContainerSpec.Name] = actor
				}
			}
		}
	}

	// immediately let pod actor to evaluate its state in case of recovered from stored state
	n.Reference().Send(n.Reference(), HouseKeeping{})

	// start house keeping loop to make sure pod reach its final state, either failed or revive from temporary runtime error
	go func() {
		ticker := time.NewTicker(houseKeepingPeriod)
		for {
			if n.stop {
				ticker.Stop()
				break
			}
		}

		select {
		case _ = <-ticker.C:
			// ticking, state if it has failed last time
			if n.lastError != nil {
				n.Reference().Send(n.Reference(), HouseKeeping{})
			}
		}
	}()
}

func (n *PodActor) Reference() message.ActorRef {
	return n.innerActor.Reference()
}

func (n *PodActor) notifySession(sessionName string, msg interface{}) error {
	ca, found := n.SessionActors[sessionName]
	if !found {
		return fmt.Errorf("Session with name %s not found", sessionName)
	}

	return n.innerActor.Reference().Send(ca, msg)
}

func (n *PodActor) notifyContainer(containerName string, msg interface{}) error {
	ca, found := n.ContainerActors[containerName]
	if !found {
		return fmt.Errorf("Container with name %s not found", containerName)
	}

	return n.innerActor.Reference().Send(ca.Reference(), msg)
}

func (n *PodActor) IsNotInService() bool {
	return n.pod.PodState != fornaxtypes.PodStateRunning
}

func (n *PodActor) messageProcess(msg message.ActorMessage) (interface{}, error) {
	var err error
	switch msg.Body.(type) {
	case internal.PodCreate:
		err = n.create()
	case internal.PodActive:
		err = n.active()
	case internal.PodTerminate:
		err = n.terminate()
	case internal.PodContainerCreated:
		err = n.onPodContainerCreated(msg.Body.(internal.PodContainerCreated))
	case internal.PodContainerStarted:
		err = n.onPodContainerStarted(msg.Body.(internal.PodContainerStarted))
	case internal.PodContainerReady:
		err = n.onPodContainerReady(msg.Body.(internal.PodContainerReady))
	case internal.PodContainerStopped:
		err = n.onPodContainerStopped(msg.Body.(internal.PodContainerStopped))
	case internal.PodContainerFailed:
		err = n.onPodContainerFailed(msg.Body.(internal.PodContainerFailed))
	case HouseKeeping:
		err = n.housekeeping()
	default:
	}

	if err != nil {
		n.lastError = err
		return nil, err
	} else {
		SetPodStatus(n.pod)

		return internal.PodStatusChange{
			Pod: n.pod,
		}, nil
	}
}

func (a *PodActor) create() error {
	pod := a.pod
	klog.InfoS("Creating pod", types.UniquePodName(pod))
	if PodInTerminating(pod) {
		return fmt.Errorf("Pod %s is being terminated or already terminated, fornaxcore may not in sync,", types.UniquePodName(pod))
	}

	if PodCreated(pod) {
		return fmt.Errorf("Pod %s is already created, fornaxcore may not in sync,", types.UniquePodName(pod))
	}

	a.pod.PodState = types.PodStateCreating
	err := a.CreatePod()
	if err != nil {
		return err
	}

	// set pod state created when all containers are started
	a.pod.PodState = types.PodStateCreated

	return nil
}

func (a *PodActor) terminate() error {
	pod := a.pod
	klog.InfoS("Stop container and termiate pod", types.UniquePodName(pod))

	if len(a.SessionActors) != 0 {
		pod.PodState = types.PodStateEvacuating
		// TODO evacuate session
	} else if pod.PodState != types.PodStateTerminated {
		pod.PodState = types.PodStateTerminating

		terminated, err := a.TerminatePod(time.Duration(*pod.PodSpec.DeletionGracePeriodSeconds))
		if err != nil {
			return fmt.Errorf("Pod %s termination failed, state is left in terminating to retry later,", types.UniquePodName(pod))
		}

		if terminated {
			pod.PodState = types.PodStateTerminated
			a.cleanup()
		}
	} else {
		// pod is already in terminating state, idempotently retry
	}
	return nil
}

func (a *PodActor) cleanup() error {
	klog.InfoS("Cleanup pod ", types.UniquePodName(a.pod))
	err := a.CleanupPod()
	if err != nil {
		klog.ErrorS(err, "Cleanup pod failed", "Pod", types.UniquePodName(a.pod))
		return err
	}
	a.innerActor.Reference().Send(a.supervisor, internal.PodCleanup{
		Pod: a.pod,
	})
	return nil
}

func (a *PodActor) active() error {
	// TODO, when container runtime support standby mode
	return nil
}

func (n *PodActor) housekeeping() error {
	pod := n.pod
	var err error
	switch {
	case pod.PodState == types.PodStateTerminating:
		err = n.terminate()
	case pod.PodState == types.PodStateTerminated:
		err = n.cleanup()
	case pod.PodState == types.PodStateCreating && pod.RuntimePod == nil:
		// pod create completely failed, send pod failed message and stop actor
		pod.PodState = types.PodStateFailed
		err = n.cleanup()
	case pod.PodState == types.PodStateCreating && pod.RuntimePod != nil && pod.RuntimePod.Sandbox == nil:
		// pod create can not got sandbox details, recheck
		var sandboxStatus *criv1.PodSandbox
		sandboxStatus, err = n.dependencies.CRIRuntimeService.GetPodSandbox(pod.RuntimePod.Id)
		if err == nil && sandboxStatus != nil {
			if sandboxStatus.State == criv1.PodSandboxState_SANDBOX_NOTREADY {
				pod.PodState = types.PodStateFailed
				err = n.cleanup()
			} else if sandboxStatus.State == criv1.PodSandboxState_SANDBOX_READY {
				// sandbox is ready, start container
				// n.startContainer(podSandboxConfig *criv1.PodSandboxConfig, v1Container *v1.Container, pullSecrets []v1.Secret, initContainer bool)
			}
		}
		if err == nil && sandboxStatus == nil {
			pod.PodState = types.PodStateFailed
			err = n.cleanup()
		}
	case pod.PodState == types.PodStateCreating && pod.RuntimePod != nil && pod.RuntimePod.Sandbox != nil:
		// could be init container failed or part of container failed
		n.terminate()
	case pod.PodState == types.PodStateFailed:
		n.terminate()
	}

	return err
}

func (n *PodActor) onPodContainerCreated(msg internal.PodContainerCreated) error {
	pod := msg.Pod
	container := msg.Container
	klog.InfoS("Container created",
		"PodName", types.UniquePodName(pod),
		"ContainerName", container.ContainerSpec.Name,
	)
	if n.pod.PodState == types.PodStateTerminating || n.pod.PodState == types.PodStateTerminated {
		klog.InfoS("Container created after when pod is in terminating state",
			"PodName", types.UniquePodName(pod),
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
	klog.InfoS("Container started",
		"PodName", types.UniquePodName(pod),
		"ContainerName", container.ContainerSpec.Name,
	)
	// TODO, update pod cpu, memory resource usage
	return nil
}

func (a *PodActor) onPodContainerStopped(msg internal.PodContainerStopped) error {
	pod := msg.Pod
	container := msg.Container
	klog.InfoS("Container stopped",
		"PodName", types.UniquePodName(pod),
		"ContainerName", container.ContainerSpec.Name,
	)

	// TODO, release cpu, memory resource stat usage
	a.handlePodContainerFailure(pod, container)
	return nil
}

func (a *PodActor) onPodContainerFailed(msg internal.PodContainerFailed) error {
	// TODO verify all container artifacts are cleanup
	// report pod container status
	pod := msg.Pod
	container := msg.Container
	klog.InfoS("Container probe failed",
		"PodName", types.UniquePodName(pod),
		"ContainerName", container.ContainerSpec.Name,
	)
	a.handlePodContainerFailure(pod, container)
	return nil
}

func (a *PodActor) handlePodContainerFailure(pod *types.FornaxPod, container *types.Container) {
	if pod.PodState == types.PodStateTerminating || pod.PodState == types.PodStateFailed {
		// Pod is being termianted, expected message
		terminated, err := a.TerminatePod(time.Duration(*pod.PodSpec.DeletionGracePeriodSeconds))
		if err != nil {
			klog.ErrorS(err, "Can not terminate pod", "podName", types.UniquePodName(pod))
		} else if terminated {
			pod.PodState = types.PodStateTerminated
			// all container terminated, cleanup pod
			a.cleanup()
		}
	} else if container.InitContainer {
		if podcontainer.ContainerExitNormal(container.ContainerStatus) {
			// init container is expected to run to end
		} else if podcontainer.ContainerExitAbnormal(container.ContainerStatus) {
			// init container failed, terminate pod
			pod.PodState = types.PodStateFailed
			a.TerminatePod(time.Duration(*a.pod.PodSpec.DeletionGracePeriodSeconds))
		}
	} else {
		pod.PodState = types.PodStateFailed
		a.TerminatePod(time.Duration(*a.pod.PodSpec.DeletionGracePeriodSeconds))
	}
}

// when container readiness check passed
func (a *PodActor) onPodContainerReady(msg internal.PodContainerReady) error {
	pod := a.pod
	container := msg.Container
	klog.InfoS("Container is ready after probe check",
		"PodName", types.UniquePodName(pod),
		"ContainerName", container.ContainerSpec.Name,
	)
	allContainerReady := true
	for _, v := range a.pod.Containers {
		allContainerReady = allContainerReady && v.State == types.ContainerStateReady && v.InitContainer == false
	}

	if allContainerReady {
		pod.PodState = types.PodStateRunning
	}
	return nil
}

func NewPodActor(supervisor message.ActorRef, pod *fornaxtypes.FornaxPod, nodeConfig *config.NodeConfiguration, dependencies *dependency.Dependencies) *PodActor {
	actor := &PodActor{
		supervisor:      supervisor,
		stop:            false,
		pod:             pod,
		innerActor:      nil,
		SessionActors:   map[string]message.ActorRef{},
		ContainerActors: map[string]*podcontainer.PodContainerActor{},
		dependencies:    dependencies,
		nodeConfig:      nodeConfig,
	}
	actor.innerActor = message.NewLocalChannelActor(string(pod.Identifier), actor.messageProcess)
	return actor
}
