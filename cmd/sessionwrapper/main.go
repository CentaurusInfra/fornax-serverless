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

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	sessiongrpc "centaurusinfra.io/fornax-serverless/pkg/nodeagent/sessionservice/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"k8s.io/klog/v2"
)

func main() {
	endpoint := os.Getenv(fornaxv1.LabelFornaxCoreSessionService)
	opensession_cmd := os.Getenv("SESSION_WRAPPER_OPEN_SESSION_CMD")

	config := &SessionConfig{
		endpoint: fmt.Sprintf("%s:%d", endpoint, 1022),
		openCmd:  opensession_cmd,
	}

	instanceId := os.Getenv(fornaxv1.LabelFornaxCorePod)
	client := NewSessionServiceClient(instanceId, config)
	sigCh := make(chan os.Signal, 1)
	done := make(chan int, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	signal.Notify(sigCh, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		switch sig {
		case syscall.SIGINT:
			done <- 1
		case syscall.SIGTERM:
			done <- 2
		}
	}()
	client.Start(context.Background())
	// capture signal send to session wrapper, and gracefully stopping all open sessions
	result := <-done
	client.GracefullyStopSessions()
	fmt.Printf("exiting %d\n", result)
	os.Exit(result)
}

const (
	DefaultClosingGracePeriodDuration = 60 * time.Second
	DefaultHeartbeatDuration          = 10 * time.Second
	DefaultConnTimeout                = 5 * time.Second
	DefaultCallTimeout                = 5 * time.Second
	DefaultMaxRecvMsgSize             = 16 * 1024
)

type SessionConfig struct {
	endpoint string
	openCmd  string
}

type Session struct {
	id                         string
	pid                        int
	podId                      string
	state                      sessiongrpc.SessionState
	closingGracePeriodDuration time.Duration
	closingTimestamp           *time.Time
	clients                    []*sessiongrpc.ClientSession
}

type sessionServiceClient struct {
	messageId        int64
	stopping         bool
	done             chan bool
	config           *SessionConfig
	identifier       string
	conn             *grpc.ClientConn
	service          sessiongrpc.SessionServiceClient
	getMessageClient sessiongrpc.SessionService_GetMessageClient
	sessions         map[string]*Session
}

func (f *sessionServiceClient) PutMessage(message *sessiongrpc.SessionMessage) error {
	klog.InfoS("Send a message to FornaxCore", "endpoint", f.config.endpoint, "msgType", message.GetMessageType())
	if f.service == nil {
		return errors.New("FornaxCore connection is not initialized yet")
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultCallTimeout)
	defer cancel()
	opts := grpc.EmptyCallOption{}
	_, err := f.service.PutMessage(ctx, message, opts)
	if err != nil {
		klog.ErrorS(err, "Failed to send message to fornax core", "endpoint", f.config.endpoint)
		return err
	}
	return nil
}

func (f *sessionServiceClient) disconnect() error {
	return f.conn.Close()
}

func (f *sessionServiceClient) connect() error {
	connect := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultConnTimeout)
		defer cancel()

		opts := []grpc_retry.CallOption{
			grpc_retry.WithBackoff(grpc_retry.BackoffLinear(100 * time.Millisecond)),
			grpc_retry.WithCodes(codes.NotFound, codes.Aborted, codes.Unavailable, codes.DataLoss, codes.Unknown),
		}
		conn, err := grpc.DialContext(
			ctx,
			f.config.endpoint,
			grpc.WithBlock(),
			grpc.WithInsecure(),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(DefaultMaxRecvMsgSize)),
			grpc.WithStreamInterceptor(grpc_retry.StreamClientInterceptor(opts...)),
			grpc.WithUnaryInterceptor(grpc_retry.UnaryClientInterceptor(opts...)),
		)
		if err != nil {
			klog.ErrorS(err, "Connect fornaxCore failed", "endpoint", f.config.endpoint)
			return err
		}

		f.conn = conn
		f.service = sessiongrpc.NewSessionServiceClient(conn)
		return nil
	}

	err := util.BackoffExec(2*time.Second, 1*time.Minute, 3*time.Minute, 1.7, connect)
	return err
}

func (f *sessionServiceClient) initGetMessageClient(ctx context.Context, identifier *sessiongrpc.PodIdentifier) error {
	if f.conn == nil {
		klog.InfoS("Connecting to FornaxCore", "endpoint", f.config.endpoint)
		err := f.connect()
		if err != nil {
			return err
		}
	}
	klog.InfoS("Init fornax core get message client", "endpoint", f.config.endpoint)
	gclient, err := f.service.GetMessage(ctx, identifier)
	if err != nil {
		return err
	}
	f.getMessageClient = gclient
	return nil
}

// should exec in a go routine, fornaxCoreClient recvMessage loop forever until it's old stop
// it receive message and dispatch it to receivers' channel registered by GetMessage
func (f *sessionServiceClient) recvMessage(ctx context.Context) {
	klog.InfoS("Receiving message from FornaxCore", "endpoint", f.config.endpoint)
	for {
		if f.getMessageClient == nil {
			err := f.initGetMessageClient(ctx, &sessiongrpc.PodIdentifier{
				PodId: f.identifier,
			})
			if err != nil {
				klog.ErrorS(err, "Failed to init fornax core get message client", "endpoint", f.config.endpoint)
				time.Sleep(2 * time.Second)
				continue
			}
		}

		msg, err := f.getMessageClient.Recv()
		if err == io.EOF {
			klog.ErrorS(err, "FornaxCore closed stream at server side, reset to get a new stream client")
			f.getMessageClient = nil
			continue
		}

		if err != nil {
			klog.ErrorS(err, "Receive message failed with unexpected error, reset to get a new stream client")
			if err != nil {
				f.getMessageClient = nil
				continue
			}
		}

		klog.InfoS("Received a message from FornaxCore", "fornax", f.config.endpoint, "msgType", msg.GetMessageType())
		err = f.handleSessionCommand(msg)
		if err != nil {
			klog.ErrorS(err, "Failed to handle session")
		}
	}
}

// Stop implements FornaxCore
func (f *sessionServiceClient) handleSessionCommand(msg *sessiongrpc.SessionMessage) error {
	sessionId := msg.GetSessionIdentifier()
	switch msg.GetMessageType() {
	case sessiongrpc.MessageType_PING_SESSION:
		if f.stopping {
			return errors.New("instance is terminating")
		}
		session, found := f.sessions[sessionId.GetIdentifier()]
		if !found {
			klog.InfoS("Session NotFound", "sessionId", sessionId)
			return nil
		}
		if session.pid != 0 && session.state != sessiongrpc.SessionState_STATE_CLOSED {
			if _, err := os.FindProcess(session.pid); err != nil {
				klog.InfoS("Session process NotFound", "sessionId", sessionId, "pid", session.pid)
				session.state = sessiongrpc.SessionState_STATE_CLOSED
				err = f.sendHeartbeat(session)
				// kill again, make sure it's dead dead
				syscall.Kill(session.pid, syscall.SIGKILL)
				return err
			}
		} else {
			// TODO, is session process really working, how to get client session
			return f.sendHeartbeat(session)
		}
	case sessiongrpc.MessageType_CLOSE_SESSION:
		session, found := f.sessions[sessionId.GetIdentifier()]
		if !found {
			klog.InfoS("Session NotFound", "sessionId", sessionId)
			return nil
		}
		if session.state == sessiongrpc.SessionState_STATE_CLOSED {
			klog.InfoS("Session Already closed", "sessionId", sessionId)
			return nil
		}
		closeSession := msg.GetCloseSession()
		session.state = sessiongrpc.SessionState_STATE_CLOSING
		session.closingGracePeriodDuration = time.Duration(closeSession.GracePeriodSeconds) * time.Second
		now := time.Now()
		session.closingTimestamp = &now
		if proc, err := os.FindProcess(session.pid); err != nil {
			return nil
		} else {
			err := proc.Signal(syscall.SIGTERM)
			if err != nil {
				errno, ok := err.(syscall.Errno)
				if !ok {
					return err
				}
				switch errno {
				case syscall.ESRCH:
					// now such process, treat it a closed
					session.state = sessiongrpc.SessionState_STATE_CLOSED
					return nil
				case syscall.EPERM:
					return nil
				default:
					return err
				}
			}
			return nil
		}
	case sessiongrpc.MessageType_OPEN_SESSION:
		if f.stopping {
			return errors.New("instance is terminating")
		}
		session := &Session{
			id:                         sessionId.GetIdentifier(),
			pid:                        0,
			podId:                      sessionId.GetPodId(),
			state:                      sessiongrpc.SessionState_STATE_INITIALIZING,
			closingGracePeriodDuration: DefaultClosingGracePeriodDuration,
			closingTimestamp:           nil,
			clients:                    []*sessiongrpc.ClientSession{},
		}

		cmd := f.config.openCmd
		procAttr := os.ProcAttr{}
		procAttr.Files = []*os.File{os.Stdin, os.Stdout, os.Stderr}
		proc, err := os.StartProcess(cmd, []string{}, &procAttr)
		if err != nil {
			session.state = sessiongrpc.SessionState_STATE_CLOSED
			klog.ErrorS(err, "Failed to start session process")
		} else {
			session.pid = proc.Pid
			session.state = sessiongrpc.SessionState_STATE_OPEN
			go func() {
				// wait for session process exit, session is closed or process exit itself
				s, _ := proc.Wait()
				klog.InfoS("Session process exit", "code", s.ExitCode())
				session.state = sessiongrpc.SessionState_STATE_CLOSED
			}()
		}
		f.sessions[sessionId.GetIdentifier()] = session
		err = f.sendHeartbeat(session)
		return err
	default:
	}

	return nil
}

func (f *sessionServiceClient) sendHeartbeat(session *Session) error {
	msgId := fmt.Sprintf("%d", f.messageId)
	msgType := sessiongrpc.MessageType_SESSION_STATE
	sessionState := session.state
	msgBody := &sessiongrpc.SessionMessage_SessionStatus{
		SessionStatus: &sessiongrpc.SessionStatus{
			SessionState:  sessionState,
			ClientSession: []*sessiongrpc.ClientSession{},
		},
	}
	msg := &sessiongrpc.SessionMessage{
		MessageIdentifier: msgId,
		SessionIdentifier: &sessiongrpc.SessionIdentifier{
			PodId:      session.podId,
			Identifier: session.id,
		},
		MessageType: msgType,
		MessageBody: msgBody,
	}

	klog.InfoS("Session heartbeat", "session", session.id, "state", sessionState)
	return f.PutMessage(msg)
}

// send session heart beat, and send sig_kill to closing sessions if they exceed closing gracefulPeriodDuration
// when system is stopping, check if all sessions are closed, and send done signal
func (f *sessionServiceClient) houseKeeping() {
	klog.InfoS("Session house keeping")
	for _, v := range f.sessions {
		if v.state == sessiongrpc.SessionState_STATE_CLOSING {
			timeCutoff := time.Now().Add(-1 * v.closingGracePeriodDuration)
			if v.closingTimestamp.Before(timeCutoff) {
				syscall.Kill(v.pid, syscall.SIGKILL)
			}
		}
		f.sendHeartbeat(v)
	}

	if f.stopping {
		allSessionClosed := true
		for _, v := range f.sessions {
			if v.state != sessiongrpc.SessionState_STATE_CLOSED {
				allSessionClosed = false
				break
			}
		}
		if allSessionClosed {
			f.done <- true
		}
	}
}

// close all open sessions using sig_term and wait for all session are closed
func (f *sessionServiceClient) GracefullyStopSessions() {
	klog.InfoS("Stopping session wrapper", "endpoint", f.config.endpoint)
	f.stopping = true
	for _, v := range f.sessions {
		if v.pid != 0 && v.state != sessiongrpc.SessionState_STATE_CLOSED {
			v.state = sessiongrpc.SessionState_STATE_CLOSING
			now := time.Now()
			v.closingTimestamp = &now
			syscall.Kill(v.pid, syscall.SIGTERM)
		}
	}
	<-f.done
}

// Start implements FornaxCore
func (f *sessionServiceClient) Start(ctx context.Context) {
	klog.InfoS("Starting session wrapper", "endpoint", f.config.endpoint)
	go f.recvMessage(ctx)
	go func() {
		ticker := time.NewTicker(DefaultHeartbeatDuration)
		for {
			select {
			case <-ticker.C:
				f.houseKeeping()
			}
		}
	}()
}

func NewSessionServiceClient(identifier string, config *SessionConfig) *sessionServiceClient {
	f := &sessionServiceClient{
		messageId:        0,
		stopping:         false,
		done:             make(chan bool),
		config:           config,
		identifier:       identifier,
		conn:             nil,
		service:          nil,
		getMessageClient: nil,
		sessions:         map[string]*Session{},
	}
	return f
}
