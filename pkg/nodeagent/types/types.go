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

package types

import (
	"fmt"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	v1 "k8s.io/api/core/v1"
)

// +enum
type PodState string

const (
	PodStateCreating PodState = "Creating"
	// PosStateCreated is a preserved state for standby pod use
	PodStateCreated PodState = "Created"
	// PosStateRunning is a state when startup and readiness probe passed
	PodStateRunning PodState = "Running"
	// PosStateEvacuating is a state preserved for use to evacuate session
	PodStateEvacuating PodState = "Evacuating"
	// PosStateTerminating is a state when fornax core require to terminate
	PodStateTerminating PodState = "Terminating"
	// PosStateTerminated is a normal pod exit status when fornax core request to terminate
	PodStateTerminated PodState = "Terminated"
	// PosStateFailed is a abnormal pod exit status when pod met unexpected condtion
	PodStateFailed PodState = "Failed"
)

type ContainerState string

const (
	ContainerStateCreating    ContainerState = "Creating"
	ContainerStateCreated     ContainerState = "Created"
	ContainerStateStopping    ContainerState = "Stopping"
	ContainerStateStopped     ContainerState = "Stopped"
	ContainerStateTerminated  ContainerState = "Terminated"
	ContainerStateTerminating ContainerState = "Terminating"
	ContainerStateStandby     ContainerState = "Standby"
	ContainerStateReady       ContainerState = "Ready"
	ContainerStateStarted     ContainerState = "Started"
)

type FornaxContainer struct {
	State            ContainerState           `json:"state,omitempty"`
	InitContainer    bool                     `json:"initContainer,omitempty"`
	ContainerSpec    *v1.Container            `json:"containerSpec,omitempty"`
	RuntimeContainer *runtime.Container       `json:"runtimeContainer,omitempty"`
	ContainerStatus  *runtime.ContainerStatus `json:"containerStatus,omitempty"`
}

type FornaxNodeWithRevision struct {
	Identifier string   `json:"identifier,omitempty"`
	Node       *v1.Node `json:"node,omitempty"`
	Revision   int64    `json:"revision,omitempty"`
}

type FornaxPod struct {
	Identifier              string                      `json:"identifier,omitempty"`
	FornaxPodState          PodState                    `json:"fornaxPodState,omitempty"`
	Daemon                  bool                        `json:"daemon,omitempty"`
	Pod                     *v1.Pod                     `json:"pod,omitempty"`
	ConfigMap               *v1.ConfigMap               `json:"configMap,omitempty"`
	RuntimePod              *runtime.Pod                `json:"runtimePod,omitempty"`
	Containers              map[string]*FornaxContainer `json:"containers"`
	Sessions                map[string]*FornaxSession   `json:"sessions"`
	LastStateTransitionTime time.Time                   `json:"lastStateTransitionTime,omitempty"`
}

// +enum
type SessionState string

const (
	SessionStateStarting    SessionState = "Starting"
	SessionStateReady       SessionState = "Ready"
	SessionStateClosed      SessionState = "Closed"
	SessionStateClosing     SessionState = "Closing"
	SessionStateNoHeartbeat SessionState = "NoHeartbeat"
)

type ClientSession struct {
	Identifier  string
	SessionData map[string]string
}

type FornaxSession struct {
	Identifier     string                       `json:"identifier,omitempty"`
	PodIdentifier  string                       `json:"podIdentifier,omitempty"`
	Session        *fornaxv1.ApplicationSession `json:"session,omitempty"`
	ClientSessions map[string]*ClientSession    `json:"clientSessions,omitempty"`
}

func UniquePodName(pod *FornaxPod) string {
	return fmt.Sprintf("Namespace:%s,Name:%s,UID:%s", pod.Pod.Namespace, pod.Pod.Name, pod.Pod.UID)
}

func PodIsNotStandBy(pod *FornaxPod) bool {
	//TODO if not standby pod
	return true
}

func PodHasOpenSessions(pod *FornaxPod) bool {
	for _, v := range pod.Sessions {
		if v.Session.Status.SessionStatus != fornaxv1.SessionStatusClosed || len(v.ClientSessions) > 0 {
			return true
		}
	}
	return false
}

func PodInTerminating(fppod *FornaxPod) bool {
	return len(fppod.FornaxPodState) != 0 && (fppod.FornaxPodState == PodStateTerminating || fppod.FornaxPodState == PodStateTerminated)
}

func PodCreated(fppod *FornaxPod) bool {
	return (len(fppod.FornaxPodState) != 0 && fppod.FornaxPodState != PodStateCreating) || fppod.RuntimePod != nil
}

func PodInTransitState(fppod *FornaxPod) bool {
	return len(fppod.FornaxPodState) == 0 || fppod.FornaxPodState == PodStateCreating || fppod.FornaxPodState == PodStateEvacuating || fppod.FornaxPodState == PodStateTerminating || fppod.FornaxPodState == PodStateCreated
}
