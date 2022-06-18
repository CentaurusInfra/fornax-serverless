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
	"errors"

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
	fornaxcores map[string]FornaxCoreClient
	channel     chan *grpc.FornaxCoreMessage
	nodeActor   message.ActorRef
}

func (n *FornaxCoreActor) Start(nodeActor message.ActorRef) error {
	// node actor is passed in as node actor initialization require fornax core actor ref
	n.nodeActor = nodeActor

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
					case msgType == grpc.MessageType_FORNAX_CORE_CONFIGURATION:
						n.onFornaxCoreConfigurationCommand(msg.GetFornaxCoreConfiguration())
					default:
						// all fornaxcore message is sent to node actor to handle, fornaxcore actor is message broker
						n.innerActor.Reference().Send(n.nodeActor, msg)
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
				klog.ErrorS(err, "failed to send message to fornax core")
			}
		}
	}()
	return nil, nil
}

// fornaxcore configuration tell node if fornaxcore has any change, currently only handle fornaxcore join and leave
// and setup connection with new fornaxcore and disconnect from old one
func (n *FornaxCoreActor) onFornaxCoreConfigurationCommand(msg *grpc.FornaxCoreConfiguration) error {
	// reinitialize fornaxcore clients according configuration
	newips := []string{}
	newipset := map[string]bool{}
	primaryIp := msg.GetPrimary().GetIp()
	if len(primaryIp) == 0 {
		return errors.New("primary ip in fornax core configuration is nil")
	}
	_, found := n.fornaxcores[primaryIp]
	if !found {
		newips = append(newips, primaryIp)
		newipset[primaryIp] = true
	}

	for _, v := range msg.GetStandbys() {
		_, found := n.fornaxcores[v.GetIp()]
		if !found {
			newips = append(newips, v.GetIp())
			newipset[v.GetIp()] = true
		}
	}

	oldfornaxcores := map[string]FornaxCoreClient{}
	for k, v := range n.fornaxcores {
		_, found := newipset[k]
		if !found {
			// disappearing fornax core, mark it old and remove it from fornaxcores and close connection to it
			oldfornaxcores[k] = v
		}
	}

	newfornaxcores := InitFornaxCoreClients(newips)
	for k, v := range newfornaxcores {
		n.fornaxcores[k] = v
	}

	for k, v := range oldfornaxcores {
		delete(n.fornaxcores, k)
		v.Stop()
	}
	return nil
}

func (n *FornaxCoreActor) Reference() message.ActorRef {
	return n.innerActor.Reference()
}

func InitFornaxCoreClients(ips []string) map[string]FornaxCoreClient {
	configs := []*FornaxCoreConfiguration{}
	for _, v := range ips {
		configs = append(configs, NewFornaxCoreConfiguration(v))
	}
	fornaxcores := map[string]FornaxCoreClient{}
	for _, v := range configs {
		f := NewFornaxCoreClient(*v)
		fornaxcores[v.endpoint] = f
	}

	return fornaxcores
}

func NewFornaxCoreActor(identifier string, fornaxCoreIps []string) *FornaxCoreActor {
	fornaxcores := InitFornaxCoreClients(fornaxCoreIps)
	actor := &FornaxCoreActor{
		identifier:  identifier,
		stop:        false,
		fornaxcores: fornaxcores,
		channel:     make(chan *grpc.FornaxCoreMessage, 30),
		nodeActor:   nil,
	}

	actor.innerActor = message.NewLocalChannelActor(identifier, actor.FornaxCoreMessageProcessor)
	return actor
}
