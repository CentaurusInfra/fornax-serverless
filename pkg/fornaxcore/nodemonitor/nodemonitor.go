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

// todo: determine more proper timeout
const StaleNodeTimeout = 10 * time.Second

var _ server.NodeMonitor = &nodeMonitor{}

type nodeMonitor struct {
	sync.RWMutex
	nodes map[string]*v1.Node

	// buckets based on refresh-ness; the last one contains the most recently refreshed nodes
	bucketHead  *bucket
	bucketTail  *bucket
	nodeLocator map[string]*bucket

	// the nodes just been updated in the latest ticker period
	// node should be added in on receipt of node status report
	refreshedNodes map[string]interface{}

	ticker      *time.Ticker
	checkPeriod time.Duration
	countFresh  int
	chQuit      chan interface{}
}

// OnReady implements server.NodeMonitor
func (*nodeMonitor) OnReady(msgDispatcher server.MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error {
	// update node, scheduler begin to schedule pod in ready node

	// for test, send node a test pod
	msg := podutil.BuildATestPodCreate("test_app")
	klog.InfoS("Send node a pod", "pod", msg)
	msgDispatcher.DispatchMessage(*message.NodeIdentifier.Identifier, msg)
	return nil
}

// OnRegistry implements server.NodeMonitor
func (*nodeMonitor) OnRegistry(msgDispatcher server.MessageDispatcher, ctx context.Context, message *grpc.FornaxCoreMessage) error {
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
func (*nodeMonitor) OnUpdate(msgDispatcher server.MessageDispatcher, ctx context.Context, state *grpc.FornaxCoreMessage) error {
	// update node resource info
	return nil
}

type bucket struct {
	prev, next *bucket
	elements   map[string]interface{}
}

func (n *nodeMonitor) StartDetectStaleNode() {
	go func() {
		for {
			select {
			case <-n.chQuit:
				// todo: log termination
				return
			case <-n.ticker.C:
				n.appendRefreshBucket()
				// todo: process the stale nodes properly
				_ = n.getStaleNodes()
			}
		}
	}()
}

func (n *nodeMonitor) StopDetectStaleNode() {
	close(n.chQuit)
}

func (n *nodeMonitor) replenishUpdatedNodes() map[string]interface{} {
	n.Lock()
	defer n.Unlock()

	oldCopy := n.refreshedNodes
	n.refreshedNodes = make(map[string]interface{})
	return oldCopy
}

func (n *nodeMonitor) appendRefreshBucket() {
	updatedNodes := n.replenishUpdatedNodes()

	n.RLock()
	defer n.RUnlock()
	newBucket := &bucket{
		prev:     n.bucketTail,
		next:     nil,
		elements: make(map[string]interface{}),
	}

	if n.bucketHead == nil {
		n.bucketHead = newBucket
	}
	if n.bucketTail == nil {
		n.bucketTail = newBucket
	} else {
		n.bucketTail.next = newBucket
		n.bucketTail = newBucket
	}

	for nodeName := range updatedNodes {
		if _, ok := n.nodeLocator[nodeName]; ok {
			delete(n.nodeLocator[nodeName].elements, nodeName)
		}
		newBucket.elements[nodeName] = struct{}{}
		n.nodeLocator[nodeName] = newBucket
	}
}

func (n *nodeMonitor) getStaleNodes() []string {
	n.Lock()
	defer n.Unlock()

	var stales []string

	currBucket := n.bucketTail
	count := n.countFresh
	var newHead *bucket
	for {
		if currBucket == nil {
			break
		}

		if count > 0 {
			if count == 1 {
				newHead = currBucket
			}
			currBucket = currBucket.prev
			count--
			continue
		}

		for name := range currBucket.elements {
			stales = append(stales, name)
		}

		processedBucket := currBucket
		currBucket = currBucket.prev
		processedBucket.next = nil
		processedBucket.prev = nil
	}

	if newHead != nil && newHead != n.bucketHead {
		n.bucketHead = newHead
		newHead.prev = nil
	}

	return stales
}

func New(checkPeriod time.Duration) *nodeMonitor {
	return &nodeMonitor{
		nodeLocator: map[string]*bucket{},
		checkPeriod: checkPeriod,
		countFresh:  int(math.Ceil(StaleNodeTimeout.Seconds() / checkPeriod.Seconds())),
		ticker:      time.NewTicker(checkPeriod),
		chQuit:      make(chan interface{}),
	}
}
