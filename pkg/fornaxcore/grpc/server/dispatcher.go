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
	"fmt"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"k8s.io/klog/v2"
)

type MessageDispatcher interface {
	DispatchMessage(node string, message *grpc.FornaxCoreMessage) error
}

func (g *grpcServer) getNodeChan(node string) (chan<- *grpc.FornaxCoreMessage, error) {
	g.RLock()
	defer g.RUnlock()

	ch, ok := g.nodeGetMessageChans[node]
	if !ok {
		return nil, fmt.Errorf("unknown destination")
	}

	return ch, nil
}

func (g *grpcServer) DispatchMessage(nodeIdentifier string, message *grpc.FornaxCoreMessage) error {
	klog.InfoS("Send a message via GetMessage stream connection", "node", nodeIdentifier)
	ch, err := g.getNodeChan(nodeIdentifier)
	if err != nil {
		return err
	}

	ch <- message
	return nil
}
