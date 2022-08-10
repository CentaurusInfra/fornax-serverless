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

package v1

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource/resourcestrategy"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Application
// +k8s:openapi-gen=true
type Application struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationSpec   `json:"spec,omitempty"`
	Status ApplicationStatus `json:"status,omitempty"`
}

// ApplicationList
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Application `json:"items"`
}

// ApplicationSpec defines the desired state of Application
type ApplicationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// runtime image and resource requirement of a application container
	Containers []corev1.Container `json:"containers,omitempty"`

	// Data contains the configuration data.
	// Each key must consist of alphanumeric characters, '-', '_' or '.'.
	// Values with non-UTF-8 base64 string of byte sequences
	// +optional
	ConfigData map[string]string `json:"configData,omitempty"`

	// The application working mode, control how ingress port is created
	// +optional
	WorkingMode WorkingMode `json:"workingMode,omitempty"`

	// application session config
	// +optional
	SessionConfig SessionConfig `json:"sessionConfig,omitempty"`

	// application scaling policy
	// +optional
	ScalingPolicy ScalingPolicy `json:"scalingPolicy,omitempty"`
}

// +enum
type WorkingMode string

const (
	// instances of application exposed independently
	Standlone WorkingMode = "Standlone"

	// instances of application exposed as a service
	Service WorkingMode = "Service"
)

// Spec to control the application ingress endpoints
type SessionConfig struct {
	// The minimum number of application instances that must keep running
	// +optional, default 0, MinSessions should be times of NumOfSessionOfInstance
	MinSessions int `json:"minSessions,omitempty"`

	// how many sessions can a application instance hold
	// +optional, MaxSessions should be times of NumOfSessionOfInstance
	MaxSessions int `json:"maxSessions,omitempty"`

	// The maximum number of application session that can be scheduled above the desired number
	// +optional, default 1
	MaxSurge int `json:"maxSurge,omitempty"`

	// scaling when idle session less than this number
	// +optional, default 0
	MinOfIdleSessions int `json:"minOfIdleSessions,omitempty"`

	// how many sessions can a application instance hold
	NumOfSessionOfInstance int `json:"numOfSessionOfInstance,omitempty"`

	ScalingPolicyType ScalingPolicyType `json:"scalingPolicyType,omitempty"`

	IdleSessionNumThreshold uint16 `json:"idleSessionNumThreshold,omitempty"`

	IdleSessionPercentThreshold uint16 `json:"idleSessionPercentThreshold,omitempty"`
}

type DeploymentAction string

// These are valid conditions of a deployment.
const (
	// create instance
	DeploymentActionCreateInstance DeploymentAction = "CreateInstance"

	// delete instance
	DeploymentActionDeleteInstance DeploymentAction = "DeleteInstance"
)

type DeploymentStatus string

// These are valid conditions of a deployment.
const (
	// Success means the deployment finished, desired num of pods are scheduled
	DeploymentStatusSuccess DeploymentStatus = "Success"

	// part of desired num of pods scheduled, have not reach target
	DeploymentStatusPartialSuccess DeploymentStatus = "PartialSuccess"

	// Failure is scaling failed
	DeploymentStatusFailure DeploymentStatus = "Failure"
)

type DeploymentHistory struct {
	// Type of deployment condition.
	Action DeploymentAction `json:"action,omitempty"`

	// The last time this deployment was updated.
	UpdateTime metav1.Time `json:"updateTime,omitempty"`

	// The reason for the last transition.
	Reason string `json:"reason,omitempty"`

	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
}

// ApplicationStatus defines the observed state of Application
type ApplicationStatus struct {
	// lifecycle state of application
	// +optional
	State ApplicationState `json:"state,omitempty"`

	// Total number of non-terminated pods targeted
	DesiredInstances int32 `json:"desiredInstances,omitempty"`

	// Total number of available instances, including pod not scheduled yet
	// +optional
	TotalInstances int32 `json:"totalInstances,omitempty"`

	// Total number of instances pending schedule and implement
	// +optional
	PendingInstances int32 `json:"pendingInstances,omitempty"`

	// Total number of instances pending delete and cleanup
	// +optional
	DeletingInstances int32 `json:"deletingInstances,omitempty"`

	// Total number of instances which have been started by node
	// +optional
	ReadyInstances int32 `json:"readyInstances,omitempty"`

	// Total number of pods which do not have session on it
	// +optional
	IdleInstances int32 `json:"idleInstances,omitempty"`

	// DeploymentStatus of Last History
	// +optional
	DeploymentStatus DeploymentStatus `json:"deploymentStatus,omitempty"`

	// The first time this app was deployed.
	DeploymentTime metav1.Time `json:"deploymentTime,omitempty"`

	// Represents the latest available observations of a deployment's current state.
	// +optional
	// +patchMergeKey=updateTime
	// +patchStrategy=merge
	// +listType=set
	History []DeploymentHistory `json:"history,omitempty" patchStrategy:"merge" patchMergeKey:"updateTime"`
}

type ApplicationState string

const (
	AppTerminating ApplicationState = "terminating"
)

type ScalingPolicyType string

const (
	ScalingPolicyTypeIdleSessionPercent ScalingPolicyType = "idle_session_percent"
	ScalingPolicyTypeIdleSessionNum     ScalingPolicyType = "idle_session_number"
)

type ScalingPolicy struct {
	MinimumTarget uint32 `json:"minimumTarget,omitempty"`
	MaximumTarget uint32 `json:"maximumTarget,omitempty"`
	Burst         uint16 `json:"burst,omitempty"`
}

var _ resource.Object = &Application{}
var _ resourcestrategy.Validater = &Application{}

func (in *Application) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *Application) NamespaceScoped() bool {
	return true
}

func (in *Application) New() runtime.Object {
	return &Application{}
}

func (in *Application) NewList() runtime.Object {
	return &ApplicationList{}
}

func (in *Application) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "core.fornax-serverless.centaurusinfra.io",
		Version:  "v1",
		Resource: "applications",
	}
}

func (in *Application) IsStorageVersion() bool {
	return true
}

func (in *Application) Validate(ctx context.Context) field.ErrorList {
	errorList := make(field.ErrorList, 0)

	if len(in.Spec.Containers) == 0 {
		err := field.Error{
			Type:  field.ErrorTypeRequired,
			Field: "Spec.Containers",
		}
		errorList = append(errorList, &err)
	}

	if len(errorList) > 0 {
		return errorList
	} else {
		return nil
	}
}

var _ resource.ObjectList = &ApplicationList{}

func (in *ApplicationList) GetListMeta() *metav1.ListMeta {
	return &in.ListMeta
}

func (in ApplicationStatus) SubResourceName() string {
	return "status"
}

// Application implements ObjectWithStatusSubResource interface.
var _ resource.ObjectWithStatusSubResource = &Application{}

func (in *Application) GetStatus() resource.StatusSubResource {
	return in.Status
}

// ApplicationStatus{} implements StatusSubResource interface.
var _ resource.StatusSubResource = &ApplicationStatus{}

func (in ApplicationStatus) CopyTo(parent resource.ObjectWithStatusSubResource) {
	parent.(*Application).Status = in
}

var _ resource.ObjectWithArbitrarySubResource = &Application{}

func (in *Application) GetArbitrarySubResources() []resource.ArbitrarySubResource {
	return []resource.ArbitrarySubResource{
		//    // +kubebuilder:scaffold:subresource
		//    &ApplicationSession{},
	}
}
