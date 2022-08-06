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

package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"centaurusinfra.io/fornax-serverless/cmd/simulation/app/snode"
	"centaurusinfra.io/fornax-serverless/cmd/simulation/config"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/network"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/node"
	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	genericapiserver "k8s.io/apiserver/pkg/server"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/component-base/version/verflag"
)

func init() {
	utilruntime.Must(logs.AddFeatureGates(utilfeature.DefaultMutableFeatureGate))
}

const (
	NodeAgent = "nodeagent"
)

func NewCommand() *cobra.Command {
	flagSet := pflag.NewFlagSet(NodeAgent, pflag.ContinueOnError)
	flagSet.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)

	nodeConfig, err := config.DefaultNodeConfiguration()
	if err != nil {
		klog.ErrorS(err, "Failed to initialize node config")
		os.Exit(1)
	}
	config.AddConfigFlags(flagSet, nodeConfig)

	cmd := &cobra.Command{
		Use:                NodeAgent,
		Long:               `node agent takes fornax core command message to start pod and session, it save pod and session info into its own db`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// if err := checkPermissions(); err != nil {
			//  klog.ErrorS(err, "nodeagent is not running with sufficient permissions")
			//  os.Exit(1)
			// }

			ctx := genericapiserver.SetupSignalContext()

			// initial flag parse, since we disable cobra's flag parsing
			if err := flagSet.Parse(args); err != nil {
				return fmt.Errorf("failed to parse flag: %w", err)
			}

			help, err := flagSet.GetBool("help")
			if err != nil {
				return errors.New(`"help" flag is non-bool, programmer error, please correct`)
			}
			if help {
				return cmd.Help()
			}

			verflag.PrintAndExitIfRequested()

			return Run(ctx, *nodeConfig)
		},
	}
	flagSet.BoolP("help", "h", false, fmt.Sprintf("help for %s", cmd.Name()))

	// ugly, but necessary, because Cobra's default UsageFunc and HelpFunc pollute the flagset with global flags
	const usageFmt = "Usage:\n  %s\n\nFlags:\n%s"
	cmd.SetUsageFunc(func(cmd *cobra.Command) error {
		fmt.Fprintf(cmd.OutOrStderr(), usageFmt, cmd.UseLine(), flagSet.FlagUsagesWrapped(2))
		return nil
	})
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n"+usageFmt, cmd.Long, cmd.UseLine(), flagSet.FlagUsagesWrapped(2))
	})

	return cmd
}

func Run(ctx context.Context, nodeConfig config.SimulationNodeConfiguration) error {
	klog.InfoS("Golang settings", "GOGC", os.Getenv("GOGC"), "GOMAXPROCS", os.Getenv("GOMAXPROCS"), "GOTRACEBACK", os.Getenv("GOTRACEBACK"))

	if err := config.ValidateNodeConfiguration(&nodeConfig); len(err) != 0 {
		return fmt.Errorf("invalidate nodeagent configuration, errors: %v, configuration: %v", err, nodeConfig)
	}
	klog.InfoS("NodeConfiguration", "configuration", nodeConfig)

	logs.InitLogs()

	osHostIps, err := network.GetLocalV4IP()
	if err != nil {
		return err
	}
	osHostName, err := os.Hostname()
	if err != nil {
		return err
	}
	for i := 0; i < nodeConfig.NumOfNode; i++ {
		hostName := fmt.Sprintf("%s-%d", osHostName, i)
		go RunNodeAndNodeActor(ctx, osHostIps[0].String(), hostName, nodeConfig)
		klog.Infof("%d th node and node actor created \n", i)
	}

	<-ctx.Done()

	return nil
}

func RunNodeAndNodeActor(ctx context.Context, hostIp, hostName string, nodeConfig config.SimulationNodeConfiguration) error {
	klog.InfoS("NodeConfiguration", "configuration", nodeConfig.Hostname)

	logs.InitLogs()

	go daemon.SdNotify(false, "READY=1")

	fornaxNode := node.FornaxNode{
		NodeConfig:   nodeConfig.NodeConfiguration,
		V1Node:       nil,
		Pods:         node.NewPodPool(),
		Dependencies: nil,
	}
	nodeActor, err := snode.NewNodeActor(hostIp, hostName, &fornaxNode)
	if err != nil {
		klog.Errorf("can not initialize node actor, error %v", err)
	}

	klog.Info("Starting FornaxNode")
	err = nodeActor.Start()
	if err != nil {
		klog.Errorf("Can not start node, error %v", err)
	}
	klog.Info("FornaxNode started")

	return nil
}
