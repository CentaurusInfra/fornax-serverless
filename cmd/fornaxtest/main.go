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
	"fmt"
	"math/rand"
	"os"
	"time"

	"centaurusinfra.io/fornax-serverless/cmd/fornaxtest/app"
	"github.com/spf13/cobra"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	_ "k8s.io/component-base/logs/json/register" // for JSON log format registration
)

func main() {
	command := app.NewCommand()

	code := run(command)
	os.Exit(code)
}

func run(command *cobra.Command) int {
	defer logs.FlushLogs()
	rand.Seed(time.Now().UnixNano())

	command.SetGlobalNormalizationFunc(cliflag.WordSepNormalizeFunc)
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	return 0
}
