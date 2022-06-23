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
	OnRegistry(msgDispatcher MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error
	OnReady(msgDispatcher MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error
	OnUpdate(msgDispatcher MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error
}

type PodMonitor interface {
	OnUpdate(msgDispatcher MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error
}

type AppSessionMonitor interface {
	OnStart(msgDispatcher MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error
	OnClose(msgDispatcher MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error
	OnUpdate(msgDispatcher MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error
}
