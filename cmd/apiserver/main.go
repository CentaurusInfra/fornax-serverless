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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	apiserver "k8s.io/apiserver/pkg/server"
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
	storageFunc := func(scheme *runtime.Scheme, store *registry.Store, option *generic.StoreOptions) {
		store.AfterCreate = func(obj runtime.Object, options *metav1.CreateOptions) {
			fmt.Println(obj)
		}
		store.AfterUpdate = func(obj runtime.Object, options *metav1.UpdateOptions) {
			fmt.Println(obj)
		}
		store.AfterDelete = func(obj runtime.Object, options *metav1.DeleteOptions) {
			fmt.Println(obj)
		}
	}

	err := builder.APIServer.
		WithLocalDebugExtension().
		WithResourceAndStorage(&fornaxv1.Application{}, storageFunc).
		WithResourceAndStorage(&fornaxv1.ApplicationInstance{}, storageFunc).
		WithResourceAndStorage(&fornaxv1.ApplicationSession{}, storageFunc).
		WithResourceAndStorage(&fornaxv1.ClientSession{}, storageFunc).
		WithResourceAndStorage(&fornaxv1.IngressEndpoint{}, storageFunc).
		WithConfigFns(func(config *apiserver.RecommendedConfig) *apiserver.RecommendedConfig {
			fmt.Printf("%T\n", config.Authentication.Authenticator)
			fmt.Println(config.Authentication.APIAudiences)
			return config
		}).
		Execute()
	if err != nil {
		klog.Fatal(err)
	}
}
