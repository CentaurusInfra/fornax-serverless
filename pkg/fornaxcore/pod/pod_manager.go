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

package pod

import (
	"context"
	"errors"
	"sync"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/podscheduler"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var _ ie.PodManagerInterface = &podManager{}

var (
	PodNotFoundError           = errors.New("Pod does not exist")
	PodNotTerminatedYetError   = errors.New("Pod not terminated yet")
	PodIsAlreadyScheduledError = errors.New("Pod is already scheduled")
)

const (
	DefaultDeletionGracefulSeconds             = int64(30)
	DefaultNotScheduledDeletionGracefulSeconds = int64(5)
)

type PodWithFornaxNodeState struct {
	v1pod  *v1.Pod
	nodeId string
}

type PodPool struct {
	mu   sync.RWMutex
	pods map[string]*PodWithFornaxNodeState
}

func (pool *PodPool) Len() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return len(pool.pods)
}

func (pool *PodPool) copyMap() map[string]*PodWithFornaxNodeState {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pods := make(map[string]*PodWithFornaxNodeState, len(pool.pods))
	for k, v := range pool.pods {
		pods[k] = v
	}
	return pods
}

func (pool *PodPool) getItem(identifier string) *PodWithFornaxNodeState {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return pool.pods[identifier]
}

func (pool *PodPool) addItem(item *PodWithFornaxNodeState) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.pods[util.Name(item.v1pod)] = item
}

func (pool *PodPool) deleteItem(identifier string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.pods, identifier)
}

type PodStatePool struct {
	pods PodPool
}

func (pool *PodStatePool) findPod(identifier string) *PodWithFornaxNodeState {
	if p := pool.pods.getItem(identifier); p != nil {
		return p
	}
	return nil
}

func (pool *PodStatePool) deletePod(p *PodWithFornaxNodeState) {
	pool.pods.deleteItem(util.Name(p.v1pod))
}

func (pool *PodStatePool) addPod(p *PodWithFornaxNodeState) {
	pool.pods.addItem(p)
}

type podManager struct {
	ctx             context.Context
	podUpdates      chan *ie.PodEvent
	watchers        []chan<- interface{}
	podStatePool    *PodStatePool
	podScheduler    podscheduler.PodScheduler
	nodeAgentClient nodeagent.NodeAgentClient
}

// FindPod implements PodManager
func (pm *podManager) FindPod(identifier string) *v1.Pod {
	p := pm.podStatePool.findPod(identifier)
	if p != nil {
		return p.v1pod
	}

	return nil
}

func (pm *podManager) Watch(watcher chan<- interface{}) {
	pm.watchers = append(pm.watchers, watcher)
}

func (pm *podManager) Run(podScheduler podscheduler.PodScheduler) error {
	klog.Info("starting pod manager")
	pm.podScheduler = podScheduler

	klog.Info("starting pod updates notification")
	go func() {
		for {
			select {
			case <-pm.ctx.Done():
				// TODO, shutdown more gracefully, handoff fornaxcore primary ownership
				break
			case update := <-pm.podUpdates:
				for _, watcher := range pm.watchers {
					watcher <- update
				}
			}
		}
	}()

	return nil
}

// DeletePod is called by node manager when it found a pod does not exist anymore
// or from TerminatePod method if pod is not scheduled yet
// pod phase is should not in PodRunning
func (pm *podManager) DeletePod(nodeId string, pod *v1.Pod) (*v1.Pod, error) {
	// remove pod from schedule queue, if it does not exit, it's no op
	pm.podScheduler.RemovePod(pod)

	fornaxPodState := pm.podStatePool.findPod(util.Name(pod))
	if fornaxPodState == nil {
		return nil, PodNotFoundError
	}

	if util.PodIsRunning(fornaxPodState.v1pod) {
		return nil, PodNotTerminatedYetError
	}

	// pod does not have deletion timestamp, set it
	podInCache := fornaxPodState.v1pod
	if podInCache.GetDeletionTimestamp() == nil {
		if pod.DeletionTimestamp != nil {
			podInCache.DeletionTimestamp = pod.GetDeletionTimestamp()
		} else {
			podInCache.DeletionTimestamp = util.NewCurrentMetaTime()
		}
	}

	pm.podStatePool.deletePod(fornaxPodState)
	pm.podUpdates <- &ie.PodEvent{
		NodeId: nodeId,
		Pod:    podInCache.DeepCopy(),
		Type:   ie.PodEventTypeDelete,
	}

	return podInCache, nil
}

// TerminatePod is called by pod ower to terminate a pod, pod is move to terminating queue until a gracefulPeriod
// if pod is already scheduled, call node agent to terminate and  wait for node agent report back
// if pod not scheduled yet, just delete
// there could be a race condition with pod scheduler if node have not report back after sending to it
// pod will be terminate again by pod owner when node report this pod back,
func (pm *podManager) TerminatePod(pod *v1.Pod) error {
	// try best to remove pod from schedule queue, if it does not exit, it's no op
	pm.podScheduler.RemovePod(pod)

	fornaxPodState := pm.podStatePool.findPod(util.Name(pod))
	if fornaxPodState == nil {
		return PodNotFoundError
	}
	podInCache := fornaxPodState.v1pod

	// two cases,
	// 1/ pod not scheduled at all, then it's save to delete
	// 2/ pod is scheduled, but have not report back from node,
	// there could be a race condition when scheduler send a pod to node, but node have not report back,
	// we decided to just delete this pod, when node report it back and app owner will determine should pod de deleted again
	if len(fornaxPodState.nodeId) == 0 {
		pm.DeletePod("", pod)
	}

	// if pod exist, and pod does not have deletion timestamp, set it
	if podInCache.GetDeletionTimestamp() == nil {
		podInCache.DeletionTimestamp = util.NewCurrentMetaTime()
		if pod.DeletionTimestamp != nil {
			podInCache.DeletionTimestamp = pod.DeletionTimestamp.DeepCopy()
		}
	}

	if podInCache.DeletionGracePeriodSeconds == nil {
		gracePeriod := DefaultDeletionGracefulSeconds
		if pod.DeletionGracePeriodSeconds != nil {
			gracePeriod = *pod.GetDeletionGracePeriodSeconds()
		}
		podInCache.DeletionGracePeriodSeconds = &gracePeriod
	}

	// send to node agent to terminate pod if this pod is associated with a node
	if len(fornaxPodState.nodeId) > 0 && util.PodNotTerminated(podInCache) {
		// pod is bound with node, let node agent terminate it before deletion
		err := pm.nodeAgentClient.TerminatePod(fornaxPodState.nodeId, fornaxPodState.v1pod)
		if err != nil {
			return err
		}
	}

	return nil
}

func (pm *podManager) createPod(nodeId string, pod *v1.Pod) {
	var eType ie.PodEventType
	switch {
	case util.PodIsTerminated(pod):
		eType = ie.PodEventTypeTerminate
	default:
		eType = ie.PodEventTypeCreate
	}
	pm.podStatePool.addPod(&PodWithFornaxNodeState{
		v1pod:  pod,
		nodeId: nodeId,
	})
	pm.podUpdates <- &ie.PodEvent{
		NodeId: nodeId,
		Pod:    pod.DeepCopy(),
		Type:   eType,
	}
}

func (pm *podManager) updatePod(nodeId string, pod *v1.Pod, oldPodState *PodWithFornaxNodeState) {
	switch {
	case util.PodIsTerminated(pod):
		pm.podStatePool.deletePod(oldPodState)
		pm.podUpdates <- &ie.PodEvent{
			NodeId: nodeId,
			Pod:    pod.DeepCopy(),
			Type:   ie.PodEventTypeTerminate,
		}
	default:
		oldPodState.v1pod = pod.DeepCopy()
		oldPodState.nodeId = nodeId

		pm.podUpdates <- &ie.PodEvent{
			NodeId: nodeId,
			Pod:    pod.DeepCopy(),
			Type:   ie.PodEventTypeUpdate,
		}
	}
}

// AddPod is called when node agent report a newly implemented pod or application try to create a new pending pod
func (pm *podManager) AddPod(nodeId string, pod *v1.Pod) (*v1.Pod, error) {
	fornaxPodState := pm.podStatePool.findPod(util.Name(pod))
	if fornaxPodState == nil {
		newPod := pod.DeepCopy()
		// pod does not exist in pod manager, even it's terminated, still add it, and will delete next time when node does not report again
		if util.PodIsTerminated(newPod) {
			// pod is reported back by node agent as a terminated or failed pod
			if newPod.DeletionTimestamp == nil {
				newPod.DeletionTimestamp = util.NewCurrentMetaTime()
			}
			pm.createPod(nodeId, newPod)
		} else if len(newPod.Status.HostIP) > 0 {
			pm.createPod(nodeId, newPod)
		} else {
			pm.createPod(nodeId, newPod)
			pm.podScheduler.AddPod(newPod, 0*time.Second)
		}
		return newPod, nil
	} else {
		podInCache := fornaxPodState.v1pod
		// no change, node agent probably just send a full list again
		if podInCache.ResourceVersion == pod.ResourceVersion {
			return podInCache, nil
		}
		util.MergePod(podInCache, pod)
		fornaxPodState.nodeId = nodeId
		if util.PodIsTerminated(pod) {
			if podInCache.DeletionTimestamp == nil {
				if pod.DeletionTimestamp != nil {
					podInCache.DeletionTimestamp = pod.DeletionTimestamp.DeepCopy()
				} else {
					podInCache.DeletionTimestamp = util.NewCurrentMetaTime()
				}
			}
			pm.updatePod(nodeId, podInCache, fornaxPodState)
		} else if len(nodeId) > 0 {
			// pod is reported back by node agent but pod owner have determinted pod should be deleted, termiante this pod
			if podInCache.DeletionTimestamp != nil && util.PodNotTerminated(pod) {
				klog.InfoS("Terminate a running pod which was request to terminate", "pod", util.Name(pod))
				err := pm.nodeAgentClient.TerminatePod(nodeId, pod)
				if err != nil {
					return nil, err
				}
				pm.updatePod(nodeId, podInCache, fornaxPodState)
			} else {
				pm.updatePod(nodeId, podInCache, fornaxPodState)
			}
		} else {
			// this case is more likely pod owner call pod manager again to create a pending schedule pod twice
			pm.podScheduler.AddPod(podInCache, 0*time.Second)
		}
		return podInCache, nil
	}
}

func NewPodManager(ctx context.Context, nodeAgentProxy nodeagent.NodeAgentClient) *podManager {
	return &podManager{
		ctx:        ctx,
		podUpdates: make(chan *ie.PodEvent, 100),
		watchers:   []chan<- interface{}{},
		podStatePool: &PodStatePool{
			pods: PodPool{pods: map[string]*PodWithFornaxNodeState{}}},
		nodeAgentClient: nodeAgentProxy,
	}
}
