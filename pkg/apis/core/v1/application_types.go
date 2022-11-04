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

	// container will use grpc session service on node agent to start application session
	UsingNodeSessionService bool `json:"usingNodeSessionService,omitempty"`

	// Data contains the configuration data.
	// Each key must consist of alphanumeric characters, '-', '_' or '.'.
	// Values with non-UTF-8 base64 string of byte sequences
	// +optional
	ConfigData map[string]string `json:"configData,omitempty"`

	// application scaling policy
	ScalingPolicy ScalingPolicy `json:"scalingPolicy,omitempty"`
}

type ScalingPolicyType string

const (
	// scaling according idle session number
	ScalingPolicyTypeIdleSessionPercent ScalingPolicyType = "idle_session_percent"

	// scaling according idle session percent
	ScalingPolicyTypeIdleSessionNum ScalingPolicyType = "idle_session_number"
)

type ScalingPolicy struct {
	MinimumInstance uint32 `json:"minimumInstance,omitempty"`
	MaximumInstance uint32 `json:"maximumInstance,omitempty"`
	Burst           uint32 `json:"burst,omitempty"`

	// what session scaling policy to use, absolute num or percent
	ScalingPolicyType ScalingPolicyType `json:"scalingPolicyType,omitempty"`

	// +optional, must set if ScalingPolicyType == "idle_session_number"
	IdleSessionNumThreshold *IdelSessionNumThreshold `json:"idleSessionNumThreshold,omitempty"`

	// +optional, must set if ScalingPolicyType == "idle_session_percent"
	IdleSessionPercentThreshold *IdelSessionPercentThreshold `json:"idleSessionPercentThreshold,omitempty"`
}

// high watermark should > low watermark, if both are 0, then no auto scaling for idle buffer,
// application instance are created on demand when there is no instance to hold a comming session
type IdelSessionNumThreshold struct {
	// scaling down when idle session more than this number
	// +optional, default 0
	HighWaterMark uint32 `json:"highWaterMark,omitempty"`

	// scaling up when idle session less than this number
	// +optional, default 0
	LowWaterMark uint32 `json:"lowWaterMark,omitempty"`
}

// high watermark should > low watermark, if both are 0, then no auto scaling for idle buffer,
// application instance are created on demand when there is no instance to hold a comming session
type IdelSessionPercentThreshold struct {
	// scaling down when idle session percent more than this number
	// +optional, default 0, must less than 100
	HighWaterMark uint32 `json:"idleSessionNumThresholdHighWaterMark,omitempty"`

	// scaling up when idle session percent less than this number
	// +optional, default 0, must less than 100
	LowWaterMark uint32 `json:"idleSessionNumThresholdLowWaterMark,omitempty"`
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
	AllocatedInstances int32 `json:"allocatedInstances,omitempty"`

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

var ApplicationGrv = schema.GroupVersionResource{
	Group:    "core.fornax-serverless.centaurusinfra.io",
	Version:  "v1",
	Resource: "applications",
}

func (in *Application) GetGroupVersionResource() schema.GroupVersionResource {
	return ApplicationGrv
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

	if in.Spec.ScalingPolicy.MaximumInstance == 0 {
		err := field.Error{
			Type:   field.ErrorTypeInvalid,
			Field:  "Spec.ScalingPolicy.MaximumInstance",
			Detail: "Value should be greater than 0",
		}
		errorList = append(errorList, &err)
	}

	if in.Spec.ScalingPolicy.MaximumInstance < in.Spec.ScalingPolicy.MinimumInstance {
		err := field.Error{
			Type:   field.ErrorTypeInvalid,
			Field:  "Spec.ScalingPolicy.MaximumInstance",
			Detail: "Value should not be less than Spec.ScalingPolicy.MaximumInstance",
		}
		errorList = append(errorList, &err)
	}

	if in.Spec.ScalingPolicy.ScalingPolicyType == ScalingPolicyTypeIdleSessionNum && in.Spec.ScalingPolicy.IdleSessionNumThreshold == nil {
		err := field.Error{
			Type:   field.ErrorTypeNotFound,
			Field:  "Spec.IdleSessionNumThreshold",
			Detail: "Spec.ScalingPolicy.ScalingPolicyType is idle_session_number, but Spec.ScalingPolicy.IdleSessionNumThreshold not found",
		}
		errorList = append(errorList, &err)
	}

	if in.Spec.ScalingPolicy.ScalingPolicyType == ScalingPolicyTypeIdleSessionPercent && in.Spec.ScalingPolicy.IdleSessionPercentThreshold == nil {
		err := field.Error{
			Type:   field.ErrorTypeNotFound,
			Field:  "Spec.IdleSessionPercentThreshold",
			Detail: "Spec.ScalingPolicy.ScalingPolicyType is idle_session_percent, but Spec.ScalingPolicy.IdleSessionPercentThreshold not found",
		}
		errorList = append(errorList, &err)
	}

	if in.Spec.ScalingPolicy.IdleSessionPercentThreshold != nil &&
		in.Spec.ScalingPolicy.IdleSessionPercentThreshold.HighWaterMark < in.Spec.ScalingPolicy.IdleSessionPercentThreshold.LowWaterMark {
		err := field.Error{
			Type:   field.ErrorTypeInvalid,
			Field:  "Spec.IdleSessionPercentThreshold",
			Detail: "HighWaterMark must be greater than LowWaterMark",
		}
		errorList = append(errorList, &err)
	}

	if in.Spec.ScalingPolicy.IdleSessionPercentThreshold != nil &&
		in.Spec.ScalingPolicy.IdleSessionPercentThreshold.HighWaterMark > 100 {
		err := field.Error{
			Type:   field.ErrorTypeInvalid,
			Field:  "Spec.IdleSessionPercentThreshold",
			Detail: "HighWaterMark must be less than 100",
		}
		errorList = append(errorList, &err)
	}

	if in.Spec.ScalingPolicy.IdleSessionNumThreshold != nil &&
		in.Spec.ScalingPolicy.IdleSessionNumThreshold.HighWaterMark < in.Spec.ScalingPolicy.IdleSessionNumThreshold.LowWaterMark {
		err := field.Error{
			Type:   field.ErrorTypeInvalid,
			Field:  "Spec.IdleSessionNumThreshold",
			Detail: "HighWaterMark must be greater than LowWaterMark",
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
