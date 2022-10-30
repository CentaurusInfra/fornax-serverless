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
	"centaurusinfra.io/fornax-serverless/pkg/store/factory"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	fornaxv1.AddToScheme(scheme)
}

func main() {
	// initialize fornax resource memory store
	ctx := context.Background()
	appStatusStore := factory.NewFornaxApplicationStatusStorage(ctx)
	appSessionStore := factory.NewFornaxApplicationSessionStorage(ctx)

	// new fornaxcore grpc grpcServer which implement node agent proxy
	grpcServer := grpc_server.NewGrpcServer()

	// start internal managers and pod scheduler
	podManager := pod.NewPodManager(ctx, grpcServer)
	sessionManager := session.NewSessionManager(ctx, grpcServer, appSessionStore)
	nodeManager := node.NewNodeManager(ctx, grpcServer, podManager, sessionManager)
	podScheduler := podscheduler.NewPodScheduler(ctx, grpcServer, nodeManager, podManager,
		&podscheduler.SchedulePolicy{
			NumOfEvaluatedNodes: 100,
			BackoffDuration:     10 * time.Second,
			NodeSortingMethod:   podscheduler.NodeSortingMethodMoreMemory,
		})
	podScheduler.Run()
	podManager.Run(podScheduler)
	nodeManager.Run()

	// start application manager at last as it require api server
	klog.Info("starting application manager")
	appManager := application.NewApplicationManager(ctx, podManager, sessionManager, appStatusStore)
	appManager.Run(ctx)

	// start fornaxcore grpc server to listen nodes
	klog.Info("starting fornaxcore grpc node agent server")
	port := 18001
	// we are using k8s api server, command line flags are only parsed when apiserver started
	// TODO, parse flags before start api server and get certificates from command line flags,
	certFile := ""
	keyFile := ""
	err := grpcServer.RunGrpcServer(ctx, nodemonitor.NewNodeMonitor(nodeManager), port, certFile, keyFile)
	if err != nil {
		klog.Fatal(err)
	}
	klog.Info("Fornaxcore grpc server started")

	// TODO, wait for all known nodes are registered

	// start api server to listen to clients
	klog.Info("starting fornaxcore rest api server")
	// +kubebuilder:scaffold:resource-register
	apiserver := builder.APIServer.
		WithLocalDebugExtension().
		WithConfigFns(func(config *server.RecommendedConfig) *server.RecommendedConfig {
			optionsGetter := config.RESTOptionsGetter
			config.RESTOptionsGetter = &factory.FornaxRestOptionsFactory{
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
