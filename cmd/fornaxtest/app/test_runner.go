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
	"sync"
	"time"

	"centaurusinfra.io/fornax-serverless/cmd/fornaxtest/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	// "k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/component-base/version/verflag"
)

func init() {
	utilruntime.Must(logs.AddFeatureGates(utilfeature.DefaultMutableFeatureGate))
}

const (
	FornaxCoreTestRunner = "fornaxcore_testrunner"
)

func NewCommand() *cobra.Command {
	flagSet := pflag.NewFlagSet(FornaxCoreTestRunner, pflag.ContinueOnError)
	flagSet.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)

	simulateConfig := config.DefaultConfiguration()
	config.AddConfigFlags(flagSet, simulateConfig)

	cmd := &cobra.Command{
		Use:                FornaxCoreTestRunner,
		Long:               `simulate fornax core client to create application and session for function and performance test`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

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

			Run(ctx, *simulateConfig)
			return nil
		},
	}
	flagSet.BoolP("help", "h", false, fmt.Sprintf("help for %s", cmd.Name()))

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

func Run(ctx context.Context, testConfig config.TestConfiguration) {
	// initApplicationSessionInformer(ctx)

	RunTest := func(cycle int) {
		wg := sync.WaitGroup{}
		for i := 0; i < testConfig.NumOfApps; i++ {
			namespace := "fornaxtest"
			appName := fmt.Sprintf("echoserver%d", cycle*testConfig.NumOfApps+i)
			wg.Add(1)
			go func() {
				switch testConfig.TestCase {
				case config.AppFullCycleTest:
					runAppFullCycleTest(namespace, appName, testConfig)
				case config.SessionFullCycleTest:
					runSessionFullCycleTest(namespace, appName, testConfig)
				case config.SessionCreateTest:
					createSessionTest(namespace, appName, testConfig)
				case config.AppCreateTest:
				}
				wg.Done()
			}()

			if i > 0 && i%testConfig.NumOfAppPerSec == 0 {
				klog.Infof("created %d app", i)
				time.Sleep(1000 * time.Millisecond)
			}
		}
		klog.Infof("created %d app", testConfig.NumOfApps)
		wg.Wait()
	}
	logs.InitLogs()

	startTime := time.Now().UnixMilli()
	for i := 0; i < testConfig.NumOfTestCycle; i++ {
		st := time.Now().UnixMilli()
		klog.Infof("--------Test %d begin--------\n", i+1)
		oneTestCycleSessions = TestSessionArray{}
		RunTest(i)
		et := time.Now().UnixMilli()
		summarySessionTestResult(oneTestCycleSessions, st, et)
		allTestSessions = append(allTestSessions, oneTestCycleSessions...)
		klog.Infof("--------Test %d end----------\n\n", i+1)
	}
	endTime := time.Now().UnixMilli()

	klog.Infof("--------Test summary ----------\n")
	summaryAppTestResult(allTestApps, startTime, endTime)
	summarySessionTestResult(allTestSessions, startTime, endTime)
	os.Exit(0)
}
