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
	"net"
	"sync"
	"time"

	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/sessionservice"
	session_grpc "centaurusinfra.io/fornax-serverless/pkg/nodeagent/sessionservice/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"

	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"k8s.io/klog/v2"
)

const (
	DefaultSessionHeartBeatDuration             = 1 * time.Minute
	DefaultDeadSessionHeartbeatMissingThreshold = 3
)

type SessionServer interface {
	sessionservice.SessionService
	session_grpc.SessionServiceServer
}

type GetSessionMessageClient struct {
	podId   string
	channel chan<- *session_grpc.SessionMessage
}

type SessionStateHeartbeat struct {
	stateCallback           func(internal.SessionState)
	podId                   string
	sessionId               string
	consectuivePingFailures uint16
	lastSeen                time.Time
}

var _ SessionServer = &grpcServer{}

type grpcServer struct {
	mu sync.RWMutex
	// session state callback and heartbeat map by session id
	stateHeartbeats map[string]*SessionStateHeartbeat

	// pod's get message connection by pod id
	getMessageClients map[string]*GetSessionMessageClient

	session_grpc.UnimplementedSessionServiceServer
}

// mustEmbedUnimplementedSessionServiceServer implements SessionServer
func (*grpcServer) mustEmbedUnimplementedSessionServiceServer() {
	panic("unimplemented")
}

func (g *grpcServer) Run(ctx context.Context, port int32) error {
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		klog.ErrorS(err, "Node agent session grpc server failed to listen", "port", port)
		return err
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	session_grpc.RegisterSessionServiceServer(grpcServer, g)
	go func() {
		err = grpcServer.Serve(lis)
		if err != nil {
			klog.ErrorS(err, "Node agent session grpc server stopped to serve")
		}
	}()

	go func() {
		ticker := time.NewTicker(DefaultSessionHeartBeatDuration)
		for {
			select {
			case <-ctx.Done():
				grpcServer.GracefulStop()
				return
			case <-ticker.C:
				g.checkAndCleanSessionHeartbeat()
			}
		}
	}()

	return nil
}

func (g *grpcServer) getSession(sessionId string) *SessionStateHeartbeat {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if s, found := g.stateHeartbeats[sessionId]; found {
		return s
	}
	return nil
}

func (g *grpcServer) getSessions() []*SessionStateHeartbeat {
	g.mu.RLock()
	defer g.mu.RUnlock()
	sessions := []*SessionStateHeartbeat{}
	for _, s := range g.stateHeartbeats {
		sessions = append(sessions, s)
	}
	return sessions
}

func (g *grpcServer) checkAndCleanSessionHeartbeat() {
	deadSession := []string{}
	sessions := g.getSessions()
	for _, s := range sessions {
		lastSeen := s.lastSeen
		heartbeatLastSeenCutoff := lastSeen.Add(DefaultSessionHeartBeatDuration)
		if s.consectuivePingFailures > DefaultDeadSessionHeartbeatMissingThreshold {
			// this session is considered as dead since it's not seen in past 3 heartbeat duration
			g.forwardSessionState(s.podId, s.sessionId, internal.SessionState{
				SessionId:      s.sessionId,
				SessionState:   types.SessionStateNoHeartbeat,
				ClientSessions: []types.ClientSession{},
			})
			deadSession = append(deadSession, s.sessionId)
			continue
		}
		if time.Now().After(heartbeatLastSeenCutoff) {
			// did not receive heartbeat of this session in past heartbeat duration, ping it
			s.consectuivePingFailures += 1
			g.PingSession(s.podId, s.sessionId, s.stateCallback)
		}
	}

	if len(deadSession) > 0 {
		g.mu.Lock()
		defer g.mu.Unlock()
		for _, v := range deadSession {
			delete(g.stateHeartbeats, v)
		}
	}
}

// pod use get message to maitain a stream connection with session service to receive session command messages
// only one connection is allowed from one pod, method return until pod disconnect or send message failed via this stream connection
func (g *grpcServer) GetMessage(identifier *session_grpc.PodIdentifier, server session_grpc.SessionService_GetMessageServer) error {
	var messageSeq int64 = 0
	klog.InfoS("Received GetMessage stream connection from pod", "pod", identifier)
	ch := make(chan *session_grpc.SessionMessage, 10)
	if err := g.enlistPod(identifier.GetPodId(), ch); err != nil {
		close(ch)
		return fmt.Errorf("Only one GetMessage stream connection is allowed from one pod")
	}

	chDone := server.Context().Done()
	for {
		select {
		case <-chDone:
			// stream connection broken
			close(ch)
			g.delistPod(identifier.GetPodId())
			return nil
		case msg := <-ch:
			messageSeq += 1
			seq := fmt.Sprintf("%d", messageSeq)
			msg.MessageIdentifier = seq
			klog.InfoS("Send a messge to pod", "pod", identifier, "message", msg.GetMessageType())
			if err := server.Send(msg); err != nil {
				klog.ErrorS(err, "Failed to send message via GetMessage stream", "pod", identifier.GetPodId())
				close(ch)
				g.delistPod(identifier.GetPodId())
				return err
			}
		}
	}
}

func (g *grpcServer) PutMessage(ctx context.Context, message *session_grpc.SessionMessage) (*empty.Empty, error) {
	var err error
	switch message.GetMessageType() {
	case session_grpc.MessageType_SESSION_STATE:
		msg := internal.SessionState{
			SessionId:      message.GetSessionIdentifier().GetIdentifier(),
			ClientSessions: []types.ClientSession{},
		}
		status := message.GetSessionStatus()
		sessionId := message.GetSessionIdentifier().GetIdentifier()
		podId := message.GetSessionIdentifier().GetPodId()
		switch status.GetSessionState() {
		case session_grpc.SessionState_STATE_CLOSED:
			msg.SessionState = types.SessionStateClosed
		case session_grpc.SessionState_STATE_OPEN:
			msg.SessionState = types.SessionStateReady
		case session_grpc.SessionState_STATE_CLOSING:
			msg.SessionState = types.SessionStateReady
		case session_grpc.SessionState_STATE_INITIALIZING:
			msg.SessionState = types.SessionStateStarting
		}
		g.forwardSessionState(podId, sessionId, msg)
		if msg.SessionState == types.SessionStateClosed {
			g.removeClosedSession(sessionId)
		}
	default:
		klog.Errorf(fmt.Sprintf("not supported message type %s, message %v", message.GetMessageType(), message))
	}

	return &emptypb.Empty{}, err
}

// CloseSession dispatch a SessionClose event to pod
func (g *grpcServer) CloseSession(podId, sessionId string, graceSeconds uint16) error {
	session := g.getSession(sessionId)
	if session == nil {
		return sessionservice.SessionNotFound
	}

	gs := int64(graceSeconds)
	// use saved pod id
	podId = session.podId
	messageType := session_grpc.MessageType_CLOSE_SESSION
	body := session_grpc.SessionMessage_CloseSession{
		CloseSession: &session_grpc.CloseSession{
			GracePeriodSeconds: gs,
		},
	}
	m := &session_grpc.SessionMessage{
		MessageIdentifier: "",
		SessionIdentifier: &session_grpc.SessionIdentifier{
			PodId:      podId,
			Identifier: sessionId,
		},
		MessageType: messageType,
		MessageBody: &body,
	}

	err := g.sendGrpcMessage(podId, m)
	if err != nil {
		klog.ErrorS(err, "Failed to dispatch close session message to pod", "pod", podId, "session", sessionId)
		return err
	}
	return nil
}

// OpenSession dispatch a SessionOpen event to pod
func (g *grpcServer) OpenSession(podId, sessionId string, sessionData string, stateCallbackFunc func(internal.SessionState)) error {
	heartbeat := g.getSession(sessionId)
	if heartbeat != nil {
		return sessionservice.SessionAlreadyExist
	}
	heartbeat = g.createHeartBeat(podId, sessionId, stateCallbackFunc)
	messageType := session_grpc.MessageType_OPEN_SESSION
	body := session_grpc.SessionMessage_OpenSession{
		OpenSession: &session_grpc.OpenSession{
			SessionConfiguration: &session_grpc.SessionConfiguration{
				SessionData: []byte(sessionData),
			},
		},
	}
	m := &session_grpc.SessionMessage{
		MessageType: messageType,
		MessageBody: &body,
		SessionIdentifier: &session_grpc.SessionIdentifier{
			PodId:      podId,
			Identifier: sessionId,
		},
	}

	err := g.sendGrpcMessage(podId, m)
	if err != nil {
		klog.ErrorS(err, "Failed to dispatch open session message to pod", "pod", podId, "session", sessionId)
		return err
	}
	return nil
}

// PingSession send ping message to pod/session, and create heartbeat to get session state callback
func (g *grpcServer) PingSession(podId, sessionId string, stateCallbackFunc func(internal.SessionState)) error {
	heartbeat := g.getSession(sessionId)
	if heartbeat == nil {
		// ideally it should have been saved, when node agent crash, data lost, session actor will reping to recreate state callback func
		heartbeat = g.createHeartBeat(podId, sessionId, stateCallbackFunc)
	}

	// saved pod id should be same as passed in podId, but still use saved one
	podId = heartbeat.podId
	messageType := session_grpc.MessageType_PING_SESSION
	body := session_grpc.SessionMessage_PingSession{
		PingSession: &session_grpc.PingSession{},
	}
	m := &session_grpc.SessionMessage{
		SessionIdentifier: &session_grpc.SessionIdentifier{
			PodId:      podId,
			Identifier: sessionId,
		},
		MessageType: messageType,
		MessageBody: &body,
	}

	return g.sendGrpcMessage(podId, m)
}

func (g *grpcServer) sendGrpcMessage(podId string, msg *session_grpc.SessionMessage) error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if client, found := g.getMessageClients[podId]; found {
		client.channel <- msg
	} else {
		return sessionservice.SessionStreamDisconnected
	}
	return nil
}

// forwardSessionState forward SessionState sent by container to node agent via registered sessionStateCallback func
func (g *grpcServer) forwardSessionState(podId, sessionId string, sessionState internal.SessionState) error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if stateHeartbeat, found := g.stateHeartbeats[sessionId]; found {
		stateHeartbeat.lastSeen = time.Now()
		stateHeartbeat.consectuivePingFailures = 0
		stateHeartbeat.stateCallback(sessionState)
	} else {
		// open session should be found, but when session server restart, it lost state callback,
		// since no state callback to forward, wait for node agent ping this session
	}
	return nil
}

func (g *grpcServer) removeClosedSession(sessionId string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.stateHeartbeats, sessionId)
}

func (g *grpcServer) createHeartBeat(podId, sessionId string, stateCallbackFunc func(internal.SessionState)) *SessionStateHeartbeat {
	g.mu.Lock()
	defer g.mu.Unlock()
	heartbeat := &SessionStateHeartbeat{
		stateCallback:           stateCallbackFunc,
		podId:                   podId,
		sessionId:               sessionId,
		consectuivePingFailures: 0,
		lastSeen:                time.Now(),
	}
	g.stateHeartbeats[sessionId] = heartbeat
	return heartbeat
}
func (g *grpcServer) enlistPod(pod string, ch chan<- *session_grpc.SessionMessage) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.getMessageClients[pod]; ok {
		return sessionservice.SessionStreamAlreadyEstablished
	} else {
		g.getMessageClients[pod] = &GetSessionMessageClient{
			podId:   pod,
			channel: ch,
		}
	}
	return nil
}

// delistPod is called when pod disconnect it from session service,
// in some cases, pod could disconnect and reconnect soon,
// it check session heartbeats and remove this pod if all sessions are timeout
func (g *grpcServer) delistPod(pod string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.getMessageClients, pod)
}

func NewSessionService() *grpcServer {
	return &grpcServer{
		mu:                                sync.RWMutex{},
		stateHeartbeats:                   map[string]*SessionStateHeartbeat{},
		getMessageClients:                 map[string]*GetSessionMessageClient{},
		UnimplementedSessionServiceServer: session_grpc.UnimplementedSessionServiceServer{},
	}
}
