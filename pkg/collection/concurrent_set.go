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

	"k8s.io/apimachinery/pkg/util/sets"
)

type ConcurrentStringSet struct {
	mu    sync.RWMutex
	items sets.String
}

func (pool *ConcurrentStringSet) Add(identifier string) {
	pool.mu.Lock()
	pool.items[identifier] = sets.Empty{}
	pool.mu.Unlock()
}

func (pool *ConcurrentStringSet) Delete(identifier string) {
	pool.mu.Lock()
	delete(pool.items, identifier)
	pool.mu.Unlock()
}

func (pool *ConcurrentStringSet) Has(identifier string) bool {
	pool.mu.RLock()
	_, found := pool.items[identifier]
	pool.mu.RUnlock()
	return found
}

func (pool *ConcurrentStringSet) GetKeys() []string {
	pool.mu.RLock()
	list := pool.items.List()
	pool.mu.RUnlock()
	return list
}

func (pool *ConcurrentStringSet) Len() int {
	pool.mu.RLock()
	l := pool.items.Len()
	pool.mu.RUnlock()
	return l
}

func NewConcurrentSet() *ConcurrentStringSet {
	return &ConcurrentStringSet{
		mu:    sync.RWMutex{},
		items: sets.String{},
	}
}
