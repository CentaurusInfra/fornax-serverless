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

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/dependency"
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

func checkPermissions() error {
	if uid := os.Getuid(); uid != 0 {
		return fmt.Errorf("nodeagent needs to run as uid(0)")
	}
	return nil
}

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
			if err := checkPermissions(); err != nil {
				klog.ErrorS(err, "nodeagent is not running with sufficient permissions")
				os.Exit(1)
			}

			ctx := genericapiserver.SetupSignalContext()

			// initial flag parse, since we disable cobra's flag parsing
			if err := flagSet.Parse(args); err != nil {
				return fmt.Errorf("failed to parse flag: %w", err)
			}

			cmds := flagSet.Args()
			if len(cmds) > 0 {
				return fmt.Errorf("unknown command %+s", cmds[0])
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

func Run(ctx context.Context, nodeConfig config.NodeConfiguration) error {
	klog.InfoS("Golang settings", "GOGC", os.Getenv("GOGC"), "GOMAXPROCS", os.Getenv("GOMAXPROCS"), "GOTRACEBACK", os.Getenv("GOTRACEBACK"))

	if err := config.ValidateNodeConfiguration(nodeConfig); len(err) != 0 {
		return fmt.Errorf("invalidate nodeagent configuration, errors: %v, configuration: %v", err, nodeConfig)
	}
	klog.InfoS("NodeConfiguration", "configuration", nodeConfig)

	logs.InitLogs()

	dependencies, err := dependency.InitBasicDependencies(ctx, nodeConfig)
	if err != nil {
		return fmt.Errorf("failed to init basic dependencies: %w", err)
	}

	if err := run(ctx, nodeConfig, dependencies); err != nil {
		return fmt.Errorf("failed to run node agent: %w", err)
	}
	return nil
}

func run(ctx context.Context, nodeConfig config.NodeConfiguration, dependencies *dependency.Dependencies) error {
	go daemon.SdNotify(false, "READY=1")

	fornaxNode, err := node.NewFornaxNode(nodeConfig, dependencies)
	if err != nil {
		klog.ErrorS(err, "Can not initialize node")
	}
	nodeActor, err := node.NewNodeActor(fornaxNode)
	if err != nil {
		klog.ErrorS(err, "Can not initialize node actor")
	}

	klog.Info("Starting FornaxNode")
	err = nodeActor.Start()
	if err != nil {
		klog.Errorf("Can not start node, error %v", err)
	}
	klog.Info("FornaxNode started")

	// wait until shutdown signal is received
	select {
	case <-ctx.Done():
		break
	}

	return nil
}
