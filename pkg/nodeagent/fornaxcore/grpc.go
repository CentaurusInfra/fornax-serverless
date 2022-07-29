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

package fornaxcore

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	fornax "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/retry"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"k8s.io/klog/v2"
)

type FornaxCoreConfiguration struct {
	endpoint       string
	connTimeout    time.Duration
	callTimeout    time.Duration
	maxRecvMsgSize int
}

const (
	DefaultConnTimeout    = 5 * time.Second
	DefaultCallTimeout    = 5 * time.Second
	DefaultMaxRecvMsgSize = 16 * 1024
)

func NewFornaxCoreConfiguration(endpoint string) *FornaxCoreConfiguration {
	return &FornaxCoreConfiguration{
		endpoint:       endpoint,
		connTimeout:    DefaultConnTimeout,
		callTimeout:    DefaultCallTimeout,
		maxRecvMsgSize: DefaultMaxRecvMsgSize,
	}
}

type FornaxCoreClient interface {
	Start()
	Stop()
	PutMessage(message *fornax.FornaxCoreMessage) error
	GetMessage(receiver string, channel chan *fornax.FornaxCoreMessage) error
}

type fornaxCoreClient struct {
	mu               sync.Mutex
	identifier       *fornax.NodeIdentifier
	done             bool
	config           *FornaxCoreConfiguration
	conn             *grpc.ClientConn
	service          fornax.FornaxCoreServiceClient
	getMessageClient fornax.FornaxCoreService_GetMessageClient
	receivers        map[string]chan *fornax.FornaxCoreMessage
}

// GetMessage implements FornaxCore
func (f *fornaxCoreClient) GetMessage(receiver string, channel chan *fornax.FornaxCoreMessage) error {
	f.receivers[receiver] = channel
	return nil
}

// PutMessage implements FornaxCore
func (f *fornaxCoreClient) PutMessage(message *fornax.FornaxCoreMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	klog.InfoS("Send a message to FornaxCore", "endpoint", f.config.endpoint, "msgType", message.GetMessageType())
	if f.service == nil {
		return errors.New("FornaxCore connection is not initialized yet")
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.config.callTimeout)
	defer cancel()
	opts := grpc.EmptyCallOption{}
	_, err := f.service.PutMessage(ctx, message, opts)
	if err != nil {
		klog.ErrorS(err, "Failed to send message to fornax core", "endpoint", f.config.endpoint)
		return err
	}
	return nil
}

func (f *fornaxCoreClient) disconnect() error {
	return f.conn.Close()
}

func (f *fornaxCoreClient) connect() error {
	connect := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), f.config.connTimeout)
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
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(f.config.maxRecvMsgSize)),
			grpc.WithStreamInterceptor(grpc_retry.StreamClientInterceptor(opts...)),
			grpc.WithUnaryInterceptor(grpc_retry.UnaryClientInterceptor(opts...)),
		)
		if err != nil {
			klog.ErrorS(err, "Connect fornaxCore failed", "endpoint", f.config.endpoint)
			return err
		}

		f.conn = conn
		f.service = fornax.NewFornaxCoreServiceClient(conn)
		return nil
	}

	err := retry.BackoffExec(2*time.Second, 1*time.Minute, 3*time.Minute, 1.7, connect)
	return err
}

func (f *fornaxCoreClient) initGetMessageClient(ctx context.Context, identifier *fornax.NodeIdentifier) error {
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
func (f *fornaxCoreClient) recvMessage() {
	klog.InfoS("Receiving message from FornaxCore", "endpoint", f.config.endpoint)
	for {
		if f.done {
			break
		}

		ctx := context.Background()
		if f.getMessageClient == nil {
			err := f.initGetMessageClient(ctx, f.identifier)
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
		panicReceivers := []string{}
		for n, v := range f.receivers {
			func() {
				defer func() {
					if err := recover(); err != nil {
						klog.Errorf("Send message panic occurred: %v", err)
						// remember it and remove closed channel after loop
						panicReceivers = append(panicReceivers, n)
					}
				}()
				v <- msg
			}()
		}

		for _, n := range panicReceivers {
			delete(f.receivers, n)
		}
	}
}

// Stop implements FornaxCore
func (f *fornaxCoreClient) Stop() {
	f.done = true
	f.disconnect()
}

// Start implements FornaxCore
func (f *fornaxCoreClient) Start() {
	go f.recvMessage()
}

var _ FornaxCoreClient = &fornaxCoreClient{}

func NewFornaxCoreClient(identifier *fornax.NodeIdentifier, config *FornaxCoreConfiguration) *fornaxCoreClient {
	f := &fornaxCoreClient{
		mu:               sync.Mutex{},
		identifier:       identifier,
		done:             false,
		config:           config,
		conn:             nil,
		service:          nil,
		getMessageClient: nil,
		receivers:        map[string]chan *fornax.FornaxCoreMessage{},
	}
	return f
}
