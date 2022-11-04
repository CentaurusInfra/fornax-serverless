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

package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/sessionservice"
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

type sessionServer interface {
	sessionservice.SessionService
	SessionServiceServer
}

type GetSessionMessageClient struct {
	podId   string
	channel chan<- *SessionMessage
}

type SessionStateHeartbeat struct {
	stateCallback           func(internal.SessionState)
	pod                     *types.FornaxPod
	session                 *types.FornaxSession
	consectuivePingFailures uint16
	lastSeen                time.Time
}

var _ sessionServer = &GrpcSessionService{}

type GrpcSessionService struct {
	mu sync.RWMutex
	// session state callback and heartbeat map by session id
	sessionHeartbeats map[string]*SessionStateHeartbeat

	// pod's get message connection by pod id
	sessionClients map[string]*GetSessionMessageClient

	UnimplementedSessionServiceServer
}

// mustEmbedUnimplementedSessionServiceServer implements SessionServer
func (*GrpcSessionService) mustEmbedUnimplementedSessionServiceServer() {
	panic("unimplemented")
}

func (g *GrpcSessionService) Run(ctx context.Context, port int32) error {
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		klog.ErrorS(err, "Node agent session grpc server failed to listen", "port", port)
		return err
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	RegisterSessionServiceServer(grpcServer, g)
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

func (g *GrpcSessionService) getSessionClient(podId string) *GetSessionMessageClient {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if s, found := g.sessionClients[podId]; found {
		return s
	}
	return nil
}

func (g *GrpcSessionService) getSessionHeartbeat(sessionId string) *SessionStateHeartbeat {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if s, found := g.sessionHeartbeats[sessionId]; found {
		return s
	}
	return nil
}

func (g *GrpcSessionService) getSessions() []*SessionStateHeartbeat {
	g.mu.RLock()
	defer g.mu.RUnlock()
	sessions := []*SessionStateHeartbeat{}
	for _, s := range g.sessionHeartbeats {
		sessions = append(sessions, s)
	}
	return sessions
}

func (g *GrpcSessionService) checkAndCleanSessionHeartbeat() {
	deadSession := []string{}
	sessions := g.getSessions()
	for _, s := range sessions {
		lastSeen := s.lastSeen
		heartbeatLastSeenCutoff := lastSeen.Add(DefaultSessionHeartBeatDuration)
		if s.consectuivePingFailures > DefaultDeadSessionHeartbeatMissingThreshold {
			// this session is considered as dead since it's not seen in past 3 heartbeat duration
			g.forwardSessionStateToPod(s.session.Identifier, internal.SessionState{
				SessionId:      s.session.Identifier,
				SessionState:   types.SessionStateNoHeartbeat,
				ClientSessions: []types.ClientSession{},
			})
			deadSession = append(deadSession, s.session.Identifier)
			continue
		}
		if time.Now().After(heartbeatLastSeenCutoff) {
			// did not receive heartbeat of this session in past heartbeat duration, ping it
			s.consectuivePingFailures += 1
			g.PingSession(s.pod, s.session, s.stateCallback)
		}
	}

	if len(deadSession) > 0 {
		g.mu.Lock()
		defer g.mu.Unlock()
		for _, v := range deadSession {
			delete(g.sessionHeartbeats, v)
		}
	}
}

// pod use get message to maitain a stream connection with session service to receive session command messages
// only one connection is allowed from one pod, method return until pod disconnect or send message failed via this stream connection
func (g *GrpcSessionService) GetMessage(identifier *PodIdentifier, server SessionService_GetMessageServer) error {
	var messageSeq int64 = 0
	klog.InfoS("Received GetMessage stream connection from pod", "pod", identifier)
	ch := make(chan *SessionMessage, 10)
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
			if err := server.Send(msg); err != nil {
				klog.ErrorS(err, "Failed to send message via GetMessage stream", "pod", identifier.GetPodId())
				close(ch)
				g.delistPod(identifier.GetPodId())
				return err
			}
		}
	}
}

func (g *GrpcSessionService) PutMessage(ctx context.Context, message *SessionMessage) (*empty.Empty, error) {
	var err error
	switch message.GetMessageType() {
	case MessageType_SESSION_STATE:
		msg := internal.SessionState{
			SessionId:      message.GetSessionIdentifier().GetIdentifier(),
			ClientSessions: []types.ClientSession{},
		}
		status := message.GetSessionStatus()
		sessionId := message.GetSessionIdentifier().GetIdentifier()
		switch status.GetSessionState() {
		case SessionState_STATE_CLOSED:
			msg.SessionState = types.SessionStateClosed
		case SessionState_STATE_OPEN:
			msg.SessionState = types.SessionStateReady
		case SessionState_STATE_CLOSING:
			msg.SessionState = types.SessionStateReady
		case SessionState_STATE_INITIALIZING:
			msg.SessionState = types.SessionStateStarting
		}
		g.forwardSessionStateToPod(sessionId, msg)
		if msg.SessionState == types.SessionStateClosed {
			g.removeClosedSession(sessionId)
		}
	default:
		klog.Errorf(fmt.Sprintf("not supported message type %s, message %v", message.GetMessageType(), message))
	}

	return &emptypb.Empty{}, err
}

// CloseSession dispatch a SessionClose event to pod
func (g *GrpcSessionService) CloseSession(pod *types.FornaxPod, session *types.FornaxSession, gracePeriodSeconds uint16) error {
	podId := pod.Identifier
	sessionId := session.Identifier
	sessHeartbeat := g.getSessionHeartbeat(sessionId)
	if sessHeartbeat == nil {
		return sessionservice.SessionNotFound
	}

	gs := int64(gracePeriodSeconds)
	// use saved pod id
	messageType := MessageType_CLOSE_SESSION
	body := SessionMessage_CloseSession{
		CloseSession: &CloseSession{
			GracePeriodSeconds: gs,
		},
	}
	m := &SessionMessage{
		MessageIdentifier: "",
		SessionIdentifier: &SessionIdentifier{
			PodId:      podId,
			Identifier: sessionId,
		},
		MessageType: messageType,
		MessageBody: &body,
	}

	err := g.sendGrpcMessageToPod(podId, m)
	if err != nil {
		klog.ErrorS(err, "Failed to dispatch close session message to pod", "pod", podId, "session", sessionId)
		return err
	}
	return nil
}

// OpenSession dispatch a SessionOpen event to pod
func (g *GrpcSessionService) OpenSession(pod *types.FornaxPod, session *types.FornaxSession, stateCallbackFunc func(internal.SessionState)) error {
	podId := pod.Identifier
	sessionId := session.Identifier
	sessionData := session.Session.Spec.SessionData
	heartbeat := g.getSessionHeartbeat(sessionId)
	if heartbeat != nil {
		return sessionservice.SessionAlreadyExist
	}
	heartbeat = g.createHeartBeat(pod, session, stateCallbackFunc)
	messageType := MessageType_OPEN_SESSION
	body := SessionMessage_OpenSession{
		OpenSession: &OpenSession{
			SessionConfiguration: &SessionConfiguration{
				SessionData: []byte(sessionData),
			},
		},
	}
	m := &SessionMessage{
		MessageType: messageType,
		MessageBody: &body,
		SessionIdentifier: &SessionIdentifier{
			PodId:      podId,
			Identifier: sessionId,
		},
	}

	err := g.sendGrpcMessageToPod(podId, m)
	if err != nil {
		klog.ErrorS(err, "Failed to dispatch open session message to pod", "pod", podId, "session", sessionId)
		return err
	}
	return nil
}

// PingSession send ping message to pod/session, and create heartbeat to get session state callback
func (g *GrpcSessionService) PingSession(pod *types.FornaxPod, session *types.FornaxSession, stateCallbackFunc func(internal.SessionState)) error {
	podId := pod.Identifier
	sessionId := session.Identifier
	heartbeat := g.getSessionHeartbeat(sessionId)
	if heartbeat == nil {
		// ideally it should have been saved, when node agent crash, data lost, session actor will reping to recreate state callback func
		heartbeat = g.createHeartBeat(pod, session, stateCallbackFunc)
	}

	messageType := MessageType_PING_SESSION
	body := SessionMessage_PingSession{
		PingSession: &PingSession{},
	}
	m := &SessionMessage{
		SessionIdentifier: &SessionIdentifier{
			PodId:      podId,
			Identifier: sessionId,
		},
		MessageType: messageType,
		MessageBody: &body,
	}

	return g.sendGrpcMessageToPod(podId, m)
}

func (g *GrpcSessionService) sendGrpcMessageToPod(podId string, msg *SessionMessage) error {
	if client := g.getSessionClient(podId); client != nil {
		client.channel <- msg
	} else {
		return sessionservice.SessionStreamDisconnected
	}
	return nil
}

// forwardSessionStateToPod forward SessionState sent by container to node agent via registered sessionStateCallback func
func (g *GrpcSessionService) forwardSessionStateToPod(sessionId string, sessionState internal.SessionState) error {
	if stateHeartbeat := g.getSessionHeartbeat(sessionId); stateHeartbeat != nil {
		stateHeartbeat.lastSeen = time.Now()
		stateHeartbeat.consectuivePingFailures = 0
		stateHeartbeat.stateCallback(sessionState)
	} else {
		// open session should be found, but when session server restart, it lost state callback,
		// since no state callback to forward, wait for node agent ping this session
	}
	return nil
}

func (g *GrpcSessionService) removeClosedSession(sessionId string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.sessionHeartbeats, sessionId)
}

func (g *GrpcSessionService) createHeartBeat(pod *types.FornaxPod, session *types.FornaxSession, stateCallbackFunc func(internal.SessionState)) *SessionStateHeartbeat {
	g.mu.Lock()
	defer g.mu.Unlock()
	heartbeat := &SessionStateHeartbeat{
		stateCallback:           stateCallbackFunc,
		pod:                     pod,
		session:                 session,
		consectuivePingFailures: 0,
		lastSeen:                time.Now(),
	}
	g.sessionHeartbeats[session.Identifier] = heartbeat
	return heartbeat
}
func (g *GrpcSessionService) enlistPod(pod string, ch chan<- *SessionMessage) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.sessionClients[pod]; ok {
		return sessionservice.SessionStreamAlreadyEstablished
	} else {
		g.sessionClients[pod] = &GetSessionMessageClient{
			podId:   pod,
			channel: ch,
		}
	}
	return nil
}

// delistPod is called when pod disconnect it from session service,
// in some cases, pod could disconnect and reconnect soon,
// it check session heartbeats and remove this pod if all sessions are timeout
func (g *GrpcSessionService) delistPod(pod string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.sessionClients, pod)
}

func NewSessionService() *GrpcSessionService {
	return &GrpcSessionService{
		mu:                                sync.RWMutex{},
		sessionHeartbeats:                 map[string]*SessionStateHeartbeat{},
		sessionClients:                    map[string]*GetSessionMessageClient{},
		UnimplementedSessionServiceServer: UnimplementedSessionServiceServer{},
	}
}
