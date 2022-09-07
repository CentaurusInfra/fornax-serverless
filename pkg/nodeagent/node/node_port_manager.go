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

package node

import (
	"errors"
	"math"
	"sync"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	v1 "k8s.io/api/core/v1"
)

const (
	HostPortNumPerRange = int32(10)
)

var (
	InSufficientHostPortError = errors.New("There are no free node port range slot")
)

type RangeSlot struct {
	allocatedPorts int32
	rangeSize      int32
	nextPortNum    int32
	allocated      bool
}

type HostPortRange struct {
	mu           sync.RWMutex
	startingPort int32
	rangeSlots   []*RangeSlot
}

func NewHostPortRange(rangeNum int, startingPort int32) *HostPortRange {
	return &HostPortRange{
		mu:           sync.RWMutex{},
		startingPort: startingPort,
		rangeSlots:   make([]*RangeSlot, rangeNum),
	}
}

func (pm *HostPortRange) deallocateHostPort(containerPorts []*v1.ContainerPort) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, v := range containerPorts {
		if v.HostPort > 0 {
			slotNo := (v.HostPort - pm.startingPort) / HostPortNumPerRange
			if pm.rangeSlots[slotNo] != nil {
				pm.rangeSlots[slotNo].allocatedPorts -= 1
				if pm.rangeSlots[slotNo].allocatedPorts <= 0 {
					pm.rangeSlots[slotNo] = nil
				}
			}
		}
	}
}

//allocateHostPort find not used host port range and set unique host port for each container port from this range,
// and mark range is used, if it can not allocate host port for all containter port, it rollback and return InSufficientHostPortError
func (pm *HostPortRange) allocateHostPort(node *v1.Node, containerPorts []*v1.ContainerPort) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	i := 0
	for slotNo, slot := range pm.rangeSlots {
		if i == len(containerPorts) {
			return nil
		}
		if slot == nil || slot.allocated == false {
			slot = &RangeSlot{
				allocatedPorts: 0,
				rangeSize:      HostPortNumPerRange,
				nextPortNum:    pm.startingPort + int32(slotNo)*HostPortNumPerRange,
				allocated:      true,
			}
			for j := 0; j < int(slot.rangeSize) && i < len(containerPorts); j++ {
				port := containerPorts[i]
				port.HostPort = slot.nextPortNum
				port.HostIP = node.Status.Addresses[0].Address
				slot.nextPortNum += 1
				slot.allocatedPorts += 1
				i += 1
			}
			pm.rangeSlots[slotNo] = slot
		}
	}

	// if we got here, we are not able to allocate host port for all container ports, rollback allocated slots
	for _, v := range containerPorts {
		if v.HostPort > 0 {
			slotNo := (v.HostPort - pm.startingPort) / HostPortNumPerRange
			if pm.rangeSlots[slotNo] != nil {
				pm.rangeSlots[slotNo].allocatedPorts -= 1
				if pm.rangeSlots[slotNo].allocatedPorts <= 0 {
					pm.rangeSlots[slotNo] = nil
				}
			}
		}
	}
	return InSufficientHostPortError
}

// initRangeWithContainerPorts set node port range slot state using a list of containerPorts of node,
// this initialization is called to before a node is ready to allocate new port to avoid duplicate allocation
func (pm *HostPortRange) initRangeWithContainerPorts(containerPorts []v1.ContainerPort) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, cont := range containerPorts {
		if cont.HostPort <= 0 {
			continue
		}
		slotNo := (cont.HostPort - pm.startingPort) / HostPortNumPerRange
		if int(slotNo) >= len(pm.rangeSlots) {
			array := make([]*RangeSlot, slotNo+10)
			copy(array, pm.rangeSlots)
			pm.rangeSlots = array
		}
		slot := pm.rangeSlots[slotNo]
		if slot == nil {
			slot = &RangeSlot{
				allocatedPorts: 0,
				rangeSize:      HostPortNumPerRange,
				nextPortNum:    pm.startingPort + int32(slotNo)*HostPortNumPerRange,
				allocated:      true,
			}
			pm.rangeSlots[slotNo] = slot
		}
		slot.allocated = true
		slot.allocatedPorts += 1
		slot.nextPortNum = int32(math.Max(float64(slot.nextPortNum), float64(cont.HostPort)))
	}
	return
}

type nodePortManager struct {
	portRange *HostPortRange
}

// AllocatePodPortMapping find a not used host port range slot to a pod and assign host port number from this range
// to each container port to make sure host port number is unique on a node to avoid conflict between pods
func (npm *nodePortManager) AllocatePodPortMapping(node *v1.Node, pod *v1.Pod) error {
	containerPorts := []*v1.ContainerPort{}
	for _, cont := range pod.Spec.Containers {
		for _, port := range cont.Ports {
			containerPorts = append(containerPorts, &port)
		}
	}

	err := npm.portRange.allocateHostPort(node, containerPorts)
	if err != nil {
		return err
	}
	conts := []v1.Container{}
	for _, v := range pod.Spec.Containers {
		cont := v.DeepCopy()
		ports := []v1.ContainerPort{}
		for _, v := range cont.Ports {
			port := v.DeepCopy()
			for _, v := range containerPorts {
				if v.ContainerPort == port.ContainerPort {
					port.HostPort = v.HostPort
					port.HostIP = v.HostIP
				}
			}
			ports = append(ports, *port)
		}
		cont.Ports = ports
		conts = append(conts, *cont)
	}
	pod.Spec.Containers = conts

	return nil
}

// DeallocatePodPortMapping check host port number of container ports of pod, and find allocated slot,
// and reset slot for allocation for other pods
func (npm *nodePortManager) DeallocatePodPortMapping(node *v1.Node, pod *v1.Pod) {
	containerPorts := []*v1.ContainerPort{}
	for _, cont := range pod.Spec.Containers {
		for _, port := range cont.Ports {
			containerPorts = append(containerPorts, &port)
		}
	}

	npm.portRange.deallocateHostPort(containerPorts)

	return
}

// initNodePortRangeSlot fill node port range usage via a list of existing pods reported on Node,
// this method is called before allocate new pod on this node,
// so node port range is initialized correctly to avoid double allocation
func (npm *nodePortManager) initNodePortRangeSlot(node *v1.Node, pod *v1.Pod) {
	containerPorts := GetContainerPorts(pod)
	npm.portRange.initRangeWithContainerPorts(containerPorts)
	return
}

func GetContainerPorts(pod *v1.Pod) []v1.ContainerPort {
	containerPorts := []v1.ContainerPort{}
	for _, cont := range pod.Spec.Containers {
		for _, port := range cont.Ports {
			containerPorts = append(containerPorts, *port.DeepCopy())
		}
	}
	return containerPorts
}

func NewNodePortManager(nodeConfig *config.NodeConfiguration) *nodePortManager {
	return &nodePortManager{
		portRange: NewHostPortRange(nodeConfig.MaxPods, nodeConfig.NodePortStartingNo),
	}
}
