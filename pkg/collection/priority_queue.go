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

type QueueItem struct {
	elem  interface{}
	index int
}

type LessFunc func(interface{}, interface{}) bool
type KeyFunc func(interface{}) string

type PriorityQueue struct {
	indexes  map[string]*QueueItem
	queue    []*QueueItem
	lessFunc LessFunc
	keyFunc  KeyFunc
}

func NewPriorityQueue(lessFunc LessFunc, keyFunc KeyFunc) *PriorityQueue {
	return &PriorityQueue{
		indexes:  map[string]*QueueItem{},
		queue:    []*QueueItem{},
		lessFunc: lessFunc,
		keyFunc:  keyFunc,
	}
}
func (pq *PriorityQueue) Len() int {
	return len(pq.queue)
}

func (pq *PriorityQueue) Less(i, j int) bool {
	return pq.lessFunc(pq.queue[i].elem, pq.queue[j].elem)
}

func (pq *PriorityQueue) Swap(i, j int) {
	pq.queue[i], pq.queue[j] = pq.queue[j], pq.queue[i]
	pq.queue[i].index = i
	pq.queue[j].index = j
}

func (pq *PriorityQueue) Push(obj interface{}) {
	n := len(pq.queue)
	item := &QueueItem{
		elem:  obj,
		index: n,
	}
	pq.queue = append(pq.queue, item)
	pq.indexes[pq.keyFunc(obj)] = item
}

func (pq *PriorityQueue) Peak() interface{} {
	n := len(pq.queue)
	if n > 0 {
		return pq.queue[n-1].elem
	} else {
		return nil
	}
}

func (pq *PriorityQueue) Pop() interface{} {
	old := pq.queue
	n := len(old)
	if n == 0 {
		return nil
	}
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	pq.queue = old[0 : n-1]
	delete(pq.indexes, pq.keyFunc(item.elem))
	return item.elem
}

func (pq *PriorityQueue) Get(name string) (int, interface{}) {
	if obj, found := pq.indexes[name]; found {
		return obj.index, obj.elem
	} else {
		return -1, nil
	}
}

func (pq *PriorityQueue) List() []interface{} {
	items := []interface{}{}
	for _, v := range pq.indexes {
		items = append(items, v.elem)
	}
	return items
}
