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

package util

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	ResourcePID v1.ResourceName = "pid"
	MaxPID                      = 100
)

func ResourceQuantity(quantity int64, resourceName v1.ResourceName) resource.Quantity {
	switch resourceName {
	case v1.ResourceCPU:
		return *resource.NewMilliQuantity(quantity, resource.DecimalSI)
	case v1.ResourceMemory:
		return *resource.NewQuantity(quantity, resource.BinarySI)
	case v1.ResourcePods:
		return *resource.NewQuantity(quantity, resource.DecimalSI)
	case v1.ResourceStorage:
		return *resource.NewQuantity(quantity, resource.BinarySI)
	case v1.ResourceEphemeralStorage:
		return *resource.NewQuantity(quantity, resource.BinarySI)
	default:
		return *resource.NewQuantity(quantity, resource.DecimalSI)
	}
}
