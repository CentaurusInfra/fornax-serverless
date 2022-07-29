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

package internalevent

import (
	v1 "k8s.io/api/core/v1"
)

type NodeUpdateType string

const (
	NodeUpdateTypeCreate NodeUpdateType = "create"
	NodeUpdateTypeUpdate NodeUpdateType = "update"
	NodeUpdateTypeDelete NodeUpdateType = "delete"
)

type PodUpdateType string

const (
	PodUpdateTypeCreate    PodUpdateType = "create"
	PodUpdateTypeUpdate    PodUpdateType = "update"
	PodUpdateTypeDelete    PodUpdateType = "delete"
	PodUpdateTypeTerminate PodUpdateType = "terminate"
)

type NodeUpdate struct {
	Node   *v1.Node
	Update NodeUpdateType
}

type PodUpdate struct {
	Node   *v1.Node
	Pod    *v1.Pod
	Update PodUpdateType
}

type NodeInfoProvider interface {
	ListNodes() []*v1.Node
	WatchNode(watcher chan<- interface{}) error
}
