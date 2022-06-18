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

package message

import (
	"k8s.io/klog/v2"
)

type ActorStop struct{}
type ActorStart struct{}
type ActorStopped struct{}
type ActorStarted struct{}

type ActorMessage struct {
	Sender ActorRef
	Body   interface{}
}

type ActorRef interface {
	Send(receiver ActorRef, msg interface{}) error
}

var _ ActorRef = &LocalChannelActorRef{}

type LocalChannelActorRef struct {
	Channel *chan ActorMessage
}

func (a *LocalChannelActorRef) Send(replyReceiver ActorRef, msg interface{}) error {
	func() {
		defer func() {
			if err := recover(); err != nil {
				klog.Errorf("send message panic occurred: %v", err)
			}
		}()

		*a.Channel <- ActorMessage{
			Sender: replyReceiver,
			Body:   msg,
		}
	}()
	return nil
}

type Actor interface {
	Start() error
	Stop() error
	Reference() ActorRef
}

var _ Actor = &LocalChannelActor{}

type LocalChannelActor struct {
	Identifier  string
	messageFunc MessageProcessFunc
	stop        bool
	channel     chan ActorMessage
}

func NewLocalChannelActor(identifier string, messageProcessor MessageProcessFunc) *LocalChannelActor {
	return &LocalChannelActor{
		Identifier:  identifier,
		channel:     make(chan ActorMessage),
		messageFunc: messageProcessor,
	}
}

// Reference implements Actor
func (a *LocalChannelActor) Reference() ActorRef {
	ref := LocalChannelActorRef{
		Channel: &a.channel,
	}
	return &ref
}

type StartFunc func() error
type StopFunc func() error
type MessageProcessFunc func(ActorMessage) (interface{}, error)

// Start implements Actor
func (a *LocalChannelActor) Start() error {
	a.stop = false
	if a.channel == nil {
		a.channel = make(chan ActorMessage, 30)
	}

	go func() {
		for {
			if a.stop {
				close(a.channel)
				break
			}

			select {
			case msg, ok := <-a.channel:
				if ok {
					klog.InfoS("receive a message: %v", msg)
					if err := a.OnReceive(msg); err != nil {
						klog.InfoS("failed to process this message: %v", err)
					}
				}
			}
		}
	}()

	return nil
}

// Stop implements Actor
func (a *LocalChannelActor) Stop() error {
	a.stop = true
	return nil
}

// OnReceive implements Actor
func (a *LocalChannelActor) OnReceive(msg ActorMessage) error {
	var err error
	var reply interface{}
	if reply, err = a.messageFunc(msg); err != nil {
		return err
	}

	if msg.Sender != nil && reply != nil {
		a.Reference().Send(msg.Sender, reply)
	}
	return nil
}
