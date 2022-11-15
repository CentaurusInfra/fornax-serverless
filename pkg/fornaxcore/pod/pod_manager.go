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

type PodStateMap struct {
	mu   sync.RWMutex
	pods map[string]*PodWithFornaxNodeState
}

func (pool *PodStateMap) Len() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return len(pool.pods)
}

func (pool *PodStateMap) copyMap() map[string]*PodWithFornaxNodeState {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pods := make(map[string]*PodWithFornaxNodeState, len(pool.pods))
	for k, v := range pool.pods {
		pods[k] = v
	}
	return pods
}

func (pool *PodStateMap) getItem(name string) *PodWithFornaxNodeState {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return pool.pods[name]
}

func (pool *PodStateMap) findPod(name string) *PodWithFornaxNodeState {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	if p, f := pool.pods[name]; f {
		return p
	}
	return nil
}

func (pool *PodStateMap) deletePod(p *PodWithFornaxNodeState) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.pods, util.Name(p.v1pod))
}

func (pool *PodStateMap) addPod(p *PodWithFornaxNodeState) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.pods[util.Name(p.v1pod)] = p
}

type podManager struct {
	ctx             context.Context
	podUpdates      chan *ie.PodEvent
	watchers        []chan<- *ie.PodEvent
	podStateMap     *PodStateMap
	podScheduler    podscheduler.PodScheduler
	nodeAgentClient nodeagent.NodeAgentClient
}

// FindPod implements PodManager
func (pm *podManager) FindPod(identifier string) *v1.Pod {
	p := pm.podStateMap.findPod(identifier)
	if p != nil {
		return p.v1pod
	}

	return nil
}

func (pm *podManager) Watch(watcher chan<- *ie.PodEvent) {
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

	fornaxPodState := pm.podStateMap.findPod(util.Name(pod))
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

	pm.podStateMap.deletePod(fornaxPodState)
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
// if pod exist, and pod does not have deletion timestamp, set it
func (pm *podManager) TerminatePod(podName string) error {

	fornaxPodState := pm.podStateMap.findPod(podName)
	if fornaxPodState == nil {
		return PodNotFoundError
	}
	podInCache := fornaxPodState.v1pod
	// try best to remove pod from schedule queue, if it does not exit, it's no op
	pm.podScheduler.RemovePod(podInCache)

	// two cases,
	// 1/ pod not scheduled at all, then it's save to delete
	// 2/ pod is scheduled, but have not report back from node,
	// there could be a race condition when scheduler send a pod to node, but node have not report back,
	// we decided to just delete this pod, when node report it back and app owner will determine should pod de deleted again
	if len(fornaxPodState.nodeId) == 0 {
		pm.DeletePod("", podInCache)
	}

	if podInCache.GetDeletionTimestamp() == nil {
		podInCache.DeletionTimestamp = util.NewCurrentMetaTime()
	}

	if podInCache.DeletionGracePeriodSeconds == nil {
		gracePeriod := DefaultDeletionGracefulSeconds
		podInCache.DeletionGracePeriodSeconds = &gracePeriod
	}

	// pod is bound with node, let node agent terminate it before deletion
	if len(fornaxPodState.nodeId) > 0 && util.PodNotTerminated(podInCache) {
		err := pm.nodeAgentClient.TerminatePod(fornaxPodState.nodeId, fornaxPodState.v1pod)
		if err != nil {
			return err
		}
	}

	return nil
}

func (pm *podManager) HibernatePod(podName string) error {
	fornaxPodState := pm.podStateMap.findPod(podName)
	if fornaxPodState == nil {
		return PodNotFoundError
	}
	podInCache := fornaxPodState.v1pod

	if len(fornaxPodState.nodeId) > 0 && util.PodNotTerminated(podInCache) {
		err := pm.nodeAgentClient.HibernatePod(fornaxPodState.nodeId, fornaxPodState.v1pod)
		if err != nil {
			return err
		}
	}

	return nil
}

func (pm *podManager) createPodAndSendEvent(nodeId string, pod *v1.Pod) {
	var eType ie.PodEventType
	switch {
	case util.PodIsTerminated(pod):
		eType = ie.PodEventTypeTerminate
	default:
		eType = ie.PodEventTypeCreate
	}
	pm.podStateMap.addPod(&PodWithFornaxNodeState{v1pod: pod, nodeId: nodeId})
	pm.podUpdates <- &ie.PodEvent{
		NodeId: nodeId,
		Pod:    pod.DeepCopy(),
		Type:   eType,
	}
}

func (pm *podManager) updatePodAndSendEvent(nodeId string, pod *v1.Pod, oldPodState *PodWithFornaxNodeState) {
}

// AddPod is called when node agent report a newly implemented pod or application try to create a new pending pod
// if pod come from application, and in pending state, add it into scheduler pool.
// add pod when it does not exist in pod manager, even it's terminated, still add it,
// pod will be eventually deleted next time when node update does not include this pod anymore
// if a pod is reported back by node agent, check revision, skip update if no change
// when pod owner have determinted pod should be deleted, termiante this pod reported by node
func (pm *podManager) AddPod(nodeId string, pod *v1.Pod) (*v1.Pod, error) {
	fornaxPodState := pm.podStateMap.findPod(util.Name(pod))
	if fornaxPodState == nil {
		newPod := pod.DeepCopy()
		if util.PodIsTerminated(newPod) {
			if newPod.DeletionTimestamp == nil {
				newPod.DeletionTimestamp = util.NewCurrentMetaTime()
			}
			pm.createPodAndSendEvent(nodeId, newPod)
		} else if len(nodeId) > 0 {
			pm.createPodAndSendEvent(nodeId, newPod)
		} else {
			pm.createPodAndSendEvent(nodeId, newPod)
			pm.podScheduler.AddPod(newPod, 0*time.Second)
		}
		return newPod, nil
	} else {
		podInCache := fornaxPodState.v1pod.DeepCopy()
		largerRv, err := util.ResourceVersionLargerThan(pod, podInCache)
		if err != nil {
			return nil, err
		}
		if !largerRv {
			return podInCache, nil
		}
		if len(nodeId) > 0 {
			if podInCache.DeletionTimestamp != nil && util.PodNotTerminated(pod) {
				klog.InfoS("Terminate a running pod which was request to terminate", "pod", util.Name(pod))
				err := pm.nodeAgentClient.TerminatePod(nodeId, pod)
				if err != nil {
					return nil, err
				}
			}
			util.MergePod(pod, podInCache)
			switch {
			case util.PodIsTerminated(podInCache):
				pm.podStateMap.deletePod(fornaxPodState)
				pm.podUpdates <- &ie.PodEvent{NodeId: nodeId, Pod: podInCache.DeepCopy(), Type: ie.PodEventTypeTerminate}
			default:
				fornaxPodState.v1pod = podInCache.DeepCopy()
				fornaxPodState.nodeId = nodeId
				pm.podUpdates <- &ie.PodEvent{NodeId: nodeId, Pod: podInCache.DeepCopy(), Type: ie.PodEventTypeUpdate}
			}
		} else {
			pm.podScheduler.AddPod(podInCache, 0*time.Second)
		}
		return podInCache, nil
	}
}

func NewPodManager(ctx context.Context, nodeAgentProxy nodeagent.NodeAgentClient) *podManager {
	return &podManager{
		ctx:             ctx,
		podUpdates:      make(chan *ie.PodEvent, 1000),
		watchers:        []chan<- *ie.PodEvent{},
		podStateMap:     &PodStateMap{pods: map[string]*PodWithFornaxNodeState{}},
		nodeAgentClient: nodeAgentProxy,
	}
}
