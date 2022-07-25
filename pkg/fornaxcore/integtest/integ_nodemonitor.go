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

package integtest

import (
	"context"
	"sync"

	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/server"
	podutil "centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var _ server.NodeMonitor = &integtestNodeMonitor{}

type integtestNodeMonitor struct {
	sync.RWMutex
	nodes map[string]*v1.Node

	chQuit chan interface{}
}

// OnPodUpdate implements server.NodeMonitor
func (*integtestNodeMonitor) OnPodUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	podState := message.GetPodState()
	klog.InfoS("Received a pod state", "pod", podutil.UniquePodName(podState.GetPod()), "state", podState.GetState())

	if podState.GetState() == grpc.PodState_Running {
		// terminate test pod
		podId := string(podState.GetPod().GetUID())
		body := grpc.FornaxCoreMessage_PodTerminate{
			PodTerminate: &grpc.PodTerminate{
				PodIdentifier: &podId,
			},
		}
		messageType := grpc.MessageType_POD_TERMINATE
		msg := &grpc.FornaxCoreMessage{
			MessageType: &messageType,
			MessageBody: &body,
		}
		return msg, nil
	} else if podState.GetState() == grpc.PodState_Terminated {
		msg := podutil.BuildATestPodCreate("test_app")
		return msg, nil
	}
	return nil, nil
}

// OnReady implements server.NodeMonitor
func (*integtestNodeMonitor) OnReady(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	// update node, scheduler begin to schedule pod in ready node

	// for test, send node a test pod
	msg := podutil.BuildATestPodCreate("test_app")
	klog.InfoS("Send node a pod", "pod", msg)
	return msg, nil
}

// OnRegistry implements server.NodeMonitor
func (*integtestNodeMonitor) OnRegistry(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	registry := message.GetNodeRegistry()

	daemonPod := podutil.BuildATestDaemonPod()

	// update node, send node configuration
	domain := default_config.DefaultDomainName
	node := registry.Node.DeepCopy()
	node.Spec.PodCIDR = "192.168.68.1/24"
	nodeConig := grpc.FornaxCoreMessage_NodeConfiguration{
		NodeConfiguration: &grpc.NodeConfiguration{
			ClusterDomain: &domain,
			Node:          node,
			DaemonPods:    []*v1.Pod{daemonPod.DeepCopy()},
		},
	}
	messageType := grpc.MessageType_NODE_CONFIGURATION
	m := &grpc.FornaxCoreMessage{
		MessageType: &messageType,
		MessageBody: &nodeConig,
	}

	klog.InfoS("Initialize node", "node configuration", m)
	return m, nil
}

// OnNodeUpdate implements server.NodeMonitor
func (*integtestNodeMonitor) OnNodeUpdate(ctx context.Context, state *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	// update node resource info
	return nil, nil
}

func NewIntegNodeMonitor() *integtestNodeMonitor {
	return &integtestNodeMonitor{
		nodes:  map[string]*v1.Node{},
		chQuit: make(chan interface{}),
	}
}
