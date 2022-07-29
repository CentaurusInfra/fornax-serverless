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

package util

import (
	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	DefaultApplicationPodBurst = 2
)

func ApplicationKey(app *fornaxv1.Application) (string, error) {
	// TODO, maybe validate application namespace
	return cache.MetaNamespaceKeyFunc(app)
}

func ApplicationBurst(app *fornaxv1.Application) int {
	if app.Spec.ScalingPolicy.Burst == 0 {
		return DefaultApplicationPodBurst
	}
	return int(app.Spec.ScalingPolicy.Burst)
}

func UniqueApplicationName(application *fornaxv1.Application) string {
	a, _ := ApplicationKey(application)
	return a
}
