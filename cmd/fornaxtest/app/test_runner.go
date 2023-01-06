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

type WatchEventsNum struct {
	delevents int32
	addevents int32
	updevents int32
}

var (
	sessionEventsNum    = WatchEventsNum{}
	appEventsNum        = WatchEventsNum{}
	podEventsNum        = WatchEventsNum{}
	nodeEventsNum       = WatchEventsNum{}
	allTestApps         = TestApplicationArray{}
	appMap              = TestAppMap{}
	appMapLock          = sync.Mutex{}
	podMap              = TestPodMap{}
	podMapLock          = sync.Mutex{}
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
	logs.InitLogs()

	ns := "fornaxtest"
	initNodeInformer(ctx, ns)
	// initPodInformer(ctx, ns)
	// initApplicationInformer(ctx, ns)
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
			klog.Infof("Num of session created, %d", len(allTestSessions))
			if done {
				break
			}
		}
	}()

	RunTest := func(namespace, appName, randSessionPrefix string) {
		klog.Infof("--------App %s Test begin--------\n", appName)
		for i := 1; i <= testConfig.NumOfTestCycle; i++ {
			cycleName := fmt.Sprintf("%s-c-%d", randSessionPrefix, i)
			switch testConfig.TestCase {
			case config.AppFullCycleTest:
				runAppFullCycleTest(cycleName, namespace, appName, testConfig)
			case config.SessionFullCycleTest:
				runSessionFullCycleTest(cycleName, namespace, appName, testConfig)
			case config.SessionCreateTest:
				createSessionTest(cycleName, namespace, appName, testConfig)
			default:
				klog.Infof("not recognized test case", testConfig.TestCase)
			}
		}
		klog.Infof("--------App %s Test end----------\n\n", appName)
	}

	// start to test all apps
	randSessionName := rand.String(16)
	wgAppTest := sync.WaitGroup{}
	for i := 0; i < testConfig.NumOfApps; i++ {
		wgAppTest.Add(1)
		appName := fmt.Sprintf("%s%d", testConfig.AppNamePrefix, i)
		klog.Infof("Run test app %s", appName)
		go func(app string) {
			RunTest(ns, app, randSessionName)
			wgAppTest.Done()
		}(appName)
	}
	wgAppTest.Wait()
	done = true

	et := time.Now().UnixMilli()
	klog.Infof("--------Test summary ----------\n")
	fmt.Printf("Received %d app add watch events\n", appEventsNum.addevents)
	fmt.Printf("Received %d app upd watch events\n", appEventsNum.updevents)
	fmt.Printf("Received %d app del watch events\n", appEventsNum.delevents)
	fmt.Printf("Received %d session add watch events\n", sessionEventsNum.addevents)
	fmt.Printf("Received %d session upd watch events\n", sessionEventsNum.updevents)
	fmt.Printf("Received %d session del watch events\n", sessionEventsNum.delevents)
	fmt.Printf("Received %d pod add watch events\n", podEventsNum.addevents)
	fmt.Printf("Received %d pod upd watch events\n", podEventsNum.updevents)
	fmt.Printf("Received %d pod del watch events\n", podEventsNum.delevents)
	fmt.Printf("Received %d node add watch events\n", nodeEventsNum.addevents)
	fmt.Printf("Received %d node upd watch events\n", nodeEventsNum.updevents)
	fmt.Printf("Received %d node del watch events\n", nodeEventsNum.delevents)
	summaryAppTestResult(allTestApps, st, et)
	summarySessionTestResult(allTestSessions, st, et)
	os.Exit(0)
}
