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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource/resourcestrategy"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClientSession
// +k8s:openapi-gen=true
type ClientSession struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClientSessionSpec   `json:"spec,omitempty"`
	Status ClientSessionStatus `json:"status,omitempty"`
}

// ClientSessionList
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClientSessionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClientSession `json:"items"`
}

// ClientSessionSpec defines the desired state of ClientSession
type ClientSessionSpec struct {
	ClientData string `json:"clientData,omitempty"`
}

var _ resource.Object = &ClientSession{}
var _ resourcestrategy.Validater = &ClientSession{}

func (in *ClientSession) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *ClientSession) NamespaceScoped() bool {
	return true
}

func (in *ClientSession) New() runtime.Object {
	return &ClientSession{}
}

func (in *ClientSession) NewList() runtime.Object {
	return &ClientSessionList{}
}

func (in *ClientSession) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "core.fornax-serverless.centaurusinfra.io",
		Version:  "v1",
		Resource: "clientsessions",
	}
}

func (in *ClientSession) IsStorageVersion() bool {
	return true
}

func (in *ClientSession) Validate(ctx context.Context) field.ErrorList {
	// TODO(user): Modify it, adding your API validation here.
	return nil
}

var _ resource.ObjectList = &ClientSessionList{}

func (in *ClientSessionList) GetListMeta() *metav1.ListMeta {
	return &in.ListMeta
}

// +enum
type ClientSessionCondition string

const (
	// session is allocated
	Active ClientSessionCondition = "active"

	// session is available for use
	Expired ClientSessionCondition = "expired"
)

// ClientSessionStatus defines the observed state of ClientSession
type ClientSessionStatus struct {
	SessionCondition ClientSessionCondition `json:"sessionCondition,omitempty"`
}

func (in ClientSessionStatus) SubResourceName() string {
	return "status"
}

// ClientSession implements ObjectWithStatusSubResource interface.
var _ resource.ObjectWithStatusSubResource = &ClientSession{}

func (in *ClientSession) GetStatus() resource.StatusSubResource {
	return in.Status
}

// ClientSessionStatus{} implements StatusSubResource interface.
var _ resource.StatusSubResource = &ClientSessionStatus{}

func (in ClientSessionStatus) CopyTo(parent resource.ObjectWithStatusSubResource) {
	parent.(*ClientSession).Status = in
}
