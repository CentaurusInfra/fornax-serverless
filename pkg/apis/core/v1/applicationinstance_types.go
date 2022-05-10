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

// ApplicationInstance
// +k8s:openapi-gen=true
type ApplicationInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationInstanceSpec   `json:"spec,omitempty"`
	Status ApplicationInstanceStatus `json:"status,omitempty"`
}

// ApplicationInstanceList
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ApplicationInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ApplicationInstance `json:"items"`
}

// ApplicationInstanceSpec defines the desired state of ApplicationInstance
type ApplicationInstanceSpec struct {
	// InstanceName
	InstanceName string `json:"instance_name,omitempty"`

	// ApplicationName
	ApplicationName string `json:"application_name,omitempty"`

	// Instance Network interface
	// InstanceInterfaces []NetworkInterface `json:"instance_interfaces,omitempty"`
}

type NetworkInterface struct {
	// ip addresss allocated to instance, it's affinity ip until end of life of this instance, pods of a instance use this ip
	IpAddress string `json:"ip_address,omitempty"`

	// vpc id
	VPC string `json:"vpc,omitempty"`
}

type InstanceHistory struct {
	PodReference corev1.LocalObjectReference `json:"pod_reference,omitempty"`

	// Type of deployment condition.
	Action InstanceAction `json:"action,omitempty"`

	// The last time this instance was updated.
	UpdateTime metav1.Time `json:"update_time,omitempty"`

	// The reason for the last transition.
	Reason string `json:"reason,omitempty"`

	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
}

type InstanceAction string

// These are valid conditions of a deployment.
const (
	// create pod
	CreatePod InstanceAction = "CreatePod"

	// stop pod
	DeletePod InstanceAction = "DeletePod"

	// create endpoint
	CreateIngressEndpoint InstanceAction = "CreateIngressEndpoint"

	// create endpoint
	DeleteIngressEndpoint InstanceAction = "DeleteIngressEndpoint"
)

type InstanceStatus string

// These are valid conditions of a deployment.
const (
	// Running means instance at least one application session is allocated on it
	Running InstanceStatus = "Running"
	// Standby means instance is sleeping, no application session allocated on it yet
	Standby InstanceStatus = "Standby"
	// Empty means instance is running, waiting for new sessions
	Empty DeploymentStatus = "Empty"
)

// ApplicationInstanceStatus defines the observed state of ApplicationInstance
type ApplicationInstanceStatus struct {

	// Status of the condition, one of True, False, Unknown.
	Status InstanceStatus `json:"status,omitempty"`

	// instance history, including pod and endpoint history
	History []InstanceHistory `json:"history,omitempty"`
}

var _ resource.Object = &ApplicationInstance{}
var _ resourcestrategy.Validater = &ApplicationInstance{}

func (in *ApplicationInstance) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *ApplicationInstance) NamespaceScoped() bool {
	return true
}

func (in *ApplicationInstance) New() runtime.Object {
	return &ApplicationInstance{}
}

func (in *ApplicationInstance) NewList() runtime.Object {
	return &ApplicationInstanceList{}
}

func (in *ApplicationInstance) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "core.fornax-serverless.centaurusinfra.io",
		Version:  "v1",
		Resource: "applicationinstances",
	}
}

func (in *ApplicationInstance) IsStorageVersion() bool {
	return true
}

func (in *ApplicationInstance) Validate(ctx context.Context) field.ErrorList {
	errorList := make(field.ErrorList, 0)
	if len(in.OwnerReferences) <= 0 {
		err := field.Error{
			Type:  field.ErrorTypeRequired,
			Field: "OwnerReferences",
		}
		errorList = append(errorList, &err)
	}

	if len(in.Spec.ApplicationName) <= 0 {
		err := field.Error{
			Type:  field.ErrorTypeRequired,
			Field: "Spec.ApplicationName",
		}
		errorList = append(errorList, &err)
	}

	if len(in.Spec.InstanceName) <= 0 {
		err := field.Error{
			Type:  field.ErrorTypeRequired,
			Field: "Spec.InstanceName",
		}
		errorList = append(errorList, &err)
	}

	// if len(in.Spec.InstanceInterfaces) <= 0 {
	// 	err := field.Error{
	// 		Type:  field.ErrorTypeRequired,
	// 		Field: "Spec.InstanceInterfaces",
	// 	}
	// 	errorList = append(errorList, &err)
	// }

	if len(errorList) > 0 {
		return errorList
	} else {
		return nil
	}
}

var _ resource.ObjectList = &ApplicationInstanceList{}

func (in *ApplicationInstanceList) GetListMeta() *metav1.ListMeta {
	return &in.ListMeta
}

func (in ApplicationInstanceStatus) SubResourceName() string {
	return "status"
}

// ApplicationInstance implements ObjectWithStatusSubResource interface.
var _ resource.ObjectWithStatusSubResource = &ApplicationInstance{}

func (in *ApplicationInstance) GetStatus() resource.StatusSubResource {
	return in.Status
}

// ApplicationInstanceStatus{} implements StatusSubResource interface.
var _ resource.StatusSubResource = &ApplicationInstanceStatus{}

func (in ApplicationInstanceStatus) CopyTo(parent resource.ObjectWithStatusSubResource) {
	parent.(*ApplicationInstance).Status = in
}
