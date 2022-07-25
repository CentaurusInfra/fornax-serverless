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

package podscheduler

import (
	"time"

	"container/heap"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	podutil "centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
)

type QueueItem struct {
	elem  interface{}
	index int
}

type LessFunc func(interface{}, interface{}) bool
type KeyFunc func(interface{}) string

type priorityQueue struct {
	indexes  map[string]*QueueItem
	queue    []*QueueItem
	lessFunc LessFunc
	keyFunc  KeyFunc
}

func NewPriorityQueue(lessFunc LessFunc, keyFunc KeyFunc) *priorityQueue {
	return &priorityQueue{
		indexes:  map[string]*QueueItem{},
		queue:    []*QueueItem{},
		lessFunc: lessFunc,
		keyFunc:  keyFunc,
	}
}
func (pq priorityQueue) Len() int {
	return len(pq.queue)
}

func (pq priorityQueue) Less(i, j int) bool {
	return pq.lessFunc(pq.queue[i].elem, pq.queue[j].elem)
}

func (pq priorityQueue) Swap(i, j int) {
	pq.queue[i], pq.queue[j] = pq.queue[j], pq.queue[i]
	pq.queue[i].index = i
	pq.queue[j].index = j
}

func (pq priorityQueue) Push(pod any) {
	n := len(pq.queue)
	item := &QueueItem{
		elem:  pod,
		index: n,
	}
	pq.queue = append(pq.queue, item)
	pq.indexes[pq.keyFunc(pod)] = item
}

func (pq priorityQueue) Peak() any {
	n := len(pq.queue)
	if n > 0 {
		return pq.queue[n-1].elem
	} else {
		return nil
	}
}

func (pq priorityQueue) Pop() any {
	old := pq.queue
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	pq.queue = old[0 : n-1]
	delete(pq.indexes, pq.keyFunc(item.elem))
	return item.elem
}

type PodScheduleItem struct {
	pod         *v1.Pod
	app         *fornaxv1.Application
	requestTime time.Time
}

func PodRequestTimeLess(pi, pj interface{}) bool {
	return pi.(PodScheduleItem).requestTime.UnixMilli() < pj.(PodScheduleItem).requestTime.UnixMilli()
}

func PodName(pj interface{}) string {
	return podutil.UniquePodName(pj.(PodScheduleItem).pod)
}

// A schedulePriorityQueue implements heap.Interface and holds Items.
type schedulePriorityQueue struct {
	queue *priorityQueue
	C     chan interface{}
}

func (pq *schedulePriorityQueue) AddPod(v1pod *v1.Pod, duration time.Duration) {
	if _, found := pq.queue.indexes[podutil.UniquePodName(v1pod)]; !found {
		item := PodScheduleItem{
			pod:         v1pod,
			requestTime: time.Now().Add(duration),
		}
		heap.Push(pq.queue, item)
	} else {
		// TODO, need update?
	}
}

func (pq *schedulePriorityQueue) RemovePod(v1pod *v1.Pod) {
	if item, found := pq.queue.indexes[podutil.UniquePodName(v1pod)]; found {
		delete(pq.queue.indexes, podutil.UniquePodName(v1pod))
		heap.Remove(pq.queue, item.index)
	}
}
func (pq *schedulePriorityQueue) GetPod(name string) *v1.Pod {
	if pod, found := pq.queue.indexes[name]; found {
		return pod.elem.(*v1.Pod)
	} else {
		return nil
	}
}

func (pq *schedulePriorityQueue) PeakPod() *v1.Pod {
	return pq.queue.Peak().(*v1.Pod)
}

func (pq *schedulePriorityQueue) NextPod() *v1.Pod {
	return heap.Pop(pq.queue).(*v1.Pod)
}

var _ PodScheduleQueue = &podScheduleQueue{}

type PodScheduleQueue interface {
	AddPod(pod *v1.Pod, duration time.Duration)
	RemovePod(pod *v1.Pod) *v1.Pod
	NextPod() *v1.Pod
	RetryPod(pod *v1.Pod, duration time.Duration)
}

type podScheduleQueue struct {
	stop        chan interface{}
	activeQueue schedulePriorityQueue
	retryQueue  schedulePriorityQueue
}

// NextPod read a pod from scheduleQueue's active queue, it block until it get a pod or schedule queue shutdown
func (q *podScheduleQueue) NextPod() (pod *v1.Pod) {
	for {
		pod = q.activeQueue.PeakPod()
		if pod == nil {
			select {
			case <-q.stop:
				break
			case <-q.activeQueue.C:
				// empty channel and go back to peak
				for {
					<-q.activeQueue.C
				}
			}
		} else {
			pod = q.activeQueue.NextPod()
			break
		}
	}
	return pod
}

// RetryPod move a pod from active queue into retry queue with a backoff duration
func (q *podScheduleQueue) RetryPod(v1pod *v1.Pod, backoffDuration time.Duration) {
	if oldcopy := q.activeQueue.GetPod(podutil.UniquePodName(v1pod)); oldcopy != nil {
		q.activeQueue.RemovePod(oldcopy)
		q.retryQueue.AddPod(oldcopy, backoffDuration)
	}
}

// AddPod add pod into active queue, if there is already a copy with same name, remove old copy and add new copy,
func (q *podScheduleQueue) AddPod(v1pod *v1.Pod, backoff time.Duration) {
	if oldcopy := q.retryQueue.GetPod(podutil.UniquePodName(v1pod)); oldcopy != nil {
		// remove old version and add new version to active queue
		q.retryQueue.RemovePod(oldcopy)
		q.activeQueue.AddPod(v1pod, backoff)
	}

	if oldcopy := q.activeQueue.GetPod(podutil.UniquePodName(v1pod)); oldcopy != nil {
		// remove old version and add new version to active queue
		q.activeQueue.RemovePod(oldcopy)
		q.activeQueue.AddPod(v1pod, backoff)
	}
}

// RemovePod remove pod from active and retry queue
func (q *podScheduleQueue) RemovePod(v1pod *v1.Pod) *v1.Pod {
	if oldcopy := q.activeQueue.GetPod(podutil.UniquePodName(v1pod)); oldcopy != nil {
		q.activeQueue.RemovePod(oldcopy)
		return oldcopy
	}

	if oldcopy := q.retryQueue.GetPod(podutil.UniquePodName(v1pod)); oldcopy != nil {
		q.retryQueue.RemovePod(oldcopy)
		return oldcopy
	}

	return nil
}

func newSchedulePriorityQueue() *schedulePriorityQueue {
	return &schedulePriorityQueue{
		queue: NewPriorityQueue(PodRequestTimeLess, PodName),
		C:     make(chan interface{}),
	}
}

func NewScheduleQueue() PodScheduleQueue {
	return &podScheduleQueue{
		stop:        make(chan interface{}),
		activeQueue: *newSchedulePriorityQueue(),
		retryQueue:  *newSchedulePriorityQueue(),
	}
}
