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

package collection

import (
	"sync"
)

type ConcurrentMap[K comparable, V interface{}] struct {
	mu   sync.RWMutex
	imap map[K]V
}

func (pool *ConcurrentMap[K, V]) Set(identifier K, value V) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	newMap := pool.imap
	newMap[identifier] = value
	pool.imap = newMap
}

func (pool *ConcurrentMap[K, V]) Delete(identifier K) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	newMap := pool.imap
	delete(newMap, identifier)
	pool.imap = newMap
}

func (pool *ConcurrentMap[K, V]) Get(identifier K) interface{} {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	if v, found := pool.imap[identifier]; found {
		return v
	}
	return nil
}

func (pool *ConcurrentMap[K, V]) Len() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return len(pool.imap)
}

func NewConcurrentMap[K comparable, V interface{}]() *ConcurrentMap[K, V] {
	return &ConcurrentMap[K, V]{
		mu:   sync.RWMutex{},
		imap: map[K]V{},
	}
}
