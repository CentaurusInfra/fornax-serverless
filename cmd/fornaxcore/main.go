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
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/klog/v2"
	"sigs.k8s.io/apiserver-runtime/pkg/builder"

	// +kubebuilder:scaffold:resource-imports

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/application"
	grpc_server "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/server"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/node"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/nodemonitor"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/podscheduler"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/session"
	"centaurusinfra.io/fornax-serverless/pkg/store"
	"centaurusinfra.io/fornax-serverless/pkg/store/backendstorage/inmemory"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	fornaxv1.AddToScheme(scheme)
}

func main() {
	// initialize in memory store
	app := &fornaxv1.Application{}
	appStore := inmemory.NewMemoryStore(app.GetGroupVersionResource().GroupResource(), true)
	appStore.Load()
	appSession := &fornaxv1.ApplicationSession{}
	appSessionStore := inmemory.NewMemoryStore(appSession.GetGroupVersionResource().GroupResource(), true)
	appSessionStore.Load()

	// new fornaxcore grpc grpcServer which implement node agent proxy
	grpcServer := grpc_server.NewGrpcServer()

	// start node and pod manager
	podManager := pod.NewPodManager(context.Background(), grpcServer)
	sessionManager := session.NewSessionManager(context.Background(), grpcServer, podManager)
	sessionManager.Run(context.Background())
	nodeManager := node.NewNodeManager(context.Background(), node.DefaultStaleNodeTimeout, grpcServer, podManager, sessionManager)
	podScheduler := podscheduler.NewPodScheduler(context.Background(), grpcServer, nodeManager, podManager,
		&podscheduler.SchedulePolicy{
			NumOfEvaluatedNodes: 200,
			BackoffDuration:     10 * time.Second,
			NodeSortingMethod:   podscheduler.NodeSortingMethodMoreMemory,
		})
	podManager.Run(podScheduler)
	podScheduler.Run()
	nodeManager.Run()

	// start application manager at last as it require api server
	klog.Info("starting application manager")
	appManager := application.NewApplicationManager(context.Background(), podManager, sessionManager)
	go appManager.Run(context.Background())

	// start fornaxcore grpc server to listen nodes
	klog.Info("starting fornaxcore grpc node agent server")
	port := 18001
	// TODO, get certificates from command line arguments
	certFile := ""
	keyFile := ""
	err := grpcServer.RunGrpcServer(context.Background(), nodemonitor.NewNodeMonitor(nodeManager), port, certFile, keyFile)
	if err != nil {
		klog.Fatal(err)
	}
	klog.Info("Fornaxcore grpc server started")

	// start api server
	klog.Info("starting fornaxcore rest api server")
	// +kubebuilder:scaffold:resource-register
	apiserver := builder.APIServer.
		WithLocalDebugExtension().
		WithConfigFns(func(config *server.RecommendedConfig) *server.RecommendedConfig {
			optionsGetter := config.RESTOptionsGetter
			config.RESTOptionsGetter = &store.FornaxRestOptionsFactory{
				OptionsGetter: optionsGetter,
			}
			return config
		}).
		WithOptionsFns(func(options *builder.ServerOptions) *builder.ServerOptions {
			return options
		}).
		WithServerFns(func(server *builder.GenericAPIServer) *builder.GenericAPIServer {
			return server
		}).
		WithResource(&fornaxv1.Application{}).
		WithResource(&fornaxv1.ApplicationSession{})
	err = apiserver.Execute()
	if err != nil {
		klog.Fatal(err)
		os.Exit(-1)
	}

}
