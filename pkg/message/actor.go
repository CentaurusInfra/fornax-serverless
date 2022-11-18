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
	"errors"

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
	Receive(msg ActorMessage) error
}

var _ ActorRef = &LocalChannelActorRef{}

type LocalChannelActorRef struct {
	Identifier string
	Channel    *chan ActorMessage
}

func Send(from, to ActorRef, msg interface{}) error {
	return to.Receive(ActorMessage{Sender: from, Body: msg})
}

func (a *LocalChannelActorRef) Receive(msg ActorMessage) error {
	var err error
	func() {
		defer func() {
			if err := recover(); err != nil {
				klog.Errorf("channel panic occurred: %v, %v", err, msg)
				err = errors.New("channel panic")
			}
		}()

		*a.Channel <- msg
	}()
	return err
}

type Actor interface {
	Start()
	Stop()
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
		channel:     make(chan ActorMessage, 30),
		messageFunc: messageProcessor,
	}
}

// Reference implements Actor
func (a *LocalChannelActor) Reference() ActorRef {
	ref := LocalChannelActorRef{
		Identifier: a.Identifier,
		Channel:    &a.channel,
	}
	return &ref
}

type StartFunc func() error
type StopFunc func() error
type MessageProcessFunc func(ActorMessage) (interface{}, error)

// Start implements Actor
func (a *LocalChannelActor) Start() {
	if a.channel == nil {
		a.channel = make(chan ActorMessage)
	}

	go func() {
		for {
			select {
			case msg, ok := <-a.channel:
				if ok {
					switch msg.Body.(type) {
					case ActorStop:
						klog.InfoS("Actor stopped", "message", msg.Body, "actor", a.Identifier)
						close(a.channel)
						return
					default:
						if err := a.OnReceive(msg); err != nil {
							klog.ErrorS(err, "Failed to process message", "message", msg.Body, "actor", a.Identifier)
						}
					}
				}
			}
		}
	}()
}

// Stop implements Actor
func (a *LocalChannelActor) Stop() {
	func() {
		defer func() {
			if err := recover(); err != nil {
				klog.Errorf("channel panic occurred: %v, %v", err)
			}
		}()

		a.channel <- ActorMessage{Sender: nil, Body: ActorStop{}}
	}()
}

// OnReceive implements Actor
func (a *LocalChannelActor) OnReceive(msg ActorMessage) error {
	var err error
	var reply interface{}
	if reply, err = a.messageFunc(msg); err != nil {
		return err
	}

	if msg.Sender != nil && reply != nil {
		Send(a.Reference(), msg.Sender, reply)
	}
	return nil
}
