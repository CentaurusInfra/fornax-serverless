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
	"runtime/debug"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/klog/v2"
	"sigs.k8s.io/apiserver-runtime/pkg/builder"

	// +kubebuilder:scaffold:resource-imports

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxk8sv1 "centaurusinfra.io/fornax-serverless/pkg/apis/k8s/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/apis/openapi"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/application"
	grpc_server "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/server"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/node"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/nodemonitor"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/podscheduler"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/session"
	"centaurusinfra.io/fornax-serverless/pkg/store"
	"centaurusinfra.io/fornax-serverless/pkg/store/factory"
	// "github.com/pkg/profile"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	fornaxv1.AddToScheme(scheme)
}

func main() {
	// defer profile.Start().Stop()
	debug.SetGCPercent(300)
	klog.Info("Initialize fornax resource memory store")
	ctx := context.Background()
	nodeStore := factory.NewFornaxNodeStorage(ctx)
	podStore := factory.NewFornaxPodStorage(ctx)
	appStatusStore := factory.NewFornaxApplicationStatusStorage(ctx)
	appSessionStore := factory.NewFornaxApplicationSessionStorage(ctx)

	// build api server, it parse command line flags
	klog.Info("Build fornaxcore rest api server")
	// +kubebuilder:scaffold:resource-register
	apiserver := builder.APIServer.
		WithOpenAPIDefinitions("FornaxCore", "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1", openapi.GetFornaxOpenAPIDefinitions).
		WithConfigFns(func(config *server.RecommendedConfig) *server.RecommendedConfig {
			optionsGetter := config.RESTOptionsGetter
			config.RESTOptionsGetter = &factory.FornaxRestOptionsFactory{
				OptionsGetter: optionsGetter,
			}
			return config
		}).
		WithOptionsFns(func(options *builder.ServerOptions) *builder.ServerOptions {
			options.RecommendedOptions.Authorization = nil
			options.RecommendedOptions.CoreAPI = nil
			options.RecommendedOptions.Admission = nil
			options.RecommendedOptions.Authentication.RemoteKubeConfigFileOptional = true
			return options
		}).
		WithServerFns(func(server *builder.GenericAPIServer) *builder.GenericAPIServer {
			return server
		}).
		WithFlagFns(func(flagSet *pflag.FlagSet) *pflag.FlagSet {
			// klog.InitFlags(flagSet)
			return flagSet
		}).
		WithResource(&fornaxv1.Application{}).
		WithResource(&fornaxv1.ApplicationSession{}).
		WithResourceAndHandler(&fornaxk8sv1.FornaxPod{}, store.FornaxReadonlyResourceHandler(&fornaxk8sv1.FornaxPod{})).
		WithResourceAndHandler(&fornaxk8sv1.FornaxNode{}, store.FornaxReadonlyResourceHandler(&fornaxk8sv1.FornaxNode{}))
	apiServerCmd, err := apiserver.Build()
	if err != nil {
		klog.Fatal(err)
		os.Exit(-1)
	}

	// new fornaxcore grpc server which talk with node agent
	klog.Info("Build Fornaxcore grpc server")
	nodeAgentServer := grpc_server.NewGrpcServer()

	// start internal managers and pod scheduler
	podManager := pod.NewPodManager(ctx, podStore, nodeAgentServer)
	sessionManager := session.NewSessionManager(ctx, appSessionStore, nodeAgentServer)
	nodeManager := node.NewNodeManager(ctx, nodeStore, nodeAgentServer, podManager, sessionManager)
	podScheduler := podscheduler.NewPodScheduler(ctx, nodeAgentServer, nodeManager, podManager,
		&podscheduler.SchedulePolicy{
			NumOfEvaluatedNodes: 100,
			BackoffDuration:     10 * time.Second,
			NodeSortingMethod:   podscheduler.NodeSortingMethodMoreMemory,
		})
	klog.Info("Starting pod scheduler")
	podScheduler.Run()
	klog.Info("Starting pod manager")
	podManager.Run(podScheduler)
	klog.Info("Starting node manager")
	nodeManager.Run()

	// start application manager at last as it require api server
	klog.Info("starting application manager")
	appManager := application.NewApplicationManager(ctx, podManager, sessionManager, appStatusStore)
	appManager.Run(ctx)

	// start fornaxcore grpc nodeagnet server to listen node agents
	klog.Info("Starting Fornaxcore grpc server")
	// TODO get certn and keyFile from commandline flags, --tls-cert-file --tls-private-key-file
	certFile := ""
	keyFile := ""
	port := 18001
	err = nodeAgentServer.RunGrpcServer(ctx, nodemonitor.NewNodeMonitor(nodeManager), port, certFile, keyFile)
	if err != nil {
		klog.Fatal(err)
		os.Exit(-1)
	}
	klog.Info("Fornaxcore grpc server started")

	// start api server to listen to clients
	err = apiServerCmd.Execute()
	if err != nil {
		klog.Fatal(err)
		os.Exit(-1)
	}

}
