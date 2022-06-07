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
	"fmt"
	"io"
	"time"

	fornax "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/retry"
	"github.com/golang/protobuf/ptypes/empty"
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
	DefaultMaxRecvMsgSize = 5
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
	Start() error
	Stop() error
	PutMessage(message *fornax.FornaxCoreMessage) error
	GetMessage(receiver string, channel chan *fornax.FornaxCoreMessage) error
}

type fornaxCoreClient struct {
	done             bool
	config           FornaxCoreConfiguration
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
	if f.service == nil {
		return fmt.Errorf("fornax core %s is not initialized yet", f.config.endpoint)
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.config.callTimeout)
	defer cancel()
	opts := grpc.EmptyCallOption{}
	_, err := f.service.PutMessage(ctx, message, opts)
	if err != nil {
		klog.ErrorS(err, "failed to send message to fornax core", "endpoint", f.config.endpoint)
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
			klog.Errorf("Connect fornaxCore failed, address %v, %v", f.config.endpoint, err)
			return err
		}

		f.conn = conn
		f.service = fornax.NewFornaxCoreServiceClient(conn)
		return nil
	}

	err := retry.BackoffExec(2*time.Second, 1*time.Minute, 3*time.Minute, 1.7, connect)
	return err
}

func (f *fornaxCoreClient) initGetMessageClient() error {
	if f.conn == nil {
		err := f.connect()
		if err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), f.config.callTimeout)
	defer cancel()
	gclient, err := f.service.GetMessage(ctx, &empty.Empty{})
	if err != nil {
		return err
	}
	f.getMessageClient = gclient
	return nil
}

// should exec in a go routine, fornaxCoreClient recvMessage loop forever until it's old stop
// it receive message and dispatch it to receivers' channel registered by GetMessage
func (f *fornaxCoreClient) recvMessage() {
	for {
		if f.done {
			break
		}

		if f.getMessageClient == nil {
			err := f.initGetMessageClient()
			if err != nil {
				klog.ErrorS(err, "can not init fornax core get message client", "endpoint", f.config.endpoint)
				time.Sleep(2 * time.Second)
				continue
			}
		}

		msg, err := f.getMessageClient.Recv()
		if err == io.EOF {
			klog.ErrorS(err, "fornaxCore closed stream at server side, reset to get a new stream client")
			f.getMessageClient = nil
			continue
		}

		if err != nil {
			klog.Errorf("receive message from fornaxCore faied with unexpected error, %q, grpc is assumed to reconnect, reset to get a new stream client", err)
			if err != nil {
				f.getMessageClient = nil
				continue
			}
		}

		// TODO handle channel block
		panicReceivers := make(map[string]bool)
		for n, v := range f.receivers {
			klog.Infof("send fornax message to receiver %s, id: %s, type %s", n, msg.MessageIdentifier, msg.MessageType)
			func() {
				defer func() {
					if err := recover(); err != nil {
						klog.Errorf("send message panic occurred: %v", err)
						// remember it and remove closed channel after loop
						panicReceivers[n] = true
					}
				}()
				v <- msg
			}()
		}

		for n := range panicReceivers {
			delete(f.receivers, n)
		}
	}
}

// Stop implements FornaxCore
func (f *fornaxCoreClient) Stop() error {
	f.done = true
	return f.disconnect()
}

// Start implements FornaxCore
func (f *fornaxCoreClient) Start() error {
	go f.recvMessage()
	return nil
}

var _ FornaxCoreClient = &fornaxCoreClient{}

func NewFornaxCoreClient(config FornaxCoreConfiguration) *fornaxCoreClient {
	f := &fornaxCoreClient{
		config:           config,
		done:             false,
		receivers:        map[string]chan *fornax.FornaxCoreMessage{},
		conn:             nil,
		service:          nil,
		getMessageClient: nil,
	}
	return f
}
