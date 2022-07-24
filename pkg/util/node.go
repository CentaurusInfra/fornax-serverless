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

package util

import (
	v1 "k8s.io/api/core/v1"
)

func UniqueNodeName(v1node *v1.Node) string {
	return v1node.GetName()
}

func MergeNodeStatus(oldcopy *v1.Node, newnode *v1.Node) {
	// keep existing node spec, and use new node status from node agent
	// set node in unschedulable state since node start registration, wait for node
	oldcopy.Status = *newnode.Status.DeepCopy()
}
