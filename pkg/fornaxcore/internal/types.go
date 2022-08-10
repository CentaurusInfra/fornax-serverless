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

type NodeEventType string

const (
	NodeEventTypeCreate NodeEventType = "create"
	NodeEventTypeUpdate NodeEventType = "update"
	NodeEventTypeDelete NodeEventType = "delete"
)

type PodEventType string

const (
	PodEventTypeCreate    PodEventType = "create"
	PodEventTypeUpdate    PodEventType = "update"
	PodEventTypeDelete    PodEventType = "delete"
	PodEventTypeTerminate PodEventType = "terminate"
)

type NodeEvent struct {
	Node *v1.Node
	Type NodeEventType
}

type PodEvent struct {
	NodeName string
	Pod      *v1.Pod
	Type     PodEventType
}

type NodeInfoProvider interface {
	List() []*v1.Node
	Watch(watcher chan<- interface{}) error
}

type PodInfoProvider interface {
	Watch(watcher chan<- interface{}) error
}
