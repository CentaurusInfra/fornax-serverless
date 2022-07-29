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

import v1 "k8s.io/api/core/v1"

type PortMapping struct {
	containerPort int32
	hostPort      int32
}

type NodePortManager interface {
	GetPorts(node *v1.Node, pod *v1.Pod) []PortMapping
}

var _ NodePortManager = &nodePortManager{}

type nodePortManager struct {
}

// GetPorts implements NodePortManager
func (*nodePortManager) GetPorts(node *v1.Node, pod *v1.Pod) []PortMapping {
	panic("unimplemented")
}

func NewNodePortManager() NodePortManager {
	return &nodePortManager{}
}
