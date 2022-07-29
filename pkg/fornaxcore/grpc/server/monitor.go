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

package server

import (
	"context"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
)

type NodeMonitor interface {
	OnRegistry(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnNodeReady(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnNodeStateUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnPodStateUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
}

type PodMonitor interface {
	OnPodUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
}

type AppSessionMonitor interface {
	OnSessionStart(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnSessionClose(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
	OnSessionUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error)
}
