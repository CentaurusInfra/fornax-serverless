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

// ApplicationSession
// +k8s:openapi-gen=true
type ApplicationSession struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationSessionSpec   `json:"spec,omitempty"`
	Status ApplicationSessionStatus `json:"status,omitempty"`
}

// ApplicationSessionList
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ApplicationSessionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ApplicationSession `json:"items"`
}

// ApplicationSessionSpec defines the desired state of ApplicationSession
type ApplicationSessionSpec struct {

	// SessionName, client provided idemponency token
	SessionName string `json:"sessionName,omitempty"`

	// Session data is a base64 string pass through into application instances when session started
	// +optional
	SessionData string `json:"sessionData,omitempty"`
}

// +enum
type SessionStatus string

const (
	// session is allocated to instance, at least one client join
	Starting SessionStatus = "Starting"

	// session is available to instance, no client session yet
	Available SessionStatus = "Available"

	// session is dead, no heartbeat
	Closed SessionStatus = "Closed"
)

// +enum
type SessionAction string

const (
	// client session join
	ClientJoin SessionAction = "ClientJoin"

	// client session exit
	ClientExit SessionAction = "ClientExit"
)

type SessionHistory struct {
	// client session
	ClientSession corev1.LocalObjectReference `json:"clientSession,omitempty"`

	// The last time this deployment was updated.
	Action SessionAction `json:"action,omitempty"`

	// The last time this deployment was updated.
	UpdateTime metav1.Time `json:"updateTime,omitempty"`

	// The reason for the last transition.
	Reason string `json:"reason,omitempty"`

	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
}

// ApplicationSessionStatus defines the observed state of ApplicationSession
type ApplicationSessionStatus struct {
	// Endpoint this session is using
	// +optional
	IngressEndpointReference corev1.LocalObjectReference `json:"ingressEndpointReference,omitempty"`

	// Session status, is Starting, Available or Closed.
	// +optional
	SessionStatus SessionStatus `json:"sessionStatus,omitempty"`

	// Represents the latest available observations of a deployment's current state.
	// +optional
	// +patchMergeKey=updateTime
	// +patchStrategy=merge
	// +listType=set
	History []SessionHistory `json:"history,omitempty" patchStrategy:"merge" patchMergeKey:"updateTime"`

	// +optional
	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=set
	ClientSessions []corev1.LocalObjectReference `json:"clientSessions,omitempty"  patchStrategy:"merge" patchMergeKey:"name"`
}

var _ resource.Object = &ApplicationSession{}
var _ resourcestrategy.Validater = &ApplicationSession{}

func (in *ApplicationSession) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *ApplicationSession) NamespaceScoped() bool {
	return true
}

func (in *ApplicationSession) New() runtime.Object {
	return &ApplicationSession{}
}

func (in *ApplicationSession) NewList() runtime.Object {
	return &ApplicationSessionList{}
}

func (in *ApplicationSession) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "core.fornax-serverless.centaurusinfra.io",
		Version:  "v1",
		Resource: "applicationsessions",
	}
}

func (in *ApplicationSession) IsStorageVersion() bool {
	return true
}

func (in *ApplicationSession) Validate(ctx context.Context) field.ErrorList {
	errorList := make(field.ErrorList, 0)
	if len(in.OwnerReferences) <= 0 {
		err := field.Error{
			Type:  field.ErrorTypeRequired,
			Field: "OwnerReferences",
		}
		errorList = append(errorList, &err)
	}

	if len(in.Spec.SessionName) == 0 {
		err := field.Error{
			Type:  field.ErrorTypeRequired,
			Field: "Spec.SessionName",
		}
		errorList = append(errorList, &err)
	}

	if len(errorList) > 0 {
		return errorList
	} else {
		return nil
	}
}

var _ resource.ObjectList = &ApplicationSessionList{}

func (in *ApplicationSessionList) GetListMeta() *metav1.ListMeta {
	return &in.ListMeta
}

func (in ApplicationSessionStatus) SubResourceName() string {
	return "status"
}

// ApplicationSession implements ObjectWithStatusSubResource interface.
var _ resource.ObjectWithStatusSubResource = &ApplicationSession{}

func (in *ApplicationSession) GetStatus() resource.StatusSubResource {
	return in.Status
}

// ApplicationSessionStatus{} implements StatusSubResource interface.
var _ resource.StatusSubResource = &ApplicationSessionStatus{}

func (in ApplicationSessionStatus) CopyTo(parent resource.ObjectWithStatusSubResource) {
	parent.(*ApplicationSession).Status = in
}
