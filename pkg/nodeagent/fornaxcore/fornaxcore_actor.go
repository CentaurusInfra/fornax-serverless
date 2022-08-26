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
	"fmt"
	"time"

	fornax "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/message"
	"k8s.io/klog/v2"
)

// fornaxcore actor work as a communication proxy with fornax core,
// it receive messages using fornax.GetMessage stream and send to node actor
// it send messages to fornax core using fornax.PutMessage
// fornaxcore actor talk with node actor using proto.actor protocol

type FornaxCoreActor struct {
	nodeIP        string
	identifier    string
	stop          bool
	innerActor    message.Actor
	fornaxcores   map[string]FornaxCoreClient
	fornaxChannel chan *fornax.FornaxCoreMessage
	nodeActor     message.ActorRef
	messageSeq    int64
}

func (n *FornaxCoreActor) Start(nodeActor message.ActorRef) error {
	// node actor is passed in as node actor initialization require fornax core actor ref
	n.nodeActor = nodeActor

	// init innerActor to talk with node actor and pod actors
	n.innerActor.Start()

	// listen to fornax grpc message
	for _, v := range n.fornaxcores {
		if err := v.GetMessage(fmt.Sprintf("FornaxCoreActor@%s", n.identifier), n.fornaxChannel); err != nil {
			return err
		}
	}

	// process fornax grpc message in a go routine
	go func() {
		for {
			if n.stop {
				close(n.fornaxChannel)
				klog.InfoS("Fornaxcore actor exit")
				break
			}
			select {
			case msg, ok := <-n.fornaxChannel:
				if ok {
					if msg.GetNodeIdentifier().GetIdentifier() != n.identifier {
						klog.Warningf("Received a message meant to send to different node %s, skip it", msg.GetMessageIdentifier)
					}

					// fornaxcore actor is message broker, all fornaxcore message is sent to node actor to handle execept fornaxcore configuration
					msgType := msg.GetMessageType()
					switch {
					case msgType == fornax.MessageType_UNSPECIFIED:
						klog.Warningf("Received message with unspecified type from fornaxcore, skip")
					case msgType == fornax.MessageType_FORNAX_CORE_CONFIGURATION:
						n.onFornaxCoreConfigurationCommand(msg.GetFornaxCoreConfiguration())
					default:
						// TODO handle error
						n.notify(n.nodeActor, msg)
					}
				} else {
					// TODO handle error
					klog.Warningf("Receiving message from fornaxcore meet error")
				}
			}
		}
	}()
	return nil
}

func (n *FornaxCoreActor) notify(receiver message.ActorRef, msg interface{}) error {
	return message.Send(n.innerActor.Reference(), receiver, msg)
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
func (n *FornaxCoreActor) actorMessageProcess(msg message.ActorMessage) (interface{}, error) {
	n.messageSeq += 1
	messageSeq := fmt.Sprintf("%d", n.messageSeq)
	for _, v := range n.fornaxcores {
		msgBody := msg.Body.(*fornax.FornaxCoreMessage)
		msgBody.MessageIdentifier = messageSeq
		msgBody.NodeIdentifier = &fornax.NodeIdentifier{
			Ip:         n.nodeIP,
			Identifier: n.identifier,
		}
		if err := v.PutMessage(msgBody); err != nil {
			klog.ErrorS(err, "failed to send message to fornax core")
		}
	}
	return nil, nil
}

// fornaxcore configuration tell node if fornaxcore has any change, currently only handle fornaxcore join and leave
// and setup connection with new fornaxcore and disconnect from old one
func (n *FornaxCoreActor) onFornaxCoreConfigurationCommand(msg *fornax.FornaxCoreConfiguration) error {
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

	newfornaxcores := InitFornaxCoreClients(n.nodeIP, n.identifier, newips)
	for k, v := range newfornaxcores {
		v.Start()
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

func InitFornaxCoreClients(nodeIp, nodeName string, fornaxCoreIps []string) map[string]FornaxCoreClient {
	configs := []*FornaxCoreConfiguration{}
	for _, v := range fornaxCoreIps {
		configs = append(configs, NewFornaxCoreConfiguration(v))
	}
	fornaxcores := map[string]FornaxCoreClient{}
	for _, v := range configs {
		f := NewFornaxCoreClient(&fornax.NodeIdentifier{
			Ip:         nodeIp,
			Identifier: nodeName,
		}, v)
		f.Start()
		fornaxcores[v.endpoint] = f
	}

	return fornaxcores
}

func NewFornaxCoreActor(nodeIP, nodeName string, fornaxCoreIps []string) *FornaxCoreActor {
	fornaxcores := InitFornaxCoreClients(nodeIP, nodeName, fornaxCoreIps)
	actor := &FornaxCoreActor{
		nodeIP:        nodeIP,
		identifier:    nodeName,
		stop:          false,
		fornaxcores:   fornaxcores,
		fornaxChannel: make(chan *fornax.FornaxCoreMessage, 30),
		messageSeq:    time.Now().Unix() + 1, // use current epeco for starting message seq, so, it will be different everytime when nodeagent start
	}

	actor.innerActor = message.NewLocalChannelActor(nodeName, actor.actorMessageProcess)
	return actor
}
