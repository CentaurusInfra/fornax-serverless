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

	"k8s.io/klog/v2"

	// +kubebuilder:scaffold:resource-imports

	grpc_server "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/server"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/integtest"
)

func main() {

	// new fornaxcore grpc grpcServer which implement node agent proxy
	grpcServer := grpc_server.NewGrpcServer()

	// start fornaxcore grpc server to listen to nodeagent
	klog.Info("starting fornaxcore grpc server")
	port := 18001
	certFile := ""
	keyFile := ""
	err := grpcServer.RunGrpcServer(context.Background(), integtest.NewIntegNodeMonitor(), port, certFile, keyFile)
	if err != nil {
		klog.Fatal(err)
	}
	klog.Info("Fornaxcore grpc server started")
	<-context.Background().Done()

	// // start api server
	// storageFunc := func(scheme *runtime.Scheme, store *registry.Store, option *generic.StoreOptions) {
	// 	store.AfterCreate = func(obj runtime.Object, options *metav1.CreateOptions) {
	// 		fmt.Println(obj)
	// 	}
	// 	store.AfterUpdate = func(obj runtime.Object, options *metav1.UpdateOptions) {
	// 		fmt.Println(obj)
	// 	}
	// 	store.AfterDelete = func(obj runtime.Object, options *metav1.DeleteOptions) {
	// 		fmt.Println(obj)
	// 	}
	// }
	// klog.Info("starting fornaxcore k8s.io rest api server")
	// // +kubebuilder:scaffold:resource-register
	// apiserver := builder.APIServer.
	// 	WithLocalDebugExtension().
	// 	WithResourceAndStorage(&fornaxv1.Application{}, storageFunc).
	// 	WithResourceAndStorage(&fornaxv1.ApplicationInstance{}, storageFunc).
	// 	WithResourceAndStorage(&fornaxv1.ApplicationSession{}, storageFunc).
	// 	WithResourceAndStorage(&fornaxv1.ClientSession{}, storageFunc).
	// 	WithResourceAndStorage(&fornaxv1.IngressEndpoint{}, storageFunc).
	// 	WithConfigFns(func(config *apiserver.RecommendedConfig) *apiserver.RecommendedConfig {
	// 		fmt.Printf("%T\n", config.Authentication.Authenticator)
	// 		fmt.Println(config.Authentication.APIAudiences)
	// 		return config
	// 	})
	//
	// err = apiserver.Execute()
	// if err != nil {
	// 	klog.Fatal(err)
	// 	os.Exit(-1)
	// }
}
