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

package app

import (
	"context"
	"fmt"
	"net"
	"time"

	fornaxcore_grpc "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/server"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/nodemonitor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"k8s.io/klog/v2"
)

func RunIntegTestGrpcServer(ctx context.Context, port int, certFile, keyFile string) error {

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

	nodeMonitor := nodemonitor.NewIntegNodeMonitor(10 * time.Second)
	server := server.New(nodeMonitor, nil, nil)
	grpcServer := grpc.NewServer(opts...)
	fornaxcore_grpc.RegisterFornaxCoreServiceServer(grpcServer, server)
	go func() {
		err = grpcServer.Serve(lis)
		if err != nil {
			klog.ErrorS(err, "Fornaxcore grpc server stopped to serve")
		}
	}()

	return nil
}
