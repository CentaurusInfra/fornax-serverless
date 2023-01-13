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
	"errors"
	"fmt"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
)

func (g *grpcServer) getNodeChan(nodeIdentifer string) (chan<- *grpc.FornaxCoreMessage, error) {
	g.RLock()

	ch, ok := g.nodeOutgoingChans[nodeIdentifer]
	if !ok {
		g.RUnlock()
		return nil, fmt.Errorf("unknown destination")
	}

	g.RUnlock()
	return ch, nil
}

func (g *grpcServer) DispatchNodeMessage(nodeIdentifier string, message *grpc.FornaxCoreMessage) error {
	var err error
	func() {
		defer func() {
			if err := recover(); err != nil {
				// node could be disconnected when dispacthing message
				err = errors.New("channel panic")
			}
		}()

		var ch chan<- *grpc.FornaxCoreMessage
		ch, err = g.getNodeChan(nodeIdentifier)
		if err == nil && ch != nil {
			ch <- message
		}
	}()
	return err
}
