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

package podmonitor

import (
	"context"
	"sync"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/server"
	podutil "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var _ server.PodMonitor = &podMonitor{}

type podMonitor struct {
	sync.RWMutex
	pods map[string]*v1.Pod

	ticker      *time.Ticker
	checkPeriod time.Duration
	chQuit      chan interface{}
}

// OnUpdate implements server.PodMonitor
func (*podMonitor) OnUpdate(msgDispatcher server.MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error {
	podState := message.GetPodState()
	klog.InfoS("Received a pod state", "app", podState.GetAppIdentifier(), "pod", podState.GetPodIdentifier(), "state", podState.GetState())

	if podState.GetState() == grpc.PodState_Running && podState.GetAppIdentifier() == "test_app" {
		// terminate test pod
		appId := podState.GetAppIdentifier()
		podId := podState.GetPodIdentifier()
		body := grpc.FornaxCoreMessage_PodTerminate{
			PodTerminate: &grpc.PodTerminate{
				PodIdentifier: &podId,
				AppIdentifier: &appId,
			},
		}
		messageType := grpc.MessageType_POD_TERMINATE
		msg := &grpc.FornaxCoreMessage{
			MessageType: &messageType,
			MessageBody: &body,
		}
		msgDispatcher.DispatchMessage(message.GetNodeIdentifier().GetIdentifier(), msg)
	} else if podState.GetState() == grpc.PodState_Terminated {
		msg := podutil.BuildATestPodCreate("test_app")
		msgDispatcher.DispatchMessage(message.GetNodeIdentifier().GetIdentifier(), msg)
	}
	return nil
}

func New(checkPeriod time.Duration) *podMonitor {
	return &podMonitor{
		checkPeriod: checkPeriod,
		ticker:      time.NewTicker(checkPeriod),
		chQuit:      make(chan interface{}),
	}
}
