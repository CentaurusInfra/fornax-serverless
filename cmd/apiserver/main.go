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

package main

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	"sigs.k8s.io/apiserver-runtime/pkg/builder"

	// +kubebuilder:scaffold:resource-imports
	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	fornaxv1.AddToScheme(scheme)
}

func main() {
	err := builder.APIServer.
		WithLocalDebugExtension().
		// +kubebuilder:scaffold:resource-register
		WithResource(&fornaxv1.Application{}).
		WithResource(&fornaxv1.ApplicationInstance{}).
		WithResource(&fornaxv1.ApplicationSession{}).
		WithResource(&fornaxv1.ClientSession{}).
		WithResource(&fornaxv1.IngressEndpoint{}).
		Execute()
	if err != nil {
		klog.Fatal(err)
	}
}
