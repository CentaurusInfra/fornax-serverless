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

package runtime

import (
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

func ContainerExit(status *ContainerStatus) bool {
	return status != nil && status.RuntimeStatus != nil && status.RuntimeStatus.FinishedAt != 0 && status.RuntimeStatus.State == criv1.ContainerState_CONTAINER_EXITED
}

func ContainerExitNormal(status *ContainerStatus) bool {
	return ContainerExit(status) && status.RuntimeStatus.ExitCode == 0
}

func ContainerExitAbnormal(status *ContainerStatus) bool {
	return ContainerExit(status) && status.RuntimeStatus.ExitCode != 0
}

func ContainerStatusUnknown(status *ContainerStatus) bool {
	return status != nil && status.RuntimeStatus != nil && status.RuntimeStatus.State == criv1.ContainerState_CONTAINER_UNKNOWN
}

func ContainerRunning(status *ContainerStatus) bool {
	return status != nil && status.RuntimeStatus != nil && status.RuntimeStatus.State == criv1.ContainerState_CONTAINER_RUNNING
}
