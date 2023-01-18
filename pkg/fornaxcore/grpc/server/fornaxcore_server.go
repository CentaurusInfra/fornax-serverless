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
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sync"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxcore_grpc "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/protobuf/types/known/emptypb"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const NodeOutgoingChanBufferSize = 20
const DefaultNodeIncomingHandlerNum = 5

type FornaxCoreServer interface {
	fornaxcore_grpc.FornaxCoreServiceServer
	nodeagent.NodeAgentClient
}

var _ FornaxCoreServer = &grpcServer{}

type grpcServer struct {
	sync.RWMutex
	fornaxcore_grpc.UnimplementedFornaxCoreServiceServer
	nodeMonitor             ie.NodeMonitorInterface
	nodeOutgoingChans       map[string]chan<- *fornaxcore_grpc.FornaxCoreMessage
	nodeIncommingChans      map[string]chan *fornaxcore_grpc.FornaxCoreMessage
	nodeIncommingChansMutex sync.RWMutex
	nodeMessageHandlerChans []chan *fornaxcore_grpc.FornaxCoreMessage
}

func (g *grpcServer) RunGrpcServer(ctx context.Context, nodeMonitor ie.NodeMonitorInterface, port int, certFile, keyFile string) error {
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		klog.ErrorS(err, "Fornaxcore grpc server failed to listen on port:", port)
		return err
	}
	var opts []grpc.ServerOption
	if certFile != "" && keyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			klog.ErrorS(err, "Fornaxcore grpc server failed to generate credentials", "certFile", certFile, "keyFile", keyFile)
			return err
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}

	// start node agent grpc server
	g.nodeMonitor = nodeMonitor
	grpcServer := grpc.NewServer(opts...)
	fornaxcore_grpc.RegisterFornaxCoreServiceServer(grpcServer, g)
	go func() {
		err = grpcServer.Serve(lis)
		if err != nil {
			klog.ErrorS(err, "Fornaxcore grpc server stopped to serve")
		}
	}()

	for _, v := range g.nodeMessageHandlerChans {
		go func(ch chan *fornaxcore_grpc.FornaxCoreMessage) {
			for {
				select {
				case <-ctx.Done():
					return
				case msg := <-ch:
					g.handleMessages(msg)
				}
			}
		}(v)
	}
	return nil
}

func (g *grpcServer) enlistNode(node string, ch chan<- *fornaxcore_grpc.FornaxCoreMessage) error {
	g.Lock()
	if _, ok := g.nodeOutgoingChans[node]; ok {
		g.Unlock()
		return fmt.Errorf("node %s already has channel", node)
	}
	g.nodeOutgoingChans[node] = ch
	g.Unlock()
	err := g.nodeMonitor.OnNodeConnect(node)
	if err != nil {
		if err == nodeagent.NodeRevisionOutOfOrderError {
			g.DispatchNodeMessage(node, NewFullSyncRequest())
		} else {
			return err
		}
	}
	return nil
}

func (g *grpcServer) delistNode(node string) {
	g.Lock()
	g.nodeMonitor.OnNodeDisconnect(node)
	if ch, found := g.nodeOutgoingChans[node]; found {
		delete(g.nodeOutgoingChans, node)
		close(ch)
	}
	g.Unlock()
}

func (g *grpcServer) GetMessage(identifier *fornaxcore_grpc.NodeIdentifier, server fornaxcore_grpc.FornaxCoreService_GetMessageServer) error {
	ch := make(chan *fornaxcore_grpc.FornaxCoreMessage, NodeOutgoingChanBufferSize)
	if err := g.enlistNode(identifier.GetIdentifier(), ch); err != nil {
		close(ch)
		return fmt.Errorf("Fornax core has established channel with this node: %s", identifier)
	}

	chDone := server.Context().Done()
	for {
		select {
		case <-chDone:
			g.delistNode(identifier.GetIdentifier())
			return nil
		case msg := <-ch:
			msg.NodeIdentifier = identifier
			if err := server.Send(msg); err != nil {
				klog.ErrorS(err, "Failed to send message via GetMessage stream connection", "node", identifier)
				g.delistNode(identifier.GetIdentifier())
				return err
			}
		}
	}
}

// get handeller channel to handle incomming message from this node, if not found,
// randomly assign one channel in nodeMessageHandlerChans to this node and save it for following incomming messages from this node
func (g *grpcServer) getNodeMessageHandlerChannel(nodeId string) chan *fornaxcore_grpc.FornaxCoreMessage {
	g.nodeIncommingChansMutex.RLock()
	if messageCh, found := g.nodeIncommingChans[nodeId]; found {
		g.nodeIncommingChansMutex.RUnlock()
		return messageCh
	} else {
		g.nodeIncommingChansMutex.RUnlock()
		i := rand.Intn(len(g.nodeMessageHandlerChans))
		channel := g.nodeMessageHandlerChans[i]
		g.nodeIncommingChansMutex.Lock()
		g.nodeIncommingChans[nodeId] = channel
		g.nodeIncommingChansMutex.Unlock()
		return channel
	}
}

// PutMessage send node's message to handler to process message and return
func (g *grpcServer) PutMessage(ctx context.Context, message *fornaxcore_grpc.FornaxCoreMessage) (*empty.Empty, error) {
	messageCh := g.getNodeMessageHandlerChannel(message.GetNodeIdentifier().GetIdentifier())
	messageCh <- message
	return &emptypb.Empty{}, nil
}

func (g *grpcServer) handleMessages(message *fornaxcore_grpc.FornaxCoreMessage) {
	var err error
	var reply *fornaxcore_grpc.FornaxCoreMessage
	switch message.GetMessageType() {
	case fornaxcore_grpc.MessageType_NODE_REGISTER:
		reply, err = g.nodeMonitor.OnNodeRegistry(message)
	case fornaxcore_grpc.MessageType_NODE_READY:
		reply, err = g.nodeMonitor.OnNodeReady(message)
	case fornaxcore_grpc.MessageType_NODE_STATE:
		reply, err = g.nodeMonitor.OnNodeStateUpdate(message)
	case fornaxcore_grpc.MessageType_POD_STATE:
		reply, err = g.nodeMonitor.OnPodStateUpdate(message)
	case fornaxcore_grpc.MessageType_SESSION_STATE:
		reply, err = g.nodeMonitor.OnSessionUpdate(message)
	default:
		klog.Errorf(fmt.Sprintf("not supported message type %s, message %v", message.GetMessageType(), message))
	}
	if err != nil {
		klog.ErrorS(err, "Failed to process a node message", "node", message.GetNodeIdentifier(), "msgType", message.GetMessageType())
	}
	if err == nodeagent.NodeRevisionOutOfOrderError {
		g.DispatchNodeMessage(message.GetNodeIdentifier().GetIdentifier(), NewFullSyncRequest())
	}
	if reply != nil {
		g.DispatchNodeMessage(message.GetNodeIdentifier().GetIdentifier(), reply)
	}
}

func (g *grpcServer) mustEmbedUnimplementedFornaxCoreServiceServer() {
}

func NewGrpcServer() *grpcServer {
	handlerChans := []chan *fornaxcore_grpc.FornaxCoreMessage{}
	for i := 0; i < DefaultNodeIncomingHandlerNum; i++ {
		handlerChans = append(handlerChans, make(chan *fornaxcore_grpc.FornaxCoreMessage, 1000))
	}

	return &grpcServer{
		RWMutex:                              sync.RWMutex{},
		nodeOutgoingChans:                    make(map[string]chan<- *fornaxcore_grpc.FornaxCoreMessage),
		nodeIncommingChans:                   make(map[string]chan *fornaxcore_grpc.FornaxCoreMessage),
		nodeMonitor:                          nil,
		UnimplementedFornaxCoreServiceServer: fornaxcore_grpc.UnimplementedFornaxCoreServiceServer{},
		nodeMessageHandlerChans:              handlerChans,
	}
}

// CreatePod dispatch a PodCreate grpc message to node agent
func (g *grpcServer) CreatePod(nodeIdentifier string, pod *v1.Pod) error {
	podIdentifier := util.Name(pod)
	messageType := fornaxcore_grpc.MessageType_POD_CREATE
	podCreate := fornaxcore_grpc.FornaxCoreMessage_PodCreate{
		PodCreate: &fornaxcore_grpc.PodCreate{
			PodIdentifier: podIdentifier,
			Pod:           pod.DeepCopy(),
			ConfigMap:     &v1.ConfigMap{},
		},
	}
	m := &fornaxcore_grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &podCreate,
	}

	err := g.DispatchNodeMessage(nodeIdentifier, m)
	if err != nil {
		klog.ErrorS(err, "Failed to dispatch pod create message to node", "node", nodeIdentifier, "pod", util.Name(pod))
		return err
	}
	return nil
}

// TerminatePod dispatch a PodTerminate grpc message to node agent
func (g *grpcServer) TerminatePod(nodeIdentifier string, pod *v1.Pod) error {
	podIdentifier := util.Name(pod)
	messageType := fornaxcore_grpc.MessageType_POD_TERMINATE
	podTerminate := fornaxcore_grpc.FornaxCoreMessage_PodTerminate{
		PodTerminate: &fornaxcore_grpc.PodTerminate{
			PodIdentifier: podIdentifier,
		},
	}
	m := &fornaxcore_grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &podTerminate,
	}

	err := g.DispatchNodeMessage(nodeIdentifier, m)
	if err != nil {
		klog.ErrorS(err, "Failed to dispatch pod terminate message to node", "node", nodeIdentifier, "pod", util.Name(pod))
		return err
	}
	return nil
}

// HibernatePod dispatch a PodHibernate grpc message to node agent
func (g *grpcServer) HibernatePod(nodeIdentifier string, pod *v1.Pod) error {
	podIdentifier := util.Name(pod)
	messageType := fornaxcore_grpc.MessageType_POD_HIBERNATE
	podHibernate := fornaxcore_grpc.FornaxCoreMessage_PodHibernate{
		PodHibernate: &fornaxcore_grpc.PodHibernate{
			PodIdentifier: podIdentifier,
		},
	}
	m := &fornaxcore_grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &podHibernate,
	}

	err := g.DispatchNodeMessage(nodeIdentifier, m)
	if err != nil {
		klog.ErrorS(err, "Failed to dispatch pod hibernate message to node", "node", nodeIdentifier, "pod", util.Name(pod))
		return err
	}
	return nil
}

// CloseSession dispatch a SessionClose event to node agent
func (g *grpcServer) CloseSession(nodeIdentifier string, pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	sessionIdentifier := util.Name(session)
	podIdentifier := util.Name(pod)
	messageType := fornaxcore_grpc.MessageType_SESSION_CLOSE
	body := fornaxcore_grpc.FornaxCoreMessage_SessionClose{
		SessionClose: &fornaxcore_grpc.SessionClose{
			SessionIdentifier: sessionIdentifier,
			PodIdentifier:     podIdentifier,
		},
	}
	m := &fornaxcore_grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &body,
	}

	err := g.DispatchNodeMessage(nodeIdentifier, m)
	if err != nil {
		klog.ErrorS(err, "Failed to dispatch message to node", "node", nodeIdentifier, "session", sessionIdentifier)
		return err
	}
	return nil

}

// OpenSession implements FornaxCoreServer
func (g *grpcServer) OpenSession(nodeIdentifier string, pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	sessionData, err := json.Marshal(session)
	if err != nil {
		return err
	}
	// OpenSession dispatch a SessionOpen event to node agent
	sessionIdentifier := util.Name(session)
	podIdentifier := util.Name(pod)
	messageType := fornaxcore_grpc.MessageType_SESSION_OPEN
	body := fornaxcore_grpc.FornaxCoreMessage_SessionOpen{
		SessionOpen: &fornaxcore_grpc.SessionOpen{
			SessionIdentifier: sessionIdentifier,
			PodIdentifier:     podIdentifier,
			SessionData:       sessionData,
		},
	}
	m := &fornaxcore_grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &body,
	}

	err = g.DispatchNodeMessage(nodeIdentifier, m)
	if err != nil {
		klog.ErrorS(err, "Failed to dispatch message to node", "node", nodeIdentifier, "session", sessionIdentifier)
		return err
	}
	return nil

}

// FullSyncNode dispatch a NodeFullSync request grpc message to node agent
func (g *grpcServer) FullSyncNode(nodeIdentifier string) error {

	msg := NewFullSyncRequest()

	err := g.DispatchNodeMessage(nodeIdentifier, msg)
	if err != nil {
		klog.ErrorS(err, "Failed to dispatch full sync message to node", "node", nodeIdentifier)
		return err
	}
	return nil
}

func NewFullSyncRequest() *fornaxcore_grpc.FornaxCoreMessage {
	msg := fornaxcore_grpc.FornaxCoreMessage_NodeFullSync{
		NodeFullSync: &fornaxcore_grpc.NodeFullSync{},
	}
	messageType := fornaxcore_grpc.MessageType_NODE_FULL_SYNC
	return &fornaxcore_grpc.FornaxCoreMessage{
		MessageType: messageType,
		MessageBody: &msg,
	}
}
