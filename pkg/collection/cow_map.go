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

type CowMap[K comparable, V interface{}] struct {
	mu   sync.Mutex
	imap map[K]V
}

func (pool *CowMap[K, V]) Set(identifier K, value V) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	newMap := pool.cloneMap()
	newMap[identifier] = value
	pool.imap = newMap
}

func (pool *CowMap[K, V]) Delete(identifier K) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	newMap := pool.cloneMap()
	delete(newMap, identifier)
	pool.imap = newMap
}

func (pool *CowMap[K, V]) Get(identifier K) interface{} {
	if v, found := pool.imap[identifier]; found {
		return v
	}
	return nil
}

func (pool *CowMap[K, V]) Len() int {
	return len(pool.imap)
}

func (pool *CowMap[K, V]) cloneMap() map[K]V {
	dst := make(map[K]V, len(pool.imap))
	for k, v := range pool.imap {
		dst[k] = v
	}
	return dst
}

func NewCowMap[K comparable, V interface{}]() *CowMap[K, V] {
	return &CowMap[K, V]{
		mu:   sync.Mutex{},
		imap: map[K]V{},
	}
}
