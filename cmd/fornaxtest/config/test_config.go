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
	TestCase            string
	NumOfTestCycle      int
	NumOfApps           int
	NumOfInitPodsPerApp int
	NumOfAppPerSec      int
	NumOfSessionPerSec  int
	NumOfSessionPerApp  int
}

type TestCase string

const (
	AppFullCycleTest     = "app_full_cycle"     // create app, create session, and delete app
	SessionFullCycleTest = "session_full_cycle" // create app, continously create session and delete session, delete app at last
	AppCreateTest        = "app_create"         // create app only
	SessionCreateTest    = "session_create"     // create session only
)

func AddConfigFlags(flagSet *pflag.FlagSet, simuConfig *TestConfiguration) {
	flagSet.StringVar(&simuConfig.TestCase, "test-case", simuConfig.TestCase, "which test is running, app_full_cycle,session_full_cycle,session_create")
	flagSet.IntVar(&simuConfig.NumOfApps, "num-of-app", simuConfig.NumOfApps, "how many applications are simulated")
	flagSet.IntVar(&simuConfig.NumOfInitPodsPerApp, "num-of-init-pod-per-app", simuConfig.NumOfInitPodsPerApp, "how many applications pods are precreated when create app")
	flagSet.IntVar(&simuConfig.NumOfSessionPerSec, "num-of-session-per-sec", simuConfig.NumOfSessionPerSec, "maximum session to created in one second for a application")
	flagSet.IntVar(&simuConfig.NumOfAppPerSec, "num-of-app-per-sec", simuConfig.NumOfAppPerSec, "maximum app to created in one second")
	flagSet.IntVar(&simuConfig.NumOfSessionPerApp, "num-of-session-per-app", simuConfig.NumOfSessionPerApp, "how many application sessions are created for a application")
	flagSet.IntVar(&simuConfig.NumOfTestCycle, "num-of-test-cycle", simuConfig.NumOfSessionPerApp, "how many test run before exit")
}

func DefaultConfiguration() *TestConfiguration {
	return &TestConfiguration{
		TestCase:            AppFullCycleTest,
		NumOfApps:           1,
		NumOfInitPodsPerApp: 0,
		NumOfAppPerSec:      50,
		NumOfSessionPerSec:  50,
		NumOfSessionPerApp:  1,
		NumOfTestCycle:      1,
	}
}
