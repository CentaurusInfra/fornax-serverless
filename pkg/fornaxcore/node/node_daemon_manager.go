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
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	v1 "k8s.io/api/core/v1"
)

type NodeDaemonManager interface {
	GetDaemons(node *ie.FornaxNodeWithState) map[string]*v1.Pod
}

var _ NodeDaemonManager = &nodeDaemonManager{}

type nodeDaemonManager struct {
}

// GetDaemons implements NodeDaemonManager
func (*nodeDaemonManager) GetDaemons(node *ie.FornaxNodeWithState) map[string]*v1.Pod {
	// TODO manage real daemon pods
	return map[string]*v1.Pod{}
}

func NewNodeDaemonManager() NodeDaemonManager {
	return &nodeDaemonManager{}
}
