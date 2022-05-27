package server

import (
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"context"
	"fmt"
)

type MessageDispatcher interface {
	DispatchMessage(ctx context.Context, node string, message *grpc.FornaxCoreMessage) error
}

func (g *grpcServer) getNodeChan(node string) (chan<- *grpc.FornaxCoreMessage, error) {
	g.RLock()
	defer g.RUnlock()

	ch, ok := g.chans[node]
	if !ok {
		return nil, fmt.Errorf("unknown destination")
	}

	return ch, nil
}

func (g *grpcServer) DispatchMessage(ctx context.Context, node string, message *grpc.FornaxCoreMessage) error {
	ch, err := g.getNodeChan(node)
	if err != nil {
		return err
	}

	ch <- message
	return nil
}
