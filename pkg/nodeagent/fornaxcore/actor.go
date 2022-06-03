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
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/message"
	"k8s.io/klog/v2"
)

// fornaxcore actor work as a communication proxy with fornax core,
// it receive messages using fornax.GetMessage stream and send to node actor
// it send messages to fornax core using fornax.PutMessage
// fornaxcore actor talk with node actor using proto.actor protocol

type FornaxCoreActor struct {
	identifier  string
	stop        bool
	innerActor  message.Actor
	fornaxcores []FornaxCoreClient
	channel     chan *grpc.FornaxCoreMessage
	nodeActor   message.ActorRef
}

func (n *FornaxCoreActor) Start() error {
	// init innerActor to talk with node actor and pod actors
	if err := n.innerActor.Start(); err != nil {
		return err
	}

	// listen to fornax grpc message
	for _, v := range n.fornaxcores {
		if err := v.Start(); err != nil {
			v.GetMessage(n.identifier, n.channel)
			return err
		}
	}

	// process fornax grpc message in a go routine
	go func() {
		for {
			if n.stop {
				close(n.channel)
				klog.InfoS("fornaxcore actor exit")
				break
			}
			select {
			case msg, ok := <-n.channel:
				if ok {
					klog.InfoS("receive a message from fornaxcore: %v", &msg)
					if msg.GetNodeIdentifier() != n.identifier {
						klog.Warningf("message meant to send to different node %s, skip it", msg.GetMessageIdentifier)
					}

					// send these messages to proper node/pod/session actors
					msgType := msg.GetMessageType()
					switch {
					case msgType == grpc.MessageType_UNSPECIFIED:
						klog.Warningf("receiving message with unspecified type from fornaxcore, skip")
					default:
						// all fornaxcore message is sent to node actor to handle, fornaxcore actor is message broker
						n.nodeActor.Send(message.ActorMessage{Sender: n.innerActor.Reference(), Body: msg})
					}
				} else {
					klog.Warningf("receiving message from fornaxcore meet error")
				}
			}
		}
	}()
	return nil
}

func (n *FornaxCoreActor) Stop() error {
	n.stop = true
	n.innerActor.Stop()
	for _, v := range n.fornaxcores {
		v.Stop()
	}
	return nil
}

// when fornaxcore actor received another actor's message, it meant to send to fornaxcore a grpc message
// do not return error as it works as proxy, node/pod/session actors are supposed to resend new state
func (n *FornaxCoreActor) FornaxCoreMessageProcessor(msg message.ActorMessage) (interface{}, error) {
	go func() {
		for _, v := range n.fornaxcores {
			if err := v.PutMessage(msg.Body.(*grpc.FornaxCoreMessage)); err != nil {
				klog.ErrorS(err, "failed to send message to fornax cor ")
			}
		}
	}()
	return nil, nil
}

func NewFornaxCoreActor(identifier string, nodeActor message.ActorRef, config FornaxCoreConfiguration) *FornaxCoreActor {
	actor := &FornaxCoreActor{
		identifier:  identifier,
		stop:        false,
		fornaxcores: []FornaxCoreClient{},
		channel:     make(chan *grpc.FornaxCoreMessage, 30),
		nodeActor:   nodeActor,
	}

	actor.innerActor = message.NewLocalChannelActor(identifier, actor.FornaxCoreMessageProcessor)
	return actor
}
