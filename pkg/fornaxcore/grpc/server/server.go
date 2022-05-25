package server

import (
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"context"
	"fmt"
	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc/peer"
	"net"
	"sync"
)

const FornaxCoreChanSize = 2

type FornaxCoreNodeServer interface {
	grpc.FornaxCoreServiceServer
	MessageDispatcher
}

type grpcServer struct {
	sync.RWMutex
	chans map[string]chan<- *grpc.FornaxCoreMessage

	nodeMonitor       NodeMonitor
	podMonitor        PodMonitor
	appSessionMonitor AppSessionMonitor
	grpc.UnimplementedFornaxCoreServiceServer
}

func (g *grpcServer) enlistNode(node string, ch chan<- *grpc.FornaxCoreMessage) {
	g.Lock()
	defer g.Unlock()
	g.chans[node] = ch
}

func (g *grpcServer) delistNode(node string) {
	g.Lock()
	defer g.Unlock()
	close(g.chans[node])
	delete(g.chans, node)
}

func (g *grpcServer) GetMessage(empty *empty.Empty, server grpc.FornaxCoreService_GetMessageServer) error {
	peer, ok := peer.FromContext(server.Context())
	if !ok {
		return fmt.Errorf("failed to identify peer client")
	}
	node, err := net.ResolveTCPAddr(peer.Addr.Network(), peer.Addr.String())
	if err != nil {
		// todo: log error
		return err
	}
	nodeIP := node.IP.String()

	chDone := server.Context().Done()
	ch := make(chan *grpc.FornaxCoreMessage, FornaxCoreChanSize)
	g.enlistNode(nodeIP, ch)

	for {
		select {
		case <-chDone:
			g.delistNode(nodeIP)
			return nil
		case msg := <-ch:
			if err = server.Send(msg); err != nil {
				// todo: log error
				g.delistNode(nodeIP)
				return err
			}
		}
	}
}

func (g *grpcServer) PutMessage(ctx context.Context, message *grpc.FornaxCoreMessage) (*empty.Empty, error) {
	switch message.GetMessageType() {
	case grpc.MessageType_NODE_REGISTER:
		err := g.nodeMonitor.OnRegistry(ctx, message.GetNodeRegistry())
		if err != nil {
			// todo: log error
		}
		return nil, err
	case grpc.MessageType_NODE_READY:
		err := g.nodeMonitor.OnReady(ctx, message.GetNodeReady())
		if err != nil {
			// todo: log error
		}
		return nil, err
	case grpc.MessageType_NODE_STATE:
		err := g.nodeMonitor.OnUpdate(ctx, message.GetNodeState())
		if err != nil {
			// todo: log error
		}
		return nil, err
	case grpc.MessageType_POD_CREATE:
		err := g.podMonitor.OnCreate(ctx, message.GetPodCreate())
		if err != nil {
			// todo: log error
		}
		return nil, err
	case grpc.MessageType_POD_ACTIVE:
		err := g.podMonitor.OnActive(ctx, message.GetPodActive())
		if err != nil {
			// todo: log error
		}
		return nil, err
	case grpc.MessageType_POD_STATE:
		err := g.podMonitor.OnUpdate(ctx, message.GetPodState())
		if err != nil {
			// todo: log error
		}
		return nil, err
	case grpc.MessageType_POD_TERMINATE:
		err := g.podMonitor.OnTerminate(ctx, message.GetPodTerminate())
		if err != nil {
			// todo: log error
		}
		return nil, err
	case grpc.MessageType_SESSION_START:
		err := g.appSessionMonitor.OnStart(ctx, message.GetSessionStart())
		if err != nil {
			// todo: log error
		}
		return nil, err
	case grpc.MessageType_SESSION_STATE:
		err := g.appSessionMonitor.OnUpdate(ctx, message.GetSessionState())
		if err != nil {
			// todo: log error
		}
		return nil, err
	case grpc.MessageType_SESSION_CLOSE:
		err := g.appSessionMonitor.OnClose(ctx, message.GetSessionClose())
		if err != nil {
			// todo: log error
		}
		return nil, err
	}

	// todo: log error message instead of panic
	panicMessage := fmt.Sprintf("not supported message type %s, message %v", message.GetMessageType(), message)
	panic(panicMessage)
}

func (g *grpcServer) mustEmbedUnimplementedFornaxCoreServiceServer() {
}

func New(nodeMonitor NodeMonitor, podMonitor PodMonitor, sessionMonitor AppSessionMonitor) FornaxCoreNodeServer {
	return &grpcServer{
		chans:             make(map[string]chan<- *grpc.FornaxCoreMessage),
		nodeMonitor:       nodeMonitor,
		podMonitor:        podMonitor,
		appSessionMonitor: sessionMonitor,
	}
}
