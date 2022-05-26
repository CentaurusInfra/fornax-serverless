package server

import (
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"context"
)

type NodeMonitor interface {
	OnRegistry(ctx context.Context, state *grpc.NodeRegistry) error
	OnReady(ctx context.Context, state *grpc.NodeReady) error
	OnUpdate(ctx context.Context, state *grpc.NodeState) error
}

type PodMonitor interface {
	OnCreate(ctx context.Context, state *grpc.PodCreate) error
	OnTerminate(ctx context.Context, state *grpc.PodTerminate) error
	OnUpdate(ctx context.Context, state *grpc.PodState) error
	OnActive(ctx context.Context, state *grpc.PodActive) error
}

type AppSessionMonitor interface {
	OnStart(ctx context.Context, state *grpc.SessionStart) error
	OnClose(ctx context.Context, state *grpc.SessionClose) error
	OnUpdate(ctx context.Context, state *grpc.SessionState) error
}
