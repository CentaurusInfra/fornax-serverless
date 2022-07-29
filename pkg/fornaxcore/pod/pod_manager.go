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
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/podscheduler"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	k8spodutil "k8s.io/kubernetes/pkg/api/v1/pod"
)

type PodManager interface {
	CreatePod(*v1.Pod, string) (*v1.Pod, error)
	TerminatePod(*v1.Pod) (*v1.Pod, error)
	DeletePod(*v1.Pod) (*v1.Pod, error)
	FindPod(identifier string) *v1.Pod
}

var _ PodManager = &podManager{}

type FornaxPodState string

var (
	PodNotFoundError           = errors.New("Pod does not exist")
	PodAlreadyTerminatedError  = errors.New("Pod alredy terminated")
	PodIsAlreadyScheduledError = errors.New("Pod is scheduled, reject double schedule")
)

const (
	DefaultPodManagerHouseKeepingDuration                = 30 * time.Second
	DefaultDeletionGracefulSeconds                       = int64(30)
	PodStatePendingSchedule               FornaxPodState = "PendingSchedule"
	PodStatePendingImpl                   FornaxPodState = "PendingImpl"
	PodStateRunning                       FornaxPodState = "Running"
	PodStateTerminating                   FornaxPodState = "Terminating"
)

type PodWithFornaxState struct {
	v1pod    *v1.Pod
	podState FornaxPodState
	nodeName string
}

type PodPool struct {
	mu   sync.RWMutex
	pool map[string]*PodWithFornaxState
}

func (pool *PodPool) addItem(identifier string, item *PodWithFornaxState) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.pool[identifier] = item
}

func (pool *PodPool) deleteItem(identifier string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.pool, identifier)
}

type podManager struct {
	ctx                context.Context
	houseKeepingTicker *time.Ticker
	podUpdates         chan interface{}
	watchers           []chan<- interface{}
	podsPerState       map[FornaxPodState]*PodPool
	podScheduler       podscheduler.PodScheduler
	nodeAgentProxy     nodeagent.NodeAgentProxy
}

// FindPod implements PodManager
func (pm *podManager) FindPod(identifier string) *v1.Pod {
	p := pm.findPod(identifier)
	if p != nil {
		return p.v1pod
	}

	return nil
}

func (pm *podManager) Run() error {
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
				pm.PrintPodSummary()
				podsToDelete := []string{}
				terminatingPods := pm.podsPerState[PodStateTerminating].pool
				for k, pod := range terminatingPods {
					if util.IsPodTerminated(pod.v1pod) && !util.IsInGracePeriod(pod.v1pod) {
						podsToDelete = append(podsToDelete, k)
					}

					if len(pod.v1pod.Status.HostIP) == 0 && len(pod.nodeName) == 0 && !util.IsInGracePeriod(pod.v1pod) {
						podsToDelete = append(podsToDelete, k)
					}
				}

				for _, v := range podsToDelete {
					pm.podsPerState[PodStateTerminating].deleteItem(v)
				}
			}
		}
	}()

	return nil
}

func (pm *podManager) findPod(identifier string) *PodWithFornaxState {
	for _, v := range pm.podsPerState {
		if p, found := v.pool[identifier]; found {
			return p
		}
	}

	return nil
}

// DeletePod is called to delete pod when node agent does not report a pod again
func (pm *podManager) DeletePod(pod *v1.Pod) (*v1.Pod, error) {
	// remove pod from schedule queue, if it does not exit, it's no op
	pm.podScheduler.RemovePod(pod)

	p := pm.findPod(util.UniquePodName(pod))
	if p == nil {
		return nil, PodNotFoundError
	}

	// if pod exist, and pod does not have deletion timestamp, set it
	existpod := p.v1pod
	if existpod.GetDeletionTimestamp() == nil {
		if pod.DeletionTimestamp != nil {
			existpod.DeletionTimestamp = pod.GetDeletionTimestamp()
		} else {
			existpod.DeletionTimestamp = util.NewCurrentMetaTime()
		}
	}
	gracePeriod := DefaultDeletionGracefulSeconds
	if existpod.DeletionGracePeriodSeconds == nil {
		existpod.DeletionGracePeriodSeconds = &gracePeriod
	}

	// there could be a race condition with scheduler when scheduler send to node, but node did not report back,
	// when node report it back and podmanager will terminate it again since pod has deletion timestamp
	if len(p.nodeName) > 0 && util.IsPodNotTerminated(existpod) {
		// not terminated yet, let node agent terminate and report back
		_, err := pm.TerminatePod(existpod)
		if err != nil && err != PodAlreadyTerminatedError {
			return nil, err
		}
	}

	if !util.IsInGracePeriod(existpod) && util.IsPodTerminated(existpod) {
		pm.podsPerState[p.podState].deleteItem(util.UniquePodName(existpod))
	} else {
		// remove pod from previous state into terminating queue, pod will be pruned by house keeping
		pm.podsPerState[p.podState].deleteItem(util.UniquePodName(existpod))
		pm.podsPerState[PodStateTerminating].addItem(util.UniquePodName(existpod), &PodWithFornaxState{
			v1pod:    existpod,
			podState: PodStateTerminating,
			nodeName: p.nodeName,
		})
	}

	return existpod, nil
}

// CreatePod create a new pod if it does not exist or update existing pod, it is called when node agent report a newly implemented pod or application try to create a new pending pod
func (pm *podManager) CreatePod(pod *v1.Pod, nodeName string) (*v1.Pod, error) {
	p := pm.findPod(util.UniquePodName(pod))
	if p == nil {
		newPod := pod.DeepCopy()
		// pod does not exist in pod manager, add it into map and delete it
		// even it's terminating, still add it, and will delete next time when node does not report again
		if util.IsPodTerminated(newPod) {
			// pod is reported back by node agent as a terminated or failed pod
			if newPod.DeletionTimestamp == nil {
				newPod.DeletionTimestamp = util.NewCurrentMetaTime()
			}
			pm.podsPerState[PodStateTerminating].addItem(util.UniquePodName(newPod), &PodWithFornaxState{
				v1pod:    newPod,
				podState: PodStateTerminating,
				nodeName: nodeName,
			})
		} else if len(newPod.Status.HostIP) > 0 {
			if k8spodutil.IsPodReady(newPod) {
				pm.podsPerState[PodStateRunning].addItem(util.UniquePodName(newPod), &PodWithFornaxState{
					v1pod:    newPod,
					podState: PodStateRunning,
					nodeName: nodeName,
				})
			} else {
				pm.podsPerState[PodStatePendingImpl].addItem(util.UniquePodName(newPod), &PodWithFornaxState{
					v1pod:    newPod,
					podState: PodStatePendingImpl,
					nodeName: nodeName,
				})
			}
		} else {
			pm.podsPerState[PodStatePendingSchedule].addItem(util.UniquePodName(newPod), &PodWithFornaxState{
				v1pod:    newPod,
				podState: PodStatePendingSchedule,
				nodeName: "",
			})
			pm.podScheduler.AddPod(newPod, 0*time.Second)
		}
		return newPod, nil
	} else {
		existPod := p.v1pod
		existPodState := p.podState
		util.MergePodStatus(existPod, pod)
		p.nodeName = nodeName
		if util.IsPodTerminated(pod) {
			// pod is reported back by node agent as a terminated or failed pod
			if pod.DeletionTimestamp == nil {
				existPod.DeletionTimestamp = util.NewCurrentMetaTime()
			} else {
				existPod.DeletionTimestamp = pod.DeletionTimestamp
			}
			if existPodState != PodStateTerminating {
				pm.podsPerState[existPodState].deleteItem(util.UniquePodName(pod))
				pm.podsPerState[PodStateTerminating].addItem(util.UniquePodName(pod), &PodWithFornaxState{
					v1pod:    existPod,
					podState: PodStateTerminating,
					nodeName: nodeName,
				})
			}
		} else if len(nodeName) > 0 {
			if existPod.DeletionTimestamp != nil {
				// pod is reported back by node agent while pod owner determinted pod should be deleted, send node terminate message
				klog.InfoS("Terminate a running pod which has delete timestamp", "pod", util.UniquePodName(pod))
				err := pm.nodeAgentProxy.TerminatePod(nodeName, pod)
				if err != nil {
					return nil, err
				}
				if existPodState != PodStateTerminating {
					pm.podsPerState[existPodState].deleteItem(util.UniquePodName(pod))
					pm.podsPerState[PodStateTerminating].addItem(util.UniquePodName(pod), &PodWithFornaxState{
						v1pod:    existPod,
						podState: PodStateTerminating,
						nodeName: nodeName,
					})
				}
			} else if k8spodutil.IsPodReady(existPod) {
				if existPodState != PodStateRunning {
					pm.podsPerState[existPodState].deleteItem(util.UniquePodName(pod))
					pm.podsPerState[PodStateRunning].addItem(util.UniquePodName(pod), &PodWithFornaxState{
						v1pod:    existPod,
						podState: PodStateRunning,
						nodeName: nodeName,
					})
				}
			} else {
				if existPodState != PodStatePendingImpl {
					pm.podsPerState[existPodState].deleteItem(util.UniquePodName(pod))
					pm.podsPerState[PodStatePendingImpl].addItem(util.UniquePodName(pod), &PodWithFornaxState{
						v1pod:    existPod,
						podState: PodStatePendingImpl,
						nodeName: nodeName,
					})
				}
			}
		} else {
			// pod does not have host ip in status should be in pending schedule state,
			// this case is more likely pod scheduler call pod manager again to create a pending schedule pod twice
			if existPodState != PodStatePendingSchedule {
				return nil, PodIsAlreadyScheduledError
			}
			// keep new spec and ask scheduler to use new pod spec if pod is changed
			existPod = pod.DeepCopy()
			newPodWS := &PodWithFornaxState{
				v1pod:    existPod,
				podState: PodStatePendingSchedule,
				nodeName: "",
			}
			pm.podsPerState[PodStatePendingSchedule].addItem(util.UniquePodName(pod), newPodWS)
			pm.podScheduler.AddPod(newPodWS.v1pod, 0)
		}
		return existPod, nil
	}
}

// TerminatePod notify node agent to terminate a pod
func (pm *podManager) TerminatePod(pod *v1.Pod) (*v1.Pod, error) {
	// be cautious, let pod scheduler remove it from its queue anyway, if not in queue, its no-op
	pm.podScheduler.RemovePod(pod)

	p := pm.findPod(util.UniquePodName(pod))
	if p == nil {
		return nil, PodNotFoundError
	} else {
		if util.IsPodTerminated(p.v1pod) {
			// pod is already terminated, delete it
			return nil, PodAlreadyTerminatedError
		} else {
			if len(p.nodeName) > 0 {
				// pod is bound with node, let node agent terminate it before deletion
				pm.nodeAgentProxy.TerminatePod(p.nodeName, p.v1pod)
			} else {
				// pod is not bound with node, set deletion timestamp, and cleanup by house keeping when it exceed deletion gracePeriod, can be recreated if node report it later
				if p.v1pod.DeletionTimestamp == nil {
					p.v1pod.DeletionTimestamp = util.NewCurrentMetaTime()
				}

				gracePeriod := DefaultDeletionGracefulSeconds
				if p.v1pod.DeletionGracePeriodSeconds == nil {
					p.v1pod.DeletionGracePeriodSeconds = &gracePeriod
				}
				return p.v1pod, nil
			}
		}

		pm.podsPerState[p.podState].deleteItem(util.UniquePodName(pod))
		pm.podsPerState[PodStateTerminating].addItem(util.UniquePodName(pod),
			&PodWithFornaxState{
				v1pod:    p.v1pod,
				podState: PodStateTerminating,
				nodeName: p.nodeName,
			})
		return p.v1pod, nil
	}
}

func (pm *podManager) PrintPodSummary() {
	klog.InfoS("pod summary:",
		"running", len(pm.podsPerState[PodStateRunning].pool),
		"pendingimpl", len(pm.podsPerState[PodStatePendingImpl].pool),
		"pendingschedule", len(pm.podsPerState[PodStatePendingSchedule].pool),
		"terminating", len(pm.podsPerState[PodStateTerminating].pool),
	)
}

func NewPodManager(ctx context.Context, podScheduler podscheduler.PodScheduler, nodeAgentProxy nodeagent.NodeAgentProxy) *podManager {
	return &podManager{
		ctx:                ctx,
		houseKeepingTicker: time.NewTicker(DefaultPodManagerHouseKeepingDuration),
		podsPerState: map[FornaxPodState]*PodPool{
			PodStatePendingImpl: {
				pool: map[string]*PodWithFornaxState{},
			},
			PodStatePendingSchedule: {
				pool: map[string]*PodWithFornaxState{},
			},
			PodStateRunning: {
				pool: map[string]*PodWithFornaxState{},
			},
			PodStateTerminating: {
				pool: map[string]*PodWithFornaxState{},
			},
		},
		nodeAgentProxy: nodeAgentProxy,
		podScheduler:   podScheduler,
	}
}
