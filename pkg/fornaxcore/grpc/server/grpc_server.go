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
	"fmt"
	"sync"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/protobuf/types/known/emptypb"
	"k8s.io/klog/v2"
)

const FornaxCoreChanSize = 2

type FornaxCoreServer interface {
	grpc.FornaxCoreServiceServer
	MessageDispatcher
}

var _ FornaxCoreServer = &grpcServer{}

type grpcServer struct {
	sync.RWMutex
	nodeGetMessageChans map[string]chan<- *grpc.FornaxCoreMessage

	nodeMonitor       NodeMonitor
	podMonitor        PodMonitor
	appSessionMonitor AppSessionMonitor
	grpc.UnimplementedFornaxCoreServiceServer
}

func (g *grpcServer) enlistNode(node string, ch chan<- *grpc.FornaxCoreMessage) error {
	g.Lock()
	defer g.Unlock()
	if _, ok := g.nodeGetMessageChans[node]; ok {
		return fmt.Errorf("node %s already has channel", node)
	}
	g.nodeGetMessageChans[node] = ch
	return nil
}

func (g *grpcServer) delistNode(node string) {
	g.Lock()
	defer g.Unlock()
	close(g.nodeGetMessageChans[node])
	delete(g.nodeGetMessageChans, node)
}

func (g *grpcServer) GetMessage(identifier *grpc.NodeIdentifier, server grpc.FornaxCoreService_GetMessageServer) error {
	var messageSeq int64 = 0
	klog.InfoS("Received GetMessage stream connection from node", "node", identifier)
	ch := make(chan *grpc.FornaxCoreMessage, FornaxCoreChanSize)
	if err := g.enlistNode(*identifier.Identifier, ch); err != nil {
		close(ch)
		return fmt.Errorf("Fornax core has established channel with this node: %s", identifier)
	}

	chDone := server.Context().Done()
	for {
		select {
		case <-chDone:
			g.delistNode(*identifier.Identifier)
			return nil
		case msg := <-ch:
			messageSeq += 1
			seq := fmt.Sprintf("%d", messageSeq)
			msg.MessageIdentifier = &seq
			msg.NodeIdentifier = identifier
			if err := server.Send(msg); err != nil {
				klog.ErrorS(err, "Failed to send message via GetMessage stream connection", "node", identifier)
				g.delistNode(*identifier.Identifier)
				return err
			}
		}
	}
}

func (g *grpcServer) PutMessage(ctx context.Context, message *grpc.FornaxCoreMessage) (*empty.Empty, error) {
	klog.InfoS("Received message from node", "node", message.NodeIdentifier, "msgType", message.MessageType)
	var err error
	switch message.GetMessageType() {
	case grpc.MessageType_NODE_REGISTER:
		err = g.nodeMonitor.OnRegistry(g, ctx, message)
	case grpc.MessageType_NODE_READY:
		err = g.nodeMonitor.OnReady(g, ctx, message)
	case grpc.MessageType_NODE_STATE:
		err = g.nodeMonitor.OnUpdate(g, ctx, message)
	case grpc.MessageType_POD_STATE:
		err = g.podMonitor.OnUpdate(g, ctx, message)
	case grpc.MessageType_SESSION_START:
		err = g.appSessionMonitor.OnStart(g, ctx, message)
	case grpc.MessageType_SESSION_STATE:
		err = g.appSessionMonitor.OnUpdate(g, ctx, message)
	case grpc.MessageType_SESSION_CLOSE:
		err = g.appSessionMonitor.OnClose(g, ctx, message)
	default:
		klog.Errorf(fmt.Sprintf("not supported message type %s, message %v", message.GetMessageType(), message))
	}

	if err != nil {
		klog.ErrorS(err, "Failed to process a node message", "nodeIdentifier", message.GetNodeIdentifier(), "msgType", message.GetMessageType())
	}
	return &emptypb.Empty{}, err
}

func (g *grpcServer) mustEmbedUnimplementedFornaxCoreServiceServer() {
}

func New(nodeMonitor NodeMonitor, podMonitor PodMonitor, sessionMonitor AppSessionMonitor) FornaxCoreServer {
	return &grpcServer{
		nodeGetMessageChans: make(map[string]chan<- *grpc.FornaxCoreMessage),
		nodeMonitor:         nodeMonitor,
		podMonitor:          podMonitor,
		appSessionMonitor:   sessionMonitor,
	}
}
