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

package application

import (
	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	v1 "k8s.io/api/core/v1"
)

type ApplicationAutoScaler interface {
	CalcDesiredPods(application *fornaxv1.Application, activePods []*v1.Pod) int
	DeletePod(application *fornaxv1.Application, pod *v1.Pod) int
	DeterminePodsToDelete(application *fornaxv1.Application, pods []*v1.Pod) []*v1.Pod
}

var _ ApplicationAutoScaler = &applicationAutoScaler{}

type applicationAutoScaler struct {
}

// CalcDesiredPods implements ApplicationAutoScaler
func (*applicationAutoScaler) CalcDesiredPods(application *fornaxv1.Application, activePods []*v1.Pod) int {
	panic("unimplemented")
}

// DeletePod implements ApplicationAutoScaler
func (*applicationAutoScaler) DeletePod(application *fornaxv1.Application, pod *v1.Pod) int {
	panic("unimplemented")
}

// DeterminePodsToDelete implements ApplicationAutoScaler
func (*applicationAutoScaler) DeterminePodsToDelete(application *fornaxv1.Application, pods []*v1.Pod) []*v1.Pod {
	panic("unimplemented")
}

func NewApplicationAutoScaler() *applicationAutoScaler {
	return nil
}
