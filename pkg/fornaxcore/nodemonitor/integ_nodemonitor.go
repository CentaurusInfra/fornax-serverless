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

package nodemonitor

import (
	"context"
	"math"
	"sync"
	"time"

	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/server"
	podutil "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var _ server.NodeMonitor = &integtestNodeMonitor{}

type integtestNodeMonitor struct {
	sync.RWMutex
	nodes map[string]*v1.Node

	// the nodes just been updated in the latest ticker period
	// node should be added in on receipt of node status report
	refreshedNodes map[string]interface{}

	ticker      *time.Ticker
	checkPeriod time.Duration
	countFresh  int
	chQuit      chan interface{}
}

// OnReady implements server.NodeMonitor
func (*integtestNodeMonitor) OnReady(msgDispatcher server.MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error {
	// update node, scheduler begin to schedule pod in ready node

	// for test, send node a test pod
	msg := podutil.BuildATestPodCreate("test_app")
	klog.InfoS("Send node a pod", "pod", msg)
	msgDispatcher.DispatchMessage(*message.NodeIdentifier.Identifier, msg)
	return nil
}

// OnRegistry implements server.NodeMonitor
func (*integtestNodeMonitor) OnRegistry(msgDispatcher server.MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error {
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
	msgDispatcher.DispatchMessage(*message.NodeIdentifier.Identifier, m)
	return nil
}

// OnUpdate implements server.NodeMonitor
func (*integtestNodeMonitor) OnUpdate(msgDispatcher server.MessageDispatcher, ctx context.Context, state *grpc.FornaxCoreMessage) error {
	// update node resource info
	return nil
}

func NewIntegNodeMonitor(checkPeriod time.Duration) *integtestNodeMonitor {
	return &integtestNodeMonitor{
		checkPeriod: checkPeriod,
		countFresh:  int(math.Ceil(StaleNodeTimeout.Seconds() / checkPeriod.Seconds())),
		ticker:      time.NewTicker(checkPeriod),
		chQuit:      make(chan interface{}),
	}
}
