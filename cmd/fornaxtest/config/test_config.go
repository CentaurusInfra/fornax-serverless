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
package config

import (
	"github.com/spf13/pflag"
)

type TestConfiguration struct {
	TestCase             string
	NumOfTestCycle       int
	NumOfApps            int
	NumOfBurstPodsPerApp int
	NumOfInitPodsPerApp  int
	NumOfSessionPerApp   int
	AppNamePrefix        string
	NoNodeSessionService bool
}

type TestCase string

const (
	AppFullCycleTest     = "app_full_cycle"     // create app, create session, and delete app
	SessionFullCycleTest = "session_full_cycle" // create app, continously create session and delete session, delete app at last
	SessionCreateTest    = "session_create"     // create session only
)

func AddConfigFlags(flagSet *pflag.FlagSet, simuConfig *TestConfiguration) {
	flagSet.StringVar(&simuConfig.TestCase, "test-case", simuConfig.TestCase, "which test is running, app_full_cycle,session_full_cycle,session_create")
	flagSet.IntVar(&simuConfig.NumOfApps, "num-of-app", simuConfig.NumOfApps, "how many applications are simulated")
	flagSet.IntVar(&simuConfig.NumOfInitPodsPerApp, "num-of-init-pod-per-app", simuConfig.NumOfInitPodsPerApp, "how many applications pods are precreated for a app")
	flagSet.IntVar(&simuConfig.NumOfBurstPodsPerApp, "num-of-burst-pod-per-app", simuConfig.NumOfBurstPodsPerApp, "how many applications pods are allow to create at same time for a app")
	flagSet.IntVar(&simuConfig.NumOfSessionPerApp, "num-of-session-per-app", simuConfig.NumOfSessionPerApp, "how many application sessions are created for a app")
	flagSet.IntVar(&simuConfig.NumOfTestCycle, "num-of-test-cycle", simuConfig.NumOfSessionPerApp, "how many test run before exit")
	flagSet.StringVar(&simuConfig.AppNamePrefix, "app-name-prefix", simuConfig.AppNamePrefix, "app name prefix")
	flagSet.BoolVar(&simuConfig.NoNodeSessionService, "no-session-service", simuConfig.NoNodeSessionService, "do not use node session service to open session if has this flag")
}

func DefaultConfiguration() *TestConfiguration {
	return &TestConfiguration{
		TestCase:             AppFullCycleTest,
		NumOfApps:            1,
		NumOfInitPodsPerApp:  0,
		NumOfBurstPodsPerApp: 2,
		NumOfSessionPerApp:   1,
		NumOfTestCycle:       1,
		AppNamePrefix:        "echo",
		NoNodeSessionService: false,
	}
}
