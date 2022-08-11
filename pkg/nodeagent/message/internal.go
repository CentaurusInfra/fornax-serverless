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

package message

import (
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
)

type NodeUpdate struct{}

type PodSandboxCreated struct {
	Pod *types.FornaxPod
}

type PodSandboxReady struct {
	Pod *types.FornaxPod
}

type PodContainerCreated struct {
	Pod       *types.FornaxPod
	Container *types.FornaxContainer
}

// when runtime container is started
type PodContainerStarted struct {
	Pod       *types.FornaxPod
	Container *types.FornaxContainer
}

type PodContainerStandy struct {
	Pod       *types.FornaxPod
	Container *types.FornaxContainer
}

// when runtime container readiness probe failed
type PodContainerUnhealthy struct {
	Pod       *types.FornaxPod
	Container *types.FornaxContainer
}

// when runtime container readiness probe succeeded,
// if no readyness probe, treat it as ready when runtime container is in runninng state
type PodContainerReady struct {
	Pod       *types.FornaxPod
	Container *types.FornaxContainer
}

type PodContainerStarting struct {
	Pod       *types.FornaxPod
	Container *types.FornaxContainer
}

type PodContainerStopping struct {
	Pod         *types.FornaxPod
	Container   *types.FornaxContainer
	GracePeriod time.Duration
}

// when runtime container stopped with a exit code
type PodContainerStopped struct {
	Pod       *types.FornaxPod
	Container *types.FornaxContainer
}

// when runtime container is removed
type PodContainerTerminated struct {
	Pod       *types.FornaxPod
	Container *types.FornaxContainer
}

// when runtime container startup and liveness probe failed
type PodContainerFailed struct {
	Pod       *types.FornaxPod
	Container *types.FornaxContainer
}

type PodTerminate struct{}

type PodActive struct{}

type PodCreate struct {
	Pod *types.FornaxPod
}

type PodCleanup struct {
	Pod *types.FornaxPod
}

type PodStatusChange struct {
	Pod *types.FornaxPod
}

type PodOOM struct {
	Pod *types.FornaxPod
}

type SessionStart struct {
	sessionId   string
	sessionData map[string]string
}

type SessionClose struct {
	sessionId   string
	gracePeriod time.Duration
}
