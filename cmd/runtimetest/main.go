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
	"os"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/dependency"
	"k8s.io/klog/v2"
)

func main() {
	runtimeService, err := dependency.InitRuntimeService(config.DefaultContainerRuntimeEndpoint)
	if err != nil {
		klog.ErrorS(err, "Failed to init runtime service")
		os.Exit(-1)
	}

	switch os.Args[1] {
	case "hibernate":
		runtimeService.HibernateContainer(os.Args[2])
	case "wakeup":
		runtimeService.WakeupContainer(os.Args[2])
	}
}
