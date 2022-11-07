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
	"reflect"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/dependency"
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	podcontainer "centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod/container"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/session"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/sessionservice"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
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
	pod             *types.FornaxPod
	innerActor      *message.LocalChannelActor
	sessionActors   map[string]*session.SessionActor
	containerActors map[string]*podcontainer.PodContainerActor
	dependencies    *dependency.Dependencies
	nodeConfig      *config.NodeConfiguration
	lastError       error
}

func (n *PodActor) Reference() message.ActorRef {
	return n.innerActor.Reference()
}

func (n *PodActor) Stop() error {
	n.stop = true
	for _, v := range n.containerActors {
		v.Stop()
	}
	n.innerActor.Stop()
	return nil
}

func (a *PodActor) Start() {
	klog.InfoS("Pod actor started", "pod", types.UniquePodName(a.pod))
	a.innerActor.Start()

	// when pod actor is restored from nodeagent db, it already has live containers and sessions, need to recreate container actors from stored state
	a.recoverContainerAndSessionActors()

	// start house keeping loop to make sure pod reach its final state, either failed or revive from temporary runtime error
	go func() {
		ticker := time.NewTicker(houseKeepingPeriod)
		for {
			if a.stop {
				ticker.Stop()
				break
			}

			select {
			case _ = <-ticker.C:
				if a.lastError != nil {
					a.notify(a.Reference(), HouseKeeping{})
				}
			}
		}
	}()
}

// when restart a pod actor, foreach stored pod container and sessions, restart actor to monitor them,
// if container is terminated, skip it, if session is still pending, set it timeout
func (a *PodActor) recoverContainerAndSessionActors() {
	for k, cont := range a.pod.Containers {
		if cont.State != types.ContainerStateTerminated {
			klog.InfoS("Recover container actor on pod", "pod", types.UniquePodName(a.pod), "container", cont.ContainerSpec.Name, "status", cont.State)
			if _, found := a.containerActors[k]; !found {
				actor := podcontainer.NewPodContainerActor(a.Reference(), a.pod, cont, a.dependencies)
				actor.Start()
				a.containerActors[cont.ContainerSpec.Name] = actor
			}
		}
	}

	for _, sess := range a.pod.Sessions {
		if !util.SessionIsClosed(sess.Session) {
			klog.InfoS("Recover session actor on pod", "pod", types.UniquePodName(a.pod), "session", sess.Identifier, "status", sess.Session.Status)
			var sessService sessionservice.SessionService
			if util.PodHasSessionServiceAnnotation(a.pod.Pod) {
				sessService = a.dependencies.SessionService
			} else {
				sessService = sessionservice.NewNullSessionService()
			}
			actor := session.NewSessionActor(a.pod, sess, sessService, a.innerActor.Reference())
			a.sessionActors[sess.Identifier] = actor
			actor.PingSession()
		}
	}
}

func (n *PodActor) notify(receiver message.ActorRef, msg interface{}) error {
	return message.Send(n.Reference(), receiver, msg)
}

func (n *PodActor) notifyContainer(containerName string, msg interface{}) error {
	ca, found := n.containerActors[containerName]
	if !found {
		return fmt.Errorf("Container with name %s not found", containerName)
	}

	return n.notify(ca.Reference(), msg)
}

func (a *PodActor) podHandler(msg message.ActorMessage) (interface{}, error) {
	oldPodState := a.pod.FornaxPodState
	var err error
	switch msg.Body.(type) {
	case internal.PodCreate:
		err = a.create()
	case internal.PodHibernate:
		err = a.hibernate()
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
	case internal.SessionOpen:
		err = a.onSessionOpenCommand(msg.Body.(internal.SessionOpen))
	case internal.SessionClose:
		err = a.onSessionCloseCommand(msg.Body.(internal.SessionClose))
	case internal.SessionState:
		a.handleSessionState(msg.Body.(internal.SessionState))
		if a.pod.FornaxPodState == types.PodStateTerminating {
			// when pod termination was requested, recheck if pod can be finally terminated after session closed
			err = a.terminate(false)
		}
	case HouseKeeping:
		// calibarate pod error and cleanup, return if cleanup failed, do not change previous error state
		if a.lastError != nil {
			err = a.handlePodError()
		}
	default:
	}

	SetPodStatus(a.pod, nil)
	if err != nil {
		a.lastError = err
		return nil, err
	} else {
		a.lastError = nil
		if a.pod.FornaxPodState == types.PodStateTerminated {
			a.lastError = a.cleanup()
			if a.lastError == nil {
				// only notify if cleanup did not fail, no need to send uncleaned pod to fornax core, it could still running
				a.notify(a.supervisor, internal.PodStatusChange{Pod: a.pod})
			}
		} else {
			// do not notify fornax core in transit state to avoid unnecessary pod state sync
			if oldPodState != a.pod.FornaxPodState && !types.PodInTransitState(a.pod) {
				klog.InfoS("PodState changed", "pod", types.UniquePodName(a.pod), "old state", oldPodState, "new state", a.pod.FornaxPodState)
				a.notify(a.supervisor, internal.PodStatusChange{Pod: a.pod})
			}
		}

		return nil, a.lastError
	}
}

func (a *PodActor) create() error {
	pod := a.pod
	klog.InfoS("Creating pod", "pod", types.UniquePodName(pod))
	if types.PodInTerminating(pod) {
		return fmt.Errorf("Pod %s is being terminated or already terminated", types.UniquePodName(pod))
	}

	if types.PodCreated(pod) {
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
func (a *PodActor) terminate(forceTerminatePod bool) error {
	pod := a.pod
	klog.InfoS("Stopping session, container and terminating pod", "pod", types.UniquePodName(pod), "state", pod.FornaxPodState, "force", forceTerminatePod)
	if pod.Pod.DeletionTimestamp == nil {
		pod.Pod.DeletionTimestamp = util.NewCurrentMetaTime()
	}
	if pod.FornaxPodState == types.PodStateTerminated {
		return nil
	}
	pod.FornaxPodState = types.PodStateTerminating

	// if force terminate, then we will skip close session, the case is container already failed, and pod is already in terminating state
	if types.PodHasOpenSessions(pod) && !forceTerminatePod {
		klog.InfoS("Close open session before terminating pod", "pod", types.UniquePodName(pod), "#session", len(a.sessionActors))
		errs := []error{}
		for _, v := range a.sessionActors {
			err := v.CloseSession()
			if err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("failed to close sessions")
		}
	} else {
		gracefulPeriodSeconds := int64(0)
		if pod.Pod.DeletionGracePeriodSeconds != nil {
			gracefulPeriodSeconds = *pod.Pod.DeletionGracePeriodSeconds
		}
		terminated, err := a.TerminatePod(time.Duration(gracefulPeriodSeconds), forceTerminatePod)
		if err != nil {
			klog.ErrorS(err, "Pod termination failed, state is left in terminating to retry later,", "pod", types.UniquePodName(pod))
			return err
		}

		if terminated {
			pod.FornaxPodState = types.PodStateTerminated
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

func (a *PodActor) hibernate() error {
	for _, v := range a.pod.Containers {
		err := a.hibernateContainer(v)
		if err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (a *PodActor) hibernateContainer(container *types.FornaxContainer) error {
	err := a.dependencies.RuntimeService.HibernateContainer(container.ContainerStatus.RuntimeStatus.Id)
	if err != nil {
		klog.ErrorS(err, "Failed to hibernate Container", "Container", container.ContainerStatus.RuntimeStatus.Id)
		return err
	} else {
		a.pod.FornaxPodState = types.PodStateHibernated
		container.State = types.ContainerStateHibernated
	}
	return nil
}

func (a *PodActor) handlePodError() (err error) {
	pod := a.pod
	klog.InfoS("Handle Pod error", "pod", types.UniquePodName(pod), "err", a.lastError, "podState", a.pod.FornaxPodState)
	switch {
	case pod.FornaxPodState == types.PodStateTerminating:
		err = a.terminate(true)
	case pod.FornaxPodState != types.PodStateTerminated && pod.RuntimePod == nil:
		// pod create failed create a sandbox
		pod.FornaxPodState = types.PodStateTerminated
	case pod.FornaxPodState != types.PodStateTerminated && pod.RuntimePod != nil && pod.RuntimePod.Sandbox == nil:
		// pod create failed to get sandbox details
		var sandbox *criv1.PodSandbox
		sandbox, err = a.dependencies.RuntimeService.GetPodSandbox(pod.RuntimePod.Id)
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
		if pod.FornaxPodState == types.PodStateRunning || pod.FornaxPodState == types.PodStateHibernated {
			// a running pod should have at a least container
			if len(a.containerActors) == 0 {
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
	klog.InfoS("Pod Container created", "Pod", types.UniquePodName(pod), "Container", container.ContainerSpec.Name)

	if n.pod.FornaxPodState == types.PodStateTerminating || n.pod.FornaxPodState == types.PodStateTerminated {
		klog.InfoS("Pod Container created after when pod is in terminating state", "Pod", types.UniquePodName(pod), "Container", container.ContainerSpec.Name)
		n.terminateContainer(container)
	}

	// TODO, update pod cpu, memory resource usage
	return nil
}

func (a *PodActor) onPodContainerStarted(msg internal.PodContainerStarted) error {
	pod := msg.Pod
	container := msg.Container
	klog.InfoS("Pod Container started", "Pod", types.UniquePodName(pod), "Container", container.ContainerSpec.Name)
	// TODO, update pod cpu, memory resource usage
	return nil
}

func (a *PodActor) onPodContainerStopped(msg internal.PodContainerStopped) error {
	pod := msg.Pod
	container := msg.Container
	klog.InfoS("Pod Container stopped", "Pod", types.UniquePodName(pod), "Container", container.ContainerSpec.Name)

	// TODO, release cpu, memory resource stat usage
	a.handlePodContainerExit(pod, container)
	return nil
}

func (a *PodActor) onPodContainerFailed(msg internal.PodContainerFailed) error {
	pod := msg.Pod
	container := msg.Container
	klog.InfoS("Pod Container Failed", "Pod", types.UniquePodName(pod), "Container", container.ContainerSpec.Name)
	a.handlePodContainerExit(pod, container)
	return nil
}

func (a *PodActor) handlePodContainerExit(pod *types.FornaxPod, container *types.FornaxContainer) {
	actor, found := a.containerActors[container.ContainerSpec.Name]
	if found {
		actor.Stop()
		delete(a.containerActors, container.ContainerSpec.Name)
	}
	if container.InitContainer {
		if runtime.ContainerExitNormal(container.ContainerStatus) {
			// init container is expected to run to end
		} else if runtime.ContainerExitAbnormal(container.ContainerStatus) {
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
	klog.InfoS("Pod Container is ready", "Pod", types.UniquePodName(pod), "Container", container.ContainerSpec.Name)

	allContainerReady := true
	for _, v := range a.pod.Containers {
		if v.InitContainer {
			allContainerReady = allContainerReady && runtime.ContainerExit(v.ContainerStatus)
		} else {
			allContainerReady = allContainerReady && runtime.ContainerRunning(v.ContainerStatus)
		}
	}

	if allContainerReady {
		pod.FornaxPodState = types.PodStateRunning
		// hibernate pod if pod spec has hibernate annotation
		if util.PodHasHibernateAnnotation(pod.Pod) && a.nodeConfig.RuntimeHandler == runtime.QuarkRuntime {
			a.hibernateContainer(container)
		}
	}
	return nil
}

// build a session actor to start session and monitor session state
func (a *PodActor) onSessionOpenCommand(msg internal.SessionOpen) (err error) {
	klog.InfoS("Open session", "Pod", a.pod.Identifier, "session", msg.SessionId)
	if a.pod.FornaxPodState == types.PodStateHibernated {
		for _, v := range a.pod.Containers {
			if v.State == types.ContainerStateHibernated {
				err := a.dependencies.RuntimeService.WakeupContainer(v.RuntimeContainer.Id)
				if err != nil {
					// if a pod can not be wakeup, terminate it to get a new one
					return a.terminate(true)
				}
				v.State = types.ContainerStateRunning
			}
		}
		a.pod.FornaxPodState = types.PodStateRunning
	} else if a.pod.FornaxPodState != types.PodStateRunning {
		return fmt.Errorf("Pod: %s is not in running state, can not open session", msg.SessionId)
	}
	if v, found := a.pod.Sessions[msg.SessionId]; found {
		if util.SessionIsOpen(v.Session) {
			return fmt.Errorf("There is already a open session for %s", msg.SessionId)
		}
	}

	sess := &types.FornaxSession{
		Identifier:     util.Name(msg.Session),
		PodIdentifier:  a.pod.Identifier,
		Session:        msg.Session.DeepCopy(),
		ClientSessions: map[string]*types.ClientSession{},
	}
	var sessService sessionservice.SessionService
	if util.PodHasSessionServiceAnnotation(a.pod.Pod) {
		sessService = a.dependencies.SessionService
	} else {
		sessService = sessionservice.NewNullSessionService()
	}
	sactor := session.NewSessionActor(a.pod, sess, sessService, a.innerActor.Reference())
	err = sactor.OpenSession()
	if err == nil {
		a.pod.Sessions[msg.SessionId] = sess
		a.sessionActors[msg.SessionId] = sactor
	}

	if err != nil {
		klog.ErrorS(err, "Failed to open session", "session", msg.SessionId)
	}

	return err
}

// find session actor to let it terminate a session, if pod actor does not exist, return failure
func (a *PodActor) onSessionCloseCommand(msg internal.SessionClose) error {
	klog.InfoS("Close session", "Pod", a.pod.Identifier, "session", msg.SessionId)
	if sActor, found := a.sessionActors[msg.SessionId]; !found {
		return fmt.Errorf("Session does not exist, %s", msg.SessionId)
	} else {
		return sActor.CloseSession()
	}
}

// simply update application session status and copy client session
// if a session timeout, terminate pod,it could close other sessions on it
func (a *PodActor) handleSessionState(s internal.SessionState) {
	session, found := a.pod.Sessions[s.SessionId]
	if !found {
		klog.Warningf("Received session state from unknown session %s", s.SessionId)
		return
	}
	newStatus := session.Session.Status.DeepCopy()

	switch s.SessionState {
	case types.SessionStateStarting:
		newStatus.SessionStatus = fornaxv1.SessionStatusStarting
	case types.SessionStateReady:
		newStatus.SessionStatus = fornaxv1.SessionStatusAvailable
	case types.SessionStateClosed:
		newStatus.SessionStatus = fornaxv1.SessionStatusClosed
		newStatus.CloseTime = util.NewCurrentMetaTime()
	case types.SessionStateNoHeartbeat:
		newStatus.SessionStatus = fornaxv1.SessionStatusClosed
		newStatus.CloseTime = util.NewCurrentMetaTime()
	}

	// just copy client sessions
	clientSessions := []v1.LocalObjectReference{}
	for _, v := range s.ClientSessions {
		clientSessions = append(clientSessions, v1.LocalObjectReference{Name: v.Identifier})
	}
	newStatus.ClientSessions = clientSessions
	if len(newStatus.ClientSessions) > 0 {
		newStatus.SessionStatus = fornaxv1.SessionStatusInUse
	}

	if !reflect.DeepEqual(session.Session.Status, *newStatus) {
		klog.InfoS("Session status changed", "session", s.SessionId, "old status", session.Session.Status, "new status", *newStatus)
		session.Session.Status = *newStatus
		a.notify(a.supervisor, internal.SessionStatusChange{Session: session, Pod: a.pod})
	}

	if util.SessionIsClosed(session.Session) {
		delete(a.sessionActors, session.Identifier)
		if session.Session.Spec.KillInstanceWhenSessionClosed {
			a.terminate(false)
		} else if util.PodHasHibernateAnnotation(a.pod.Pod) && a.nodeConfig.RuntimeHandler == runtime.QuarkRuntime {
			// hibernate again when session is closed
			a.hibernate()
		}
	}
}

func NewPodActor(supervisor message.ActorRef, pod *types.FornaxPod, nodeConfig *config.NodeConfiguration, dependencies *dependency.Dependencies, err error) *PodActor {
	actor := &PodActor{
		supervisor:      supervisor,
		stop:            false,
		pod:             pod,
		innerActor:      nil,
		lastError:       err,
		dependencies:    dependencies,
		nodeConfig:      nodeConfig,
		sessionActors:   map[string]*session.SessionActor{},
		containerActors: map[string]*podcontainer.PodContainerActor{},
	}
	actor.innerActor = message.NewLocalChannelActor(types.UniquePodName(pod), actor.podHandler)
	return actor
}
