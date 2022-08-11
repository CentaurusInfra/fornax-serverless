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
	k8spodutil "k8s.io/kubernetes/pkg/api/v1/pod"
)

type PodManager interface {
	AddPod(pod *v1.Pod, nodeName string) (*v1.Pod, error)
	DeletePod(pod *v1.Pod, nodename string) (*v1.Pod, error)
	TerminatePod(pod *v1.Pod) (*v1.Pod, error)
	FindPod(identifier string) *v1.Pod
	Watch(watcher chan<- interface{}) error
}

var _ PodManager = &podManager{}

type FornaxPodState string

var (
	PodNotFoundError           = errors.New("Pod does not exist")
	PodNotTerminatedYetError   = errors.New("Pod not terminated yet")
	PodAlreadyTerminatedError  = errors.New("Pod alredy terminated")
	PodIsAlreadyScheduledError = errors.New("Pod is scheduled, reject double schedule")
)

const (
	DefaultPodManagerHouseKeepingDuration                     = 10 * time.Second
	DefaultDeletionGracefulSeconds                            = int64(30)
	DefaultNotScheduledDeletionGracefulSeconds                = int64(5)
	PodStatePendingSchedule                    FornaxPodState = "PendingSchedule"
	PodStatePendingImpl                        FornaxPodState = "PendingImpl"
	PodStateRunning                            FornaxPodState = "Running"
	PodStateTerminating                        FornaxPodState = "Terminating"
)

type PodWithFornaxState struct {
	v1pod    *v1.Pod
	podState FornaxPodState
	nodeName string
}

type PodPool struct {
	mu   sync.RWMutex
	pods map[string]*PodWithFornaxState
}

func (pool *PodPool) copyMap() map[string]*PodWithFornaxState {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pods := make(map[string]*PodWithFornaxState, len(pool.pods))
	for k, v := range pool.pods {
		pods[k] = v
	}
	return pods
}

func (pool *PodPool) getItem(identifier string) *PodWithFornaxState {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return pool.pods[identifier]
}

func (pool *PodPool) addItem(item *PodWithFornaxState) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.pods[util.UniquePodName(item.v1pod)] = item
}

func (pool *PodPool) deleteItem(identifier string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.pods, identifier)
}

type PodStatePool struct {
	pendingImplPods     PodPool
	pendingSchedulePods PodPool
	terminatingPods     PodPool
	runningPods         PodPool
}

func (pool *PodStatePool) findPod(identifier string) *PodWithFornaxState {
	if p := pool.runningPods.getItem(identifier); p != nil {
		return p
	}

	if p := pool.terminatingPods.getItem(identifier); p != nil {
		return p
	}

	if p := pool.pendingImplPods.getItem(identifier); p != nil {
		return p
	}

	if p := pool.pendingSchedulePods.getItem(identifier); p != nil {
		return p
	}
	return nil
}

func (pool *PodStatePool) deletePod(p *PodWithFornaxState) {
	switch p.podState {
	case PodStatePendingImpl:
		pool.pendingImplPods.deleteItem(util.UniquePodName(p.v1pod))
	case PodStatePendingSchedule:
		pool.pendingSchedulePods.deleteItem(util.UniquePodName(p.v1pod))
	case PodStateRunning:
		pool.runningPods.deleteItem(util.UniquePodName(p.v1pod))
	case PodStateTerminating:
		pool.terminatingPods.deleteItem(util.UniquePodName(p.v1pod))
	}
}

func (pool *PodStatePool) addPod(p *PodWithFornaxState) {
	switch p.podState {
	case PodStatePendingImpl:
		pool.pendingImplPods.addItem(p)
	case PodStatePendingSchedule:
		pool.pendingSchedulePods.addItem(p)
	case PodStateRunning:
		pool.runningPods.addItem(p)
	case PodStateTerminating:
		pool.terminatingPods.addItem(p)
	}
}

type podManager struct {
	ctx                context.Context
	houseKeepingTicker *time.Ticker
	podUpdates         chan interface{}
	watchers           []chan<- interface{}
	podStatePool       *PodStatePool
	podScheduler       podscheduler.PodScheduler
	nodeAgentProxy     nodeagent.NodeAgentProxy
}

// FindPod implements PodManager
func (pm *podManager) FindPod(identifier string) *v1.Pod {
	p := pm.podStatePool.findPod(identifier)
	if p != nil {
		return p.v1pod
	}

	return nil
}

func (pm *podManager) Watch(watcher chan<- interface{}) error {
	pm.watchers = append(pm.watchers, watcher)
	return nil
}

func (pm *podManager) Run(podScheduler podscheduler.PodScheduler) error {
	// start pod scheduler
	klog.Info("starting pod scheduler")

	pm.podScheduler = podScheduler

	klog.Info("starting pod manager house keeping and pod updates notification")
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
			case <-pm.houseKeepingTicker.C:
				pm.printPodSummary()
				pm.pruneTerminatingPods()
			}
		}
	}()

	return nil
}

// DeletePod is called by node manager when it found a pod does not exist anymore
// and pod phase is should PodFailed or PodTerminated and remove it from memory
func (pm *podManager) DeletePod(pod *v1.Pod, nodeName string) (*v1.Pod, error) {
	// remove pod from schedule queue, if it does not exit, it's no op
	pm.podScheduler.RemovePod(pod)

	if util.IsPodNotTerminated(pod) {
		return nil, PodNotTerminatedYetError
	}

	oldPodState := pm.podStatePool.findPod(util.UniquePodName(pod))
	if oldPodState == nil {
		return nil, PodNotFoundError
	}

	// if pod exist, and pod does not have deletion timestamp, set it
	existpod := oldPodState.v1pod
	if existpod.GetDeletionTimestamp() == nil {
		if pod.DeletionTimestamp != nil {
			existpod.DeletionTimestamp = pod.GetDeletionTimestamp()
		} else {
			existpod.DeletionTimestamp = util.NewCurrentMetaTime()
		}
	}

	pm.podStatePool.deletePod(oldPodState)
	pm.podUpdates <- &ie.PodEvent{
		NodeName: nodeName,
		Pod:      pod.DeepCopy(),
		Type:     ie.PodEventTypeDelete,
	}

	return existpod, nil
}

// TerminatePod is called to terminate a pod, pod is move to terminating queue until a gracefulPeriod
// if pod not scheduled yet, it's safe to just delete after gracefulPeriod,
// when there is a race condition with pod scheduler when it report back after moving to terminating queeu,
// pod will be terminate again
// if pod is already scheduled, it wait for node agent report back
func (pm *podManager) TerminatePod(pod *v1.Pod) (*v1.Pod, error) {
	// try best to remove pod from schedule queue, if it does not exit, it's no op
	pm.podScheduler.RemovePod(pod)

	oldPodState := pm.podStatePool.findPod(util.UniquePodName(pod))
	if oldPodState == nil {
		return nil, PodNotFoundError
	}

	// if pod exist, and pod does not have deletion timestamp, set it
	existpod := oldPodState.v1pod
	if existpod.GetDeletionTimestamp() == nil {
		if pod.DeletionTimestamp != nil {
			existpod.DeletionTimestamp = pod.GetDeletionTimestamp()
		} else {
			existpod.DeletionTimestamp = util.NewCurrentMetaTime()
		}
	}

	gracePeriod := DefaultDeletionGracefulSeconds
	if len(existpod.Status.HostIP) == 0 {
		gracePeriod = DefaultNotScheduledDeletionGracefulSeconds
	}
	if existpod.DeletionGracePeriodSeconds == nil {
		existpod.DeletionGracePeriodSeconds = &gracePeriod
	}

	if oldPodState.podState == PodStateTerminating {
		// pod is already in terminating queue, return
		return oldPodState.v1pod.DeepCopy(), nil
	}

	// not terminated yet, let node agent terminate and report back
	// there could be a race condition with scheduler when scheduler send to node, but node did not report back,
	// when node report it back and podmanager will terminate it again since pod has deletion timestamp
	if len(oldPodState.nodeName) > 0 && util.IsPodNotTerminated(existpod) {
		// pod is bound with node, let node agent terminate it before deletion
		err := pm.nodeAgentProxy.TerminatePod(oldPodState.nodeName, oldPodState.v1pod)
		if err != nil {
			return nil, err
		}
	}

	// remove pod from previous state into terminating queue, pod will be pruned by house keeping
	pm.podStatePool.deletePod(oldPodState)
	oldPodState.podState = PodStateTerminating
	pm.podStatePool.addPod(oldPodState)

	return existpod, nil
}

func (pm *podManager) createPod(nodeName string, pod *v1.Pod, podState FornaxPodState) {
	var eType ie.PodEventType
	switch {
	case util.IsPodTerminated(pod):
		eType = ie.PodEventTypeTerminate
	default:
		eType = ie.PodEventTypeCreate
	}
	pm.podStatePool.addPod(&PodWithFornaxState{
		v1pod:    pod,
		podState: podState,
		nodeName: nodeName,
	})
	pm.podUpdates <- &ie.PodEvent{
		NodeName: nodeName,
		Pod:      pod.DeepCopy(),
		Type:     eType,
	}
}

func (pm *podManager) updatePod(nodeName string, pod *v1.Pod, oldPodState *PodWithFornaxState, newState FornaxPodState) {
	if oldPodState.podState != newState {
		pm.podStatePool.deletePod(oldPodState)
		pm.podStatePool.addPod(&PodWithFornaxState{
			v1pod:    pod,
			podState: newState,
			nodeName: nodeName,
		})
	}

	var eType ie.PodEventType
	switch {
	case util.IsPodTerminated(pod):
		eType = ie.PodEventTypeTerminate
	default:
		eType = ie.PodEventTypeUpdate
	}

	pm.podUpdates <- &ie.PodEvent{
		NodeName: nodeName,
		Pod:      pod.DeepCopy(),
		Type:     eType,
	}
}

// AddPod create a new pod if it does not exist or update pod, it is called when node agent report a newly implemented pod or application try to create a new pending pod
func (pm *podManager) AddPod(pod *v1.Pod, nodeName string) (*v1.Pod, error) {
	oldPodState := pm.podStatePool.findPod(util.UniquePodName(pod))
	if oldPodState == nil {
		newPod := pod.DeepCopy()
		// pod does not exist in pod manager, add it into map and delete it
		// even it's terminating, still add it, and will delete next time when node does not report again
		if util.IsPodTerminated(newPod) {
			// pod is reported back by node agent as a terminated or failed pod
			if newPod.DeletionTimestamp == nil {
				newPod.DeletionTimestamp = util.NewCurrentMetaTime()
			}
			pm.createPod(nodeName, newPod, PodStateTerminating)
		} else if len(newPod.Status.HostIP) > 0 {
			if k8spodutil.IsPodReady(newPod) {
				pm.createPod(nodeName, newPod, PodStateRunning)
			} else {
				pm.createPod(nodeName, newPod, PodStatePendingImpl)
			}
		} else {
			pm.createPod(nodeName, newPod, PodStatePendingSchedule)
			pm.podScheduler.AddPod(newPod, 0*time.Second)
		}
		return newPod, nil
	} else {
		existPod := oldPodState.v1pod
		existPodState := oldPodState.podState
		util.MergePodStatus(existPod, pod)
		oldPodState.nodeName = nodeName
		if util.IsPodTerminated(pod) {
			// pod is reported back by node agent as a terminated or failed pod
			if pod.DeletionTimestamp == nil {
				existPod.DeletionTimestamp = util.NewCurrentMetaTime()
			} else {
				existPod.DeletionTimestamp = pod.DeletionTimestamp
			}
			pm.updatePod(nodeName, existPod, oldPodState, PodStateTerminating)
		} else if len(nodeName) > 0 {
			if existPod.DeletionTimestamp != nil {
				// pod is reported back by node agent while pod owner determinted pod should be deleted, send node terminate message
				klog.InfoS("Terminate a running pod which has delete timestamp", "pod", util.UniquePodName(pod))
				err := pm.nodeAgentProxy.TerminatePod(nodeName, pod)
				if err != nil {
					return nil, err
				}
				pm.updatePod(nodeName, existPod, oldPodState, PodStateTerminating)
			} else if k8spodutil.IsPodReady(existPod) {
				pm.updatePod(nodeName, existPod, oldPodState, PodStateRunning)
			} else {
				pm.updatePod(nodeName, existPod, oldPodState, PodStatePendingImpl)
			}
		} else {
			// pod does not have host ip in status should be in pending schedule state,
			// this case is more likely pod scheduler call pod manager again to create a pending schedule pod twice
			if existPodState != PodStatePendingSchedule {
				return nil, PodIsAlreadyScheduledError
			}
			pm.podScheduler.AddPod(existPod, 0)
		}
		return existPod, nil
	}
}

func (pm *podManager) pruneTerminatingPods() {
	terminatingPods := pm.podStatePool.terminatingPods.copyMap()
	for name, pod := range terminatingPods {
		if util.IsPodTerminated(pod.v1pod) || (len(pod.nodeName) == 0 && util.IsNotInGracePeriod(pod.v1pod)) {
			pm.podStatePool.terminatingPods.deleteItem(name)
			pm.podUpdates <- &ie.PodEvent{
				NodeName: pod.nodeName,
				Pod:      pod.v1pod.DeepCopy(),
				Type:     ie.PodEventTypeDelete,
			}
		} else {
			klog.InfoS("A terminating pod", "pod", name, "phase", pod.v1pod.Status.Phase, "hostIp", pod.v1pod.Status.HostIP, "nodename", pod.nodeName, "deletion time", pod.v1pod.DeletionTimestamp, "grace second", *pod.v1pod.DeletionGracePeriodSeconds)
		}
	}
}

func (pm *podManager) printPodSummary() {
	klog.InfoS("pod summary:",
		"running", len(pm.podStatePool.runningPods.pods),
		"pendingimpl", len(pm.podStatePool.pendingImplPods.pods),
		"pendingschedule", len(pm.podStatePool.pendingSchedulePods.pods),
		"terminating", len(pm.podStatePool.terminatingPods.pods),
	)
}

func NewPodManager(ctx context.Context, nodeAgentProxy nodeagent.NodeAgentProxy) *podManager {
	return &podManager{
		ctx:                ctx,
		houseKeepingTicker: time.NewTicker(DefaultPodManagerHouseKeepingDuration),
		podUpdates:         make(chan interface{}, 100),
		watchers:           []chan<- interface{}{},
		podStatePool: &PodStatePool{
			pendingImplPods:     PodPool{pods: map[string]*PodWithFornaxState{}},
			pendingSchedulePods: PodPool{pods: map[string]*PodWithFornaxState{}},
			runningPods:         PodPool{pods: map[string]*PodWithFornaxState{}},
			terminatingPods:     PodPool{pods: map[string]*PodWithFornaxState{}}},
		nodeAgentProxy: nodeAgentProxy,
	}
}
