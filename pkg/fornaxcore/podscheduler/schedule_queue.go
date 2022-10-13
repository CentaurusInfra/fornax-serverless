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
	"sync"
	"time"

	"container/heap"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/collection"
	podutil "centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
)

type PodScheduleItem struct {
	pod         *v1.Pod
	app         *fornaxv1.Application
	requestTime time.Time
}

func PodRequestTimeLess(pi, pj interface{}) bool {
	return pi.(*PodScheduleItem).requestTime.UnixMilli() < pj.(*PodScheduleItem).requestTime.UnixMilli()
}

func PodName(pj interface{}) string {
	return podutil.Name(pj.(*PodScheduleItem).pod)
}

// A schedulePriorityQueue implements heap.Interface and holds Items.
type schedulePriorityQueue struct {
	mu    sync.RWMutex
	queue *collection.PriorityQueue
}

func (pq *schedulePriorityQueue) AddPod(v1pod *v1.Pod, duration time.Duration) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if _, item := pq.queue.Get(podutil.Name(v1pod)); item == nil {
		item := &PodScheduleItem{
			pod:         v1pod,
			requestTime: time.Now().Add(duration),
		}
		heap.Push(pq.queue, item)
	} else {
		// TODO, need update?
	}
}

// RemovePod remove pod from qeueue with same name of specified pod reference, and return removed pod
func (pq *schedulePriorityQueue) RemovePod(v1pod *v1.Pod) *v1.Pod {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if index, item := pq.queue.Get(podutil.Name(v1pod)); item != nil {
		heap.Remove(pq.queue, index)
		return item.(*PodScheduleItem).pod
	}
	return nil
}

func (pq *schedulePriorityQueue) GetPod(name string) *v1.Pod {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	if _, item := pq.queue.Get(name); item != nil {
		return item.(*PodScheduleItem).pod
	} else {
		return nil
	}
}

func (pq *schedulePriorityQueue) PeakPod() *v1.Pod {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	ob := pq.queue.Peak()
	if ob == nil {
		return nil
	}
	return ob.(*PodScheduleItem).pod
}

func (pq *schedulePriorityQueue) NextPod() *v1.Pod {
	item := pq.nextItem()
	if item == nil {
		return nil
	}
	return item.pod
}

func (pq *schedulePriorityQueue) nextItem() *PodScheduleItem {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	ob := pq.queue.Peak()
	if ob == nil {
		return nil
	}
	ob = heap.Pop(pq.queue)
	if ob == nil {
		return nil
	}
	return ob.(*PodScheduleItem)
}

type PodScheduleQueue struct {
	stop              bool
	c                 *sync.Cond
	activeQueue       schedulePriorityQueue
	backoffRetryQueue schedulePriorityQueue
}

// Stop clear queue, signal
func (q *PodScheduleQueue) Stop() {
	q.stop = true
	q.c.L.Lock()
	q.c.Broadcast()
	q.c.L.Unlock()
}

// Length implements PodScheduleQueue
func (q *PodScheduleQueue) Length() (int, int) {
	return q.activeQueue.queue.Len(), q.backoffRetryQueue.queue.Len()
}

func (ps *PodScheduleQueue) ReviveBackoffItem() {
	pods := []*v1.Pod{}
	timeNow := time.Now()
	for {
		item := ps.backoffRetryQueue.nextItem()
		if item == nil {
			break
		}
		if item.requestTime.Before(timeNow) {
			pods = append(pods, item.pod)
		} else {
			// put it back
			ps.backoffRetryQueue.AddPod(item.pod, item.requestTime.Sub(timeNow))
			break
		}
	}

	for _, v := range pods {
		ps.AddPod(v, 0)
	}
}

func (q *PodScheduleQueue) HasMore() bool {
	return q.activeQueue.PeakPod() != nil
}

// NextPod remove a pod from scheduleQueue's active queue, it block until it get a pod or schedule queue shutdown
func (q *PodScheduleQueue) NextPod() (pod *v1.Pod) {
	q.c.L.Lock()
	for {
		if q.stop {
			break
		}
		pod = q.activeQueue.NextPod()
		if pod == nil {
			q.c.Wait()
		} else {
			break
		}
	}
	q.c.L.Unlock()
	return pod
}

// BackoffPod move a pod from active queue into retry queue with a backoff duration
func (q *PodScheduleQueue) BackoffPod(v1pod *v1.Pod, backoffDuration time.Duration) {
	q.activeQueue.RemovePod(v1pod)
	q.backoffRetryQueue.AddPod(v1pod, backoffDuration)
}

// AddPod add pod into active queue, if there is already a copy with same name, remove old copy and add new copy,
func (q *PodScheduleQueue) AddPod(v1pod *v1.Pod, backoff time.Duration) {
	// remove old version and add new version to active queue
	if oldcopy := q.backoffRetryQueue.RemovePod(v1pod); oldcopy != nil {
		q.activeQueue.AddPod(v1pod, backoff)
	} else {
		// remove old version and add new version to active queue
		q.activeQueue.RemovePod(v1pod)
		q.activeQueue.AddPod(v1pod, backoff)
	}
	q.c.L.Lock()
	q.c.Broadcast()
	q.c.L.Unlock()
}

// RemovePod remove pod from active and retry queue
func (q *PodScheduleQueue) RemovePod(v1pod *v1.Pod) *v1.Pod {
	if oldcopy := q.activeQueue.RemovePod(v1pod); oldcopy != nil {
		return oldcopy
	}

	if oldcopy := q.backoffRetryQueue.RemovePod(v1pod); oldcopy != nil {
		return oldcopy
	}

	return nil
}

func newSchedulePriorityQueue() *schedulePriorityQueue {
	return &schedulePriorityQueue{
		mu:    sync.RWMutex{},
		queue: collection.NewPriorityQueue(PodRequestTimeLess, PodName),
	}
}

func NewScheduleQueue() *PodScheduleQueue {
	return &PodScheduleQueue{
		stop:              false,
		c:                 sync.NewCond(&sync.Mutex{}),
		activeQueue:       *newSchedulePriorityQueue(),
		backoffRetryQueue: *newSchedulePriorityQueue(),
	}
}
