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
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/integtest"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
)

type NodeDaemonManager interface {
	GetDaemons(node *FornaxNodeWithState) map[string]*v1.Pod
}

var _ NodeDaemonManager = &nodeDaemonManager{}

type nodeDaemonManager struct {
}

// GetDaemons implements NodeDaemonManager
func (*nodeDaemonManager) GetDaemons(node *FornaxNodeWithState) map[string]*v1.Pod {
	// TODO manage real daemon pods
	daemonPod := integtest.BuildATestDaemonPod()
	return map[string]*v1.Pod{util.UniquePodName(daemonPod): daemonPod.DeepCopy()}
}

func NewNodeDaemonManager() NodeDaemonManager {
	return &nodeDaemonManager{}
}
