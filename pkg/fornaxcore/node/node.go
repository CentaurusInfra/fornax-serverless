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

	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/store"
	fornaxutil "centaurusinfra.io/fornax-serverless/pkg/util"
)

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
	bucketStale   map[string]*ie.FornaxNodeWithState
	bucketRefresh map[string]*ie.FornaxNodeWithState
}

func (snb *StaleNodeBucket) refreshNode(node *ie.FornaxNodeWithState) {
	snb.Lock()
	defer snb.Unlock()
	snb.bucketRefresh[fornaxutil.Name(node.Node)] = node
	delete(snb.bucketStale, fornaxutil.Name(node.Node))
}

func (snb *StaleNodeBucket) getStaleNodes() []*ie.FornaxNodeWithState {
	snb.Lock()
	defer snb.Unlock()

	var stales []*ie.FornaxNodeWithState
	for _, v := range snb.bucketStale {
		stales = append(stales, v)
	}

	newBucket := map[string]*ie.FornaxNodeWithState{}
	snb.bucketStale = snb.bucketRefresh
	snb.bucketRefresh = newBucket
	return stales
}

type NodePool struct {
	mu    sync.RWMutex
	nodes map[string]*ie.FornaxNodeWithState
}

func (pool *NodePool) add(name string, pod *ie.FornaxNodeWithState) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.nodes[name] = pod
}

func (pool *NodePool) delete(name string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.nodes, name)
}

func (pool *NodePool) get(name string) *ie.FornaxNodeWithState {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if n, found := pool.nodes[name]; found {
		return n
	}

	return nil
}

func (pool *NodePool) length() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return len(pool.nodes)
}

func (pool *NodePool) list() []*ie.FornaxNodeWithState {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	nodes := []*ie.FornaxNodeWithState{}
	for _, v := range pool.nodes {
		nodes = append(nodes, v)
	}
	return nodes
}
