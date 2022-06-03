/*
Copyright 2018 The Kubernetes Authors.

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

package cm

import (
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	// evictionapi "k8s.io/kubernetes/pkg/kubelet/eviction/api"
)

// hardEvictionReservation returns a resourcelist that includes reservation of resources based on hard eviction thresholds.
// func hardEvictionReservation(thresholds []evictionapi.Threshold, capacity v1.ResourceList) v1.ResourceList {
// 	if len(thresholds) == 0 {
// 		return nil
// 	}
// 	ret := v1.ResourceList{}
// 	for _, threshold := range thresholds {
// 		if threshold.Operator != evictionapi.OpLessThan {
// 			continue
// 		}
// 		switch threshold.Signal {
// 		case evictionapi.SignalMemoryAvailable:
// 			memoryCapacity := capacity[v1.ResourceMemory]
// 			value := evictionapi.GetThresholdQuantity(threshold.Value, &memoryCapacity)
// 			ret[v1.ResourceMemory] = *value
// 		case evictionapi.SignalNodeFsAvailable:
// 			storageCapacity := capacity[v1.ResourceEphemeralStorage]
// 			value := evictionapi.GetThresholdQuantity(threshold.Value, &storageCapacity)
// 			ret[v1.ResourceEphemeralStorage] = *value
// 		}
// 	}
// 	return ret
// }

// parsePercentage parses the percentage string to numeric value.
func parsePercentage(v string) (int64, error) {
	if !strings.HasSuffix(v, "%") {
		return 0, fmt.Errorf("percentage expected, got '%s'", v)
	}
	percentage, err := strconv.ParseInt(strings.TrimRight(v, "%"), 10, 0)
	if err != nil {
		return 0, fmt.Errorf("invalid number in percentage '%s'", v)
	}
	if percentage < 0 || percentage > 100 {
		return 0, fmt.Errorf("percentage must be between 0 and 100")
	}
	return percentage, nil
}

// ParseQOSReserved parses the --qos-reserve-requests option
func ParseQOSReserved(m map[string]string) (*map[v1.ResourceName]int64, error) {
	reservations := make(map[v1.ResourceName]int64)
	for k, v := range m {
		switch v1.ResourceName(k) {
		// Only memory resources are supported.
		case v1.ResourceMemory:
			q, err := parsePercentage(v)
			if err != nil {
				return nil, err
			}
			reservations[v1.ResourceName(k)] = q
		default:
			return nil, fmt.Errorf("cannot reserve %q resource", k)
		}
	}
	return &reservations, nil
}
