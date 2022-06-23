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
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	apiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/klog"
	"sigs.k8s.io/apiserver-runtime/pkg/builder"

	// +kubebuilder:scaffold:resource-imports
	"centaurusinfra.io/fornax-serverless/cmd/apiserver/app"
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

	// start fornaxcore grpc server
	klog.Info("starting fornaxcore grpc server")
	port := 18001
	certFile := ""
	keyFile := ""
	err := app.RunGrpcServer(context.Background(), port, certFile, keyFile)
	if err != nil {
		klog.Fatal(err)
	}
	klog.Info("Fornaxcore grpc server started")

	klog.Info("starting fornaxcore k8s.io rest api server")
	// +kubebuilder:scaffold:resource-register
	apiserver := builder.APIServer.
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
		})

	// WithResource(&v1.Pod{}).
	// WithResource(&k8scorev1.FornaxNode{}).
	// WithResource(&k8scorev1.FornaxSecret{}).
	// WithResource(&k8scorev1.FornaxConfigMap{}).
	// WithResource(&k8scorev1.FornaxServiceAccount{}).
	// WithFlagFns(fns ...func(set *pflag.FlagSet) *pflag.FlagSet).
	// // Authentication is the configuration for authentication
	// Authentication AuthenticationInfo
	//
	// // Authorization is the configuration for authorization
	// Authorization AuthorizationInfo

	// LoopbackClientConfig is a config for a privileged loopback connection to the API server
	// This is required for proper functioning of the PostStartHooks on a GenericAPIServer
	// TODO: move into SecureServing(WithLoopback) as soon as insecure serving is gone
	// LoopbackClientConfig * restclient.Config

	// EgressSelector provides a lookup mechanism for dialing outbound connections.
	// It does so based on a EgressSelectorConfiguration which was read at startup.
	// EgressSelector * egressselector.EgressSelector

	// WithServerFns(func(server *builder.GenericAPIServer) *builder.GenericAPIServer {
	//  apiGroupInfo := *apiserver.APIGroupInfo{}
	//  server.InstallLegacyAPIGroup(apiserver.DefaultLegacyAPIPrefix, apiGroupInfo)
	//  return server
	// }).

	err = apiserver.Execute()
	if err != nil {
		klog.Fatal(err)
	}

}
