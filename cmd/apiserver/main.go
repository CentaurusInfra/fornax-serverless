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
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	apiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/apiserver-runtime/pkg/builder"

	// +kubebuilder:scaffold:resource-imports

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxclient "centaurusinfra.io/fornax-serverless/pkg/client/clientset/versioned"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/application"
	grpc_server "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/server"
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

	// new fornaxcore grpc grpcServer which implement node agent proxy
	grpcServer := grpc_server.NewGrpcServer()

	// start node manager
	// nodeManager := node.NewNodeManager(node.DefaultStaleNodeTimeout)

	// start pod scheduler
	// klog.Info("starting pod scheduler")
	// scheduler := podscheduler.NewPodScheduler(grpcServer, nodeManager)
	// scheduler.Run()

	// start pod manager and node manager
	// klog.Info("starting node manager")
	// podManager := pod.NewPodManager(scheduler, grpcServer)
	// nodeManager.SetPodManager(podManager)
	// nodeManager.Run()

	// start fornaxcore grpc server to listen to nodeagent
	klog.Info("starting fornaxcore grpc server")
	port := 18001
	certFile := ""
	keyFile := ""
	err := grpcServer.RunGrpcServer(context.Background(), nil, port, certFile, keyFile)
	// err := app.RunIntegTestGrpcServer(context.Background(), port, certFile, keyFile)
	if err != nil {
		klog.Fatal(err)
	}
	klog.Info("Fornaxcore grpc server started")

	// start api server
	fmt.Println("starting fornaxcore k8s.io rest api server")
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

	err = apiserver.Execute()
	if err != nil {
		klog.Fatal(err)
		os.Exit(-1)
	}

	fmt.Println("started fornaxcore k8s.io rest api server")
	// clientcmd.BuildConfigFromFlags(masterUrl string, kubeconfigPath string)
	// start application manager at last as it require api server
	var kubeconfig *rest.Config
	if root, err := os.Getwd(); err == nil {
		kubeconfigPath := root + "/kubeconfig"
		if kubeconfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath); err != nil {
			klog.ErrorS(err, "Failed to construct kube rest config")
			os.Exit(-1)
		}
	} else {
		klog.ErrorS(err, "Failed to get working dir")
		os.Exit(-1)
	}
	apiserverClient := fornaxclient.NewForConfigOrDie(kubeconfig)
	appManager := application.NewApplicationManager(nil, nil, apiserverClient)
	appManager.Run(context.Background())
}
