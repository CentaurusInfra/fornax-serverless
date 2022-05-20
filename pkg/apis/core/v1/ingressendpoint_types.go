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

// IngressEndpoint
// +k8s:openapi-gen=true
type IngressEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IngressEndpointSpec   `json:"spec,omitempty"`
	Status IngressEndpointStatus `json:"status,omitempty"`
}

// IngressEndpointList
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type IngressEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []IngressEndpoint `json:"items"`
}

// IngressEndpointSpec defines the desired state of IngressEndpoint
type IngressEndpointSpec struct {
	// TCP/UDP
	Protocol string `json:"protocol,omitempty"`

	// is port application is accessible from ingress gateway
	IngressGWIPAddress string `json:"ingressGWIPAddress,omitempty"`

	// IngressPort is port application is accessible from ingress gateway
	IngressPort int `json:"ingressPort,omitempty"`

	// destination application instances
	// +listType=set
	Destinations []Destination `json:"destinations,omitempty"`
}

type Destination struct {
	IpAddress string `json:"ipAddress,omitempty"`
	Port      int    `json:"port,omitempty"`
}

// IngressEndpointStatus defines the observed state of IngressEndpoint
type IngressEndpointAction string

const (
	SetupIngressRule    IngressEndpointAction = "SetupIngressRule"
	TeardownIngressRule IngressEndpointAction = "TeardownIngressRule"
)

// IngressEndpointAction defines the observed state of IngressEndpoint
type IngressEndpointStatus struct {
	// Status is the status of the condition.
	// Can be InUse, Idle, Unavailable
	ServiceStatus UsageStatus `json:"serviceStatus,omitempty"`

	// history record of this endpoint
	// +patchMergeKey=updateTime
	// +patchStrategy=merge
	// +listType=set
	History []IngressEndpointHistory `json:"history,omitempty" patchStrategy:"merge" patchMergeKey:"updateTime"`
}

// APIServiceCondition describes conditions for an APIService
type IngressEndpointHistory struct {
	// action on this ingress endpoint
	Action IngressEndpointAction `json:"action,omitempty"`

	// Last time the condition transitioned from one status to another.
	UpdateTime metav1.Time `json:"updateTime,omitempty"`

	// Unique, one-word, CamelCase reason for the condition's last transition.
	Reason string `json:"reason,omitempty"`

	// Human-readable message indicating details about last transition.
	Message string `json:"message,omitempty"`
}

// UsageStatus indicates the status of a condition (InUse, Idle, Unavailable).
type UsageStatus string

const (
	InUse       UsageStatus = "InUse"
	Idle        UsageStatus = "Idle"
	Unavailable UsageStatus = "Unavailable"
)

var _ resource.Object = &IngressEndpoint{}
var _ resourcestrategy.Validater = &IngressEndpoint{}

func (in *IngressEndpoint) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *IngressEndpoint) NamespaceScoped() bool {
	return true
}

func (in *IngressEndpoint) New() runtime.Object {
	return &IngressEndpoint{}
}

func (in *IngressEndpoint) NewList() runtime.Object {
	return &IngressEndpointList{}
}

func (in *IngressEndpoint) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "core.fornax-serverless.centaurusinfra.io",
		Version:  "v1",
		Resource: "ingressendpoints",
	}
}

func (in *IngressEndpoint) IsStorageVersion() bool {
	return true
}

func (in *IngressEndpoint) Validate(ctx context.Context) field.ErrorList {
	errorList := make(field.ErrorList, 0)
	if len(in.OwnerReferences) <= 0 {
		err := field.Error{
			Type:  field.ErrorTypeRequired,
			Field: "OwnerReferences",
		}
		errorList = append(errorList, &err)
	}

	if len(in.Spec.Protocol) == 0 {
		err := field.Error{
			Type:  field.ErrorTypeRequired,
			Field: "Spec.Protocol",
		}
		errorList = append(errorList, &err)
	}

	if len(in.Spec.IngressGWIPAddress) == 0 {
		err := field.Error{
			Type:  field.ErrorTypeRequired,
			Field: "Spec.IngressGWIPAddress",
		}
		errorList = append(errorList, &err)
	}

	if in.Spec.IngressPort == 0 {
		err := field.Error{
			Type:  field.ErrorTypeRequired,
			Field: "Spec.IngressPort",
		}
		errorList = append(errorList, &err)
	}

	if len(in.Spec.Destinations) <= 0 {
		err := field.Error{
			Type:  field.ErrorTypeRequired,
			Field: "Spec.Destinations",
		}
		errorList = append(errorList, &err)
	}

	if len(errorList) > 0 {
		return errorList
	} else {
		return nil
	}
}

var _ resource.ObjectList = &IngressEndpointList{}

func (in *IngressEndpointList) GetListMeta() *metav1.ListMeta {
	return &in.ListMeta
}

func (in IngressEndpointStatus) SubResourceName() string {
	return "status"
}

// IngressEndpoint implements ObjectWithStatusSubResource interface.
var _ resource.ObjectWithStatusSubResource = &IngressEndpoint{}

func (in *IngressEndpoint) GetStatus() resource.StatusSubResource {
	return in.Status
}

// IngressEndpointStatus{} implements StatusSubResource interface.
var _ resource.StatusSubResource = &IngressEndpointStatus{}

func (in IngressEndpointStatus) CopyTo(parent resource.ObjectWithStatusSubResource) {
	parent.(*IngressEndpoint).Status = in
}
