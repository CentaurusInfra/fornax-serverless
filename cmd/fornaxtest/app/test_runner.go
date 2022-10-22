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

	"k8s.io/apimachinery/pkg/util/rand"

	// "k8s.io/apimachinery/pkg/util/wait"

	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/component-base/version/verflag"
)

var (
	addevents = int32(0)
	updevents = int32(0)
)

var (
	allTestApps         = TestApplicationArray{}
	allTestSessions     = TestSessionArray{}
	appSessionMap       = TestSessionMap{}
	appSessionMapLock   = sync.Mutex{}
	testSessionCounters = []*TestSessionCounter{}
)

type TestSessionCounter struct {
	numOfSessions int
	st            int64
	et            int64
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

	RunTest := func(namespace, appName, randAppName string) {
		klog.Infof("--------App %s Test begin--------\n", appName)
		for i := 1; i <= testConfig.NumOfTestCycle; i++ {
			sessions := []*TestSession{}
			cycleName := fmt.Sprintf("%s-cycle-%d", randAppName, i)
			switch testConfig.TestCase {
			case config.AppFullCycleTest:
				sessions = runAppFullCycleTest(cycleName, namespace, appName, testConfig)
			case config.SessionFullCycleTest:
				sessions = runSessionFullCycleTest(cycleName, namespace, appName, testConfig)
			case config.SessionCreateTest:
				sessions = createSessionTest(cycleName, namespace, appName, testConfig)
			case config.AppCreateTest:
			}
			appSessionMapLock.Lock()
			allTestSessions = append(allTestSessions, sessions...)
			appSessionMapLock.Unlock()
		}
		klog.Infof("--------App %s Test end----------\n\n", appName)
	}
	logs.InitLogs()

	ns := "fornaxtest"
	initApplicationSessionInformer(ctx, ns)

	done := false
	st := time.Now().UnixMilli()
	go func() {
		for {
			time.Sleep(1 * time.Second)
			et := time.Now().UnixMilli()
			testSessionCounters = append(testSessionCounters, &TestSessionCounter{
				numOfSessions: len(allTestSessions),
				st:            st,
				et:            et,
			})
			if done {
				break
			}
		}
	}()

	// start to test all apps
	randAppName := rand.String(16)
	wgAppTest := sync.WaitGroup{}
	for i := 0; i < testConfig.NumOfApps; i++ {
		wgAppTest.Add(1)
		appName := fmt.Sprintf("echoserver%d", i)
		klog.Infof("Run test app %s", appName)
		go func(app string) {
			RunTest(ns, app, randAppName)
			wgAppTest.Done()
		}(appName)
	}
	wgAppTest.Wait()
	done = true

	et := time.Now().UnixMilli()
	klog.Infof("--------Test summary ----------\n")
	fmt.Printf("Received %d add watch events\n", addevents)
	fmt.Printf("Received %d upd watch events\n", updevents)
	summaryAppTestResult(allTestApps, st, et)
	summarySessionTestResult(allTestSessions, st, et)
	os.Exit(0)
}
