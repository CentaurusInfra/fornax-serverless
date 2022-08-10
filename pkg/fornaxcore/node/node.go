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
	"sync"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/collection"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/store"
	fornaxutil "centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
)

type NodeWorkingState string

const (
	NodeWorkingStateRegistering NodeWorkingState = "registering"
	NodeWorkingStateTerminating NodeWorkingState = "terminating"
	NodeWorkingStateInActive    NodeWorkingState = "inactive"
	NodeWorkingStateRunning     NodeWorkingState = "running"
)

type FornaxNodeWithState struct {
	Node       *v1.Node
	Revision   int64
	State      NodeWorkingState
	Pods       *collection.ConcurrentStringSet
	DaemonPods map[string]*v1.Pod
	LastSeen   time.Time
}

type NodeUpdateBucket struct {
	internalGlobalRevision int64
	bucketHead             *store.ArrayBucket
	bucketTail             *store.ArrayBucket
}

func (nub *NodeUpdateBucket) truncate() {
	// drop one bucket
	if nub.internalGlobalRevision > MaxlengthOfNodeUpdates {
		nub.bucketHead = nub.bucketHead.Next
	}
}

func (nub *NodeUpdateBucket) appendUpdate(update interface{}) {
	// every bucket has maxium 500 elements
	if len(nub.bucketTail.Elements) >= 500 {
		newBucket := &store.ArrayBucket{
			Prev:     nub.bucketTail,
			Next:     nil,
			Elements: []interface{}{},
		}
		nub.bucketTail.Next = newBucket
		nub.bucketTail = newBucket
		nub.bucketTail.Elements = append(nub.bucketTail.Elements, update)
	} else {
		nub.bucketTail.Elements = append(nub.bucketTail.Elements, update)
	}
	nub.internalGlobalRevision++
	nub.truncate()
}

type StaleNodeBucket struct {
	sync.RWMutex
	bucketStale   map[string]*FornaxNodeWithState
	bucketRefresh map[string]*FornaxNodeWithState
}

func (snb *StaleNodeBucket) refreshNode(node *FornaxNodeWithState) {
	snb.Lock()
	defer snb.Unlock()
	snb.bucketRefresh[fornaxutil.UniqueNodeName(node.Node)] = node
	delete(snb.bucketStale, fornaxutil.UniqueNodeName(node.Node))
}

func (snb *StaleNodeBucket) getStaleNodes() []*FornaxNodeWithState {
	snb.Lock()
	defer snb.Unlock()

	var stales []*FornaxNodeWithState
	for _, v := range snb.bucketStale {
		stales = append(stales, v)
	}

	newBucket := map[string]*FornaxNodeWithState{}
	snb.bucketStale = snb.bucketRefresh
	snb.bucketRefresh = newBucket
	return stales
}
