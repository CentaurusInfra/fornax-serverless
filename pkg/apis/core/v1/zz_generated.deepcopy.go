//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright 2022 The fornax-serverless Authors.

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

// Code generated by controller-gen. DO NOT EDIT.

package v1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AccessEndPoint) DeepCopyInto(out *AccessEndPoint) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AccessEndPoint.
func (in *AccessEndPoint) DeepCopy() *AccessEndPoint {
	if in == nil {
		return nil
	}
	out := new(AccessEndPoint)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Application) DeepCopyInto(out *Application) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Application.
func (in *Application) DeepCopy() *Application {
	if in == nil {
		return nil
	}
	out := new(Application)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Application) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationInstance) DeepCopyInto(out *ApplicationInstance) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationInstance.
func (in *ApplicationInstance) DeepCopy() *ApplicationInstance {
	if in == nil {
		return nil
	}
	out := new(ApplicationInstance)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ApplicationInstance) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationInstanceList) DeepCopyInto(out *ApplicationInstanceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ApplicationInstance, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationInstanceList.
func (in *ApplicationInstanceList) DeepCopy() *ApplicationInstanceList {
	if in == nil {
		return nil
	}
	out := new(ApplicationInstanceList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ApplicationInstanceList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationInstanceSpec) DeepCopyInto(out *ApplicationInstanceSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationInstanceSpec.
func (in *ApplicationInstanceSpec) DeepCopy() *ApplicationInstanceSpec {
	if in == nil {
		return nil
	}
	out := new(ApplicationInstanceSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationInstanceStatus) DeepCopyInto(out *ApplicationInstanceStatus) {
	*out = *in
	if in.History != nil {
		in, out := &in.History, &out.History
		*out = make([]InstanceHistory, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationInstanceStatus.
func (in *ApplicationInstanceStatus) DeepCopy() *ApplicationInstanceStatus {
	if in == nil {
		return nil
	}
	out := new(ApplicationInstanceStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationList) DeepCopyInto(out *ApplicationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Application, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationList.
func (in *ApplicationList) DeepCopy() *ApplicationList {
	if in == nil {
		return nil
	}
	out := new(ApplicationList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ApplicationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationSession) DeepCopyInto(out *ApplicationSession) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationSession.
func (in *ApplicationSession) DeepCopy() *ApplicationSession {
	if in == nil {
		return nil
	}
	out := new(ApplicationSession)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ApplicationSession) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationSessionList) DeepCopyInto(out *ApplicationSessionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ApplicationSession, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationSessionList.
func (in *ApplicationSessionList) DeepCopy() *ApplicationSessionList {
	if in == nil {
		return nil
	}
	out := new(ApplicationSessionList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ApplicationSessionList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationSessionSpec) DeepCopyInto(out *ApplicationSessionSpec) {
	*out = *in
	if in.CloseGracePeriodSeconds != nil {
		in, out := &in.CloseGracePeriodSeconds, &out.CloseGracePeriodSeconds
		*out = new(uint16)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationSessionSpec.
func (in *ApplicationSessionSpec) DeepCopy() *ApplicationSessionSpec {
	if in == nil {
		return nil
	}
	out := new(ApplicationSessionSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationSessionStatus) DeepCopyInto(out *ApplicationSessionStatus) {
	*out = *in
	if in.AccessEndPoints != nil {
		in, out := &in.AccessEndPoints, &out.AccessEndPoints
		*out = make([]AccessEndPoint, len(*in))
		copy(*out, *in)
	}
	if in.ClientSessions != nil {
		in, out := &in.ClientSessions, &out.ClientSessions
		*out = make([]corev1.LocalObjectReference, len(*in))
		copy(*out, *in)
	}
	if in.AvailableTime != nil {
		in, out := &in.AvailableTime, &out.AvailableTime
		*out = (*in).DeepCopy()
	}
	if in.CloseTime != nil {
		in, out := &in.CloseTime, &out.CloseTime
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationSessionStatus.
func (in *ApplicationSessionStatus) DeepCopy() *ApplicationSessionStatus {
	if in == nil {
		return nil
	}
	out := new(ApplicationSessionStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationSpec) DeepCopyInto(out *ApplicationSpec) {
	*out = *in
	if in.Containers != nil {
		in, out := &in.Containers, &out.Containers
		*out = make([]corev1.Container, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ConfigData != nil {
		in, out := &in.ConfigData, &out.ConfigData
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	in.ScalingPolicy.DeepCopyInto(&out.ScalingPolicy)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationSpec.
func (in *ApplicationSpec) DeepCopy() *ApplicationSpec {
	if in == nil {
		return nil
	}
	out := new(ApplicationSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationStatus) DeepCopyInto(out *ApplicationStatus) {
	*out = *in
	in.LatestHistory.DeepCopyInto(&out.LatestHistory)
	if in.History != nil {
		in, out := &in.History, &out.History
		*out = make([]DeploymentHistory, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationStatus.
func (in *ApplicationStatus) DeepCopy() *ApplicationStatus {
	if in == nil {
		return nil
	}
	out := new(ApplicationStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClientSession) DeepCopyInto(out *ClientSession) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClientSession.
func (in *ClientSession) DeepCopy() *ClientSession {
	if in == nil {
		return nil
	}
	out := new(ClientSession)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClientSession) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClientSessionList) DeepCopyInto(out *ClientSessionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ClientSession, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClientSessionList.
func (in *ClientSessionList) DeepCopy() *ClientSessionList {
	if in == nil {
		return nil
	}
	out := new(ClientSessionList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClientSessionList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClientSessionSpec) DeepCopyInto(out *ClientSessionSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClientSessionSpec.
func (in *ClientSessionSpec) DeepCopy() *ClientSessionSpec {
	if in == nil {
		return nil
	}
	out := new(ClientSessionSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClientSessionStatus) DeepCopyInto(out *ClientSessionStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClientSessionStatus.
func (in *ClientSessionStatus) DeepCopy() *ClientSessionStatus {
	if in == nil {
		return nil
	}
	out := new(ClientSessionStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DeploymentHistory) DeepCopyInto(out *DeploymentHistory) {
	*out = *in
	in.UpdateTime.DeepCopyInto(&out.UpdateTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DeploymentHistory.
func (in *DeploymentHistory) DeepCopy() *DeploymentHistory {
	if in == nil {
		return nil
	}
	out := new(DeploymentHistory)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Destination) DeepCopyInto(out *Destination) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Destination.
func (in *Destination) DeepCopy() *Destination {
	if in == nil {
		return nil
	}
	out := new(Destination)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IdelSessionNumThreshold) DeepCopyInto(out *IdelSessionNumThreshold) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IdelSessionNumThreshold.
func (in *IdelSessionNumThreshold) DeepCopy() *IdelSessionNumThreshold {
	if in == nil {
		return nil
	}
	out := new(IdelSessionNumThreshold)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IdelSessionPercentThreshold) DeepCopyInto(out *IdelSessionPercentThreshold) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IdelSessionPercentThreshold.
func (in *IdelSessionPercentThreshold) DeepCopy() *IdelSessionPercentThreshold {
	if in == nil {
		return nil
	}
	out := new(IdelSessionPercentThreshold)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IngressEndpoint) DeepCopyInto(out *IngressEndpoint) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IngressEndpoint.
func (in *IngressEndpoint) DeepCopy() *IngressEndpoint {
	if in == nil {
		return nil
	}
	out := new(IngressEndpoint)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *IngressEndpoint) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IngressEndpointHistory) DeepCopyInto(out *IngressEndpointHistory) {
	*out = *in
	in.UpdateTime.DeepCopyInto(&out.UpdateTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IngressEndpointHistory.
func (in *IngressEndpointHistory) DeepCopy() *IngressEndpointHistory {
	if in == nil {
		return nil
	}
	out := new(IngressEndpointHistory)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IngressEndpointList) DeepCopyInto(out *IngressEndpointList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]IngressEndpoint, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IngressEndpointList.
func (in *IngressEndpointList) DeepCopy() *IngressEndpointList {
	if in == nil {
		return nil
	}
	out := new(IngressEndpointList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *IngressEndpointList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IngressEndpointSpec) DeepCopyInto(out *IngressEndpointSpec) {
	*out = *in
	if in.Destinations != nil {
		in, out := &in.Destinations, &out.Destinations
		*out = make([]Destination, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IngressEndpointSpec.
func (in *IngressEndpointSpec) DeepCopy() *IngressEndpointSpec {
	if in == nil {
		return nil
	}
	out := new(IngressEndpointSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IngressEndpointStatus) DeepCopyInto(out *IngressEndpointStatus) {
	*out = *in
	if in.History != nil {
		in, out := &in.History, &out.History
		*out = make([]IngressEndpointHistory, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IngressEndpointStatus.
func (in *IngressEndpointStatus) DeepCopy() *IngressEndpointStatus {
	if in == nil {
		return nil
	}
	out := new(IngressEndpointStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InstanceHistory) DeepCopyInto(out *InstanceHistory) {
	*out = *in
	out.PodReference = in.PodReference
	in.UpdateTime.DeepCopyInto(&out.UpdateTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InstanceHistory.
func (in *InstanceHistory) DeepCopy() *InstanceHistory {
	if in == nil {
		return nil
	}
	out := new(InstanceHistory)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetworkInterface) DeepCopyInto(out *NetworkInterface) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetworkInterface.
func (in *NetworkInterface) DeepCopy() *NetworkInterface {
	if in == nil {
		return nil
	}
	out := new(NetworkInterface)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ScalingPolicy) DeepCopyInto(out *ScalingPolicy) {
	*out = *in
	if in.IdleSessionNumThreshold != nil {
		in, out := &in.IdleSessionNumThreshold, &out.IdleSessionNumThreshold
		*out = new(IdelSessionNumThreshold)
		**out = **in
	}
	if in.IdleSessionPercentThreshold != nil {
		in, out := &in.IdleSessionPercentThreshold, &out.IdleSessionPercentThreshold
		*out = new(IdelSessionPercentThreshold)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ScalingPolicy.
func (in *ScalingPolicy) DeepCopy() *ScalingPolicy {
	if in == nil {
		return nil
	}
	out := new(ScalingPolicy)
	in.DeepCopyInto(out)
	return out
}
