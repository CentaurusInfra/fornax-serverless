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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var SchemeGroupVersion = schema.GroupVersion{Group: "core.fornax-serverless.centaurusinfra.io", Version: "v1"}

// Resource takes an unqualified resource and returns a Group qualified GroupResource
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

var AddToScheme = func(scheme *runtime.Scheme) error {
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{
		Group:   "core.fornax-serverless.centaurusinfra.io",
		Version: "v1",
	})
	// +kubebuilder:scaffold:install

	scheme.AddKnownTypes(schema.GroupVersion{
		Group:   "core.fornax-serverless.centaurusinfra.io",
		Version: "v1",
	}, &Application{}, &ApplicationList{})

	scheme.AddKnownTypes(schema.GroupVersion{
		Group:   "core.fornax-serverless.centaurusinfra.io",
		Version: "v1",
	}, &ClientSession{}, &ClientSessionList{})

	scheme.AddKnownTypes(schema.GroupVersion{
		Group:   "core.fornax-serverless.centaurusinfra.io",
		Version: "v1",
	}, &ApplicationSession{}, &ApplicationSessionList{})

	scheme.AddKnownTypes(schema.GroupVersion{
		Group:   "core.fornax-serverless.centaurusinfra.io",
		Version: "v1",
	}, &ApplicationInstance{}, &ApplicationInstanceList{})

	scheme.AddKnownTypes(schema.GroupVersion{
		Group:   "core.fornax-serverless.centaurusinfra.io",
		Version: "v1",
	}, &IngressEndpoint{}, &IngressEndpointList{})

	scheme.AddKnownTypes(schema.GroupVersion{
		Group:   "core.fornax-serverless.centaurusinfra.io",
		Version: "v1",
	}, &Application{}, &ApplicationList{})
	scheme.AddKnownTypes(schema.GroupVersion{
		Group:   "core.fornax-serverless.centaurusinfra.io",
		Version: "v1",
	}, &ApplicationInstance{}, &ApplicationInstanceList{})
	return nil
}
