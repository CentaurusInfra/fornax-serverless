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
	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime/cri"
	v1 "k8s.io/api/core/v1"
)

// +enum
type PodState string

const (
	PodStateInitialiazing PodState = "Creating"
	PodStateCreated       PodState = "Created"
	PodStateTerminating   PodState = "Terminating"
	PodStateTerminated    PodState = "Terminated"
	PodStateCleaningup    PodState = "Cleaningup"
	PodStateStandby       PodState = "Standby"
	PodStateRunning       PodState = "Running"
	PodStateEvacuating    PodState = "Evacuating"
)

type Container struct {
	Identifier      string              `json:"identifier,omitempty"`
	Container       v1.Container        `json:"container,omitempty"`
	RuntimeConainer cri.ContainerStatus `json:"runtimeConainer,omitempty"`
}
type Pod struct {
	Identifier       string               `json:"identifier,omitempty"`
	Application      fornaxv1.Application `json:"application,omitempty"`
	PodState         string               `json:"podState,omitempty"`
	Pod              v1.Pod               `json:"pod,omitempty"`
	ConfigMap        v1.ConfigMap         `json:"configMap,omitempty"`
	RuntimePod       cri.Pod              `json:"runtimePod,omitempty"`
	RuntimePodStatus cri.PodStatus        `json:"runtimePodStatus,omitempty"`
}
