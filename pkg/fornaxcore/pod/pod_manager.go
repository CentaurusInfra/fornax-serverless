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
	DefaultPodManagerHouseKeepingDuration      = 10 * time.Second
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
	ctx                context.Context
	houseKeepingTicker *time.Ticker
	podUpdates         chan *ie.PodEvent
	watchers           []chan<- interface{}
	podStatePool       *PodStatePool
	podScheduler       podscheduler.PodScheduler
	nodeAgentClient    nodeagent.NodeAgentClient
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
// and pod phase is should PodFailed or PodTerminated and remove it from memory
func (pm *podManager) DeletePod(nodeId string, pod *v1.Pod) (*v1.Pod, error) {
	// remove pod from schedule queue, if it does not exit, it's no op
	pm.podScheduler.RemovePod(pod)

	if util.PodNotTerminated(pod) {
		return nil, PodNotTerminatedYetError
	}

	oldPodState := pm.podStatePool.findPod(util.Name(pod))
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
		NodeId: nodeId,
		Pod:    pod.DeepCopy(),
		Type:   ie.PodEventTypeDelete,
	}

	return existpod, nil
}

// TerminatePod is called to terminate a pod, pod is move to terminating queue until a gracefulPeriod
// if pod not scheduled yet, it's safe to just delete after gracefulPeriod,
// when there is a race condition with pod scheduler when it report back after moving to terminating queeu,
// pod will be terminate again, if pod is already scheduled, it wait for node agent report back
func (pm *podManager) TerminatePod(pod *v1.Pod) error {
	// try best to remove pod from schedule queue, if it does not exit, it's no op
	pm.podScheduler.RemovePod(pod)

	fornaxPodState := pm.podStatePool.findPod(util.Name(pod))
	if fornaxPodState == nil {
		return PodNotFoundError
	}
	existpod := fornaxPodState.v1pod
	if existpod.DeletionTimestamp != nil {
		// already in deleting state, no op here, two cases
		// 1/ node have not been report back
		// 2/ node did not receive request, but pod will be deleted if it report back
		return nil
	}

	// if pod exist, and pod does not have deletion timestamp, set it
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

	// not terminated yet, let node agent terminate and report back
	// there could be a race condition with scheduler when scheduler send to node, but node did not report back,
	// when node report it back and podmanager will terminate it again since pod has deletion timestamp
	if len(fornaxPodState.nodeId) > 0 && util.PodNotTerminated(existpod) {
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

// AddPod create a new pod if it does not exist or update pod, it is called when node agent report a newly implemented pod or application try to create a new pending pod
func (pm *podManager) AddPod(nodeId string, pod *v1.Pod) (*v1.Pod, error) {
	oldPodState := pm.podStatePool.findPod(util.Name(pod))
	if oldPodState == nil {
		newPod := pod.DeepCopy()
		// pod does not exist in pod manager, add it into map and delete it
		// even it's terminating, still add it, and will delete next time when node does not report again
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
		existPod := oldPodState.v1pod
		// no change, node agent probably just send a full list again
		if existPod.ResourceVersion == pod.ResourceVersion {
			return existPod, nil
		}
		util.MergePod(existPod, pod)
		oldPodState.nodeId = nodeId
		if util.PodIsTerminated(pod) {
			// pod is reported back by node agent as a terminated or failed pod, delete it
			if existPod.DeletionTimestamp == nil {
				existPod.DeletionTimestamp = util.NewCurrentMetaTime()
			}
			pm.updatePod(nodeId, existPod, oldPodState)
		} else if len(nodeId) > 0 {
			// pod is reported back by node agent and pod owner have determinted pod should be deleted,
			// termiante this pod
			if existPod.DeletionTimestamp != nil && pod.DeletionTimestamp == nil {
				klog.InfoS("Terminate a running pod which has delete timestamp", "pod", util.Name(pod))
				err := pm.nodeAgentClient.TerminatePod(nodeId, pod)
				if err != nil {
					return nil, err
				}
				pm.updatePod(nodeId, existPod, oldPodState)
			} else {
				pm.updatePod(nodeId, existPod, oldPodState)
			}
		} else {
			// pod does not have host ip in status should be in pending schedule state,
			// this case is more likely pod scheduler call pod manager again to create a pending schedule pod twice
			pm.podScheduler.AddPod(existPod, 0)
		}
		return existPod, nil
	}
}

func NewPodManager(ctx context.Context, nodeAgentProxy nodeagent.NodeAgentClient) *podManager {
	return &podManager{
		ctx:                ctx,
		houseKeepingTicker: time.NewTicker(DefaultPodManagerHouseKeepingDuration),
		podUpdates:         make(chan *ie.PodEvent, 100),
		watchers:           []chan<- interface{}{},
		podStatePool: &PodStatePool{
			pods: PodPool{pods: map[string]*PodWithFornaxNodeState{}}},
		nodeAgentClient: nodeAgentProxy,
	}
}
