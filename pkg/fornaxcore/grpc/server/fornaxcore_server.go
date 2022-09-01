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

const FornaxCoreChanSize = 2

type FornaxCoreServer interface {
	fornaxcore_grpc.FornaxCoreServiceServer
	nodeagent.NodeAgentClient
}

var _ FornaxCoreServer = &grpcServer{}

type grpcServer struct {
	sync.RWMutex
	nodeGetMessageChans map[string]chan<- *fornaxcore_grpc.FornaxCoreMessage

	nodeMonitor ie.NodeMonitorInterface
	fornaxcore_grpc.UnimplementedFornaxCoreServiceServer
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
	} else {

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

	return nil
}

func (g *grpcServer) enlistNode(node string, ch chan<- *fornaxcore_grpc.FornaxCoreMessage) error {
	g.Lock()
	defer g.Unlock()
	if _, ok := g.nodeGetMessageChans[node]; ok {
		return fmt.Errorf("node %s already has channel", node)
	}
	g.nodeMonitor.OnNodeConnect(node)
	g.nodeGetMessageChans[node] = ch
	return nil
}

func (g *grpcServer) delistNode(node string) {
	g.Lock()
	defer g.Unlock()
	g.nodeMonitor.OnNodeDisconnect(node)
	close(g.nodeGetMessageChans[node])
	delete(g.nodeGetMessageChans, node)
}

func (g *grpcServer) GetMessage(identifier *fornaxcore_grpc.NodeIdentifier, server fornaxcore_grpc.FornaxCoreService_GetMessageServer) error {
	var messageSeq int64 = 0
	ch := make(chan *fornaxcore_grpc.FornaxCoreMessage, FornaxCoreChanSize)
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
			messageSeq += 1
			seq := fmt.Sprintf("%d", messageSeq)
			msg.MessageIdentifier = seq
			msg.NodeIdentifier = identifier
			if err := server.Send(msg); err != nil {
				klog.ErrorS(err, "Failed to send message via GetMessage stream connection", "node", identifier)
				g.delistNode(identifier.GetIdentifier())
				return err
			}
		}
	}
}

func (g *grpcServer) PutMessage(ctx context.Context, message *fornaxcore_grpc.FornaxCoreMessage) (*empty.Empty, error) {
	var err error
	var msg *fornaxcore_grpc.FornaxCoreMessage
	switch message.GetMessageType() {
	case fornaxcore_grpc.MessageType_NODE_REGISTER:
		msg, err = g.nodeMonitor.OnRegistry(ctx, message)
	case fornaxcore_grpc.MessageType_NODE_READY:
		msg, err = g.nodeMonitor.OnNodeReady(ctx, message)
	case fornaxcore_grpc.MessageType_NODE_STATE:
		msg, err = g.nodeMonitor.OnNodeStateUpdate(ctx, message)
	case fornaxcore_grpc.MessageType_POD_STATE:
		msg, err = g.nodeMonitor.OnPodStateUpdate(ctx, message)
	case fornaxcore_grpc.MessageType_SESSION_STATE:
		msg, err = g.nodeMonitor.OnSessionUpdate(ctx, message)
	default:
		klog.Errorf(fmt.Sprintf("not supported message type %s, message %v", message.GetMessageType(), message))
	}

	if err != nil {
		klog.ErrorS(err, "Failed to process a node message", "node", message.GetNodeIdentifier(), "msgType", message.GetMessageType())
	}

	if err == nodeagent.NodeRevisionOutOfOrderError {
		g.DispatchNodeMessage(message.GetNodeIdentifier().GetIdentifier(), NewFullSyncRequest())
	}

	if msg != nil {
		g.DispatchNodeMessage(message.GetNodeIdentifier().GetIdentifier(), msg)
	}
	return &emptypb.Empty{}, err
}

func (g *grpcServer) mustEmbedUnimplementedFornaxCoreServiceServer() {
}

func NewGrpcServer() *grpcServer {
	return &grpcServer{
		RWMutex:                              sync.RWMutex{},
		nodeGetMessageChans:                  make(map[string]chan<- *fornaxcore_grpc.FornaxCoreMessage),
		UnimplementedFornaxCoreServiceServer: fornaxcore_grpc.UnimplementedFornaxCoreServiceServer{},
	}
}

// CreatePod dispatch a PodCreate grpc message to node agent
func (g *grpcServer) CreatePod(nodeIdentifier string, pod *v1.Pod) error {
	mode := fornaxcore_grpc.PodCreate_Active
	podIdentifier := util.Name(pod)
	messageType := fornaxcore_grpc.MessageType_POD_CREATE
	podCreate := fornaxcore_grpc.FornaxCoreMessage_PodCreate{
		PodCreate: &fornaxcore_grpc.PodCreate{
			PodIdentifier: podIdentifier,
			Mode:          mode,
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
		klog.ErrorS(err, "Failed to dispatch pod create message to node", "node", nodeIdentifier, "session", sessionIdentifier)
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
		klog.ErrorS(err, "Failed to dispatch pod create message to node", "node", nodeIdentifier, "session", sessionIdentifier)
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
