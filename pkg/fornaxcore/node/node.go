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
)

type NodePool struct {
	mu    sync.RWMutex
	nodes map[string]*ie.FornaxNodeWithState
}

func (pool *NodePool) add(name string, pod *ie.FornaxNodeWithState) {
	pool.mu.Lock()
	pool.nodes[name] = pod
	pool.mu.Unlock()
}

func (pool *NodePool) delete(name string) {
	pool.mu.Lock()
	delete(pool.nodes, name)
	pool.mu.Unlock()
}

func (pool *NodePool) get(name string) *ie.FornaxNodeWithState {
	pool.mu.RLock()
	if n, found := pool.nodes[name]; found {
		pool.mu.RUnlock()
		return n
	}

	pool.mu.RUnlock()
	return nil
}

func (pool *NodePool) length() int {
	pool.mu.RLock()
	l := len(pool.nodes)
	pool.mu.RUnlock()
	return l
}

func (pool *NodePool) list() []*ie.FornaxNodeWithState {
	pool.mu.RLock()
	nodes := []*ie.FornaxNodeWithState{}
	for _, v := range pool.nodes {
		nodes = append(nodes, v)
	}
	pool.mu.RUnlock()
	return nodes
}
