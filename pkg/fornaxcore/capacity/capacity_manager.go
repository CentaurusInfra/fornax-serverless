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

package capacity

import (
	"context"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	v1 "k8s.io/api/core/v1"
)

type CapacityManager interface {
	GetSandbox(pod *v1.Pod) *FornaxSandbox
}

type _CapacityManager struct {
	ctx                context.Context
	nodeManager        *internal.NodeManagerInterface
	sandBoxPool        map[string]SandboxPool
	podUpdatesChannel  chan *internal.PodEvent
	nodeUpdatesChannel chan *internal.NodeEvent
}

func NewCapacityManager(ctx context.Context, nodeManager *internal.NodeManagerInterface) *_CapacityManager {
	return &_CapacityManager{
		ctx:                ctx,
		nodeManager:        nodeManager,
		sandBoxPool:        map[string]SandboxPool{},
		podUpdatesChannel:  make(chan *internal.PodEvent),
		nodeUpdatesChannel: make(chan *internal.NodeEvent),
	}
}

func (cm *_CapacityManager) Start() {
	go func() {
		for {
			select {
			case pe := <-cm.podUpdatesChannel:
				cm.handlePodEvent(pe)
			case <-cm.ctx.Done():
				return
			}
		}
	}()

	go func() {
		for {
			select {
			case ne := <-cm.nodeUpdatesChannel:
				cm.handleNodeEvent(ne)
			case <-cm.ctx.Done():
				return
			}
		}
	}()
}

func (cm *_CapacityManager) handleNodeEvent(ne *internal.NodeEvent) error {
	switch ne.Type {
	case internal.NodeEventTypeCreate:
		// TODO
		cm.partitionNode(ne.Node)
	case internal.NodeEventTypeUpdate:
		cm.partitionNode(ne.Node)
	case internal.NodeEventTypeDelete:
		cm.removeNode(ne.Node)
	}
	return nil
}

func (cm *_CapacityManager) handlePodEvent(pe *internal.PodEvent) error {
	switch pe.Type {
	case internal.PodEventTypeCreate:
		cm.admitSandboxAllocation(pe.Pod)
	case internal.PodEventTypeUpdate:
	case internal.PodEventTypeDelete:
		cm.releaseSandboxAllocation(pe.Pod)
	}
	return nil
}

func (cm *_CapacityManager) partitionNode(node *v1.Node) error {
	return nil
}

func (cm *_CapacityManager) removeNode(node *v1.Node) error {
	return nil
}

func (cm *_CapacityManager) admitSandboxAllocation(pod *v1.Pod) error {
	return nil
}

func (cm *_CapacityManager) releaseSandboxAllocation(pod *v1.Pod) error {
	return nil
}
