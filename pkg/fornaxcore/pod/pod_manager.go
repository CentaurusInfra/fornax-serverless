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
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/podscheduler"
	fornaxstore "centaurusinfra.io/fornax-serverless/pkg/store"
	"centaurusinfra.io/fornax-serverless/pkg/store/factory"
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

// pod manager provide function to create and update pods in store,
// pod manager is called by node monitor to update pod when receiving messages from node
// or called by controllers to create a new pod or delete pods
// pod manager provide watch method to notify pod updates internally,
// controllers in process do not need to watch store or api server
// pod manager provide a scheduler to find available node for a pending pod,
// also, call node to terminate pod when pod is being deleted
type podManager struct {
	ctx             context.Context
	podUpdates      chan *ie.PodEvent
	watchers        []chan<- *ie.PodEvent
	podStore        fornaxstore.ApiStorageInterface
	podScheduler    podscheduler.PodScheduler
	nodeAgentClient nodeagent.NodeAgentClient
}

func (pm *podManager) FindPod(podName string) *v1.Pod {
	if p, err := factory.GetFornaxPodCache(pm.podStore, podName); err != nil || p == nil {
		return nil
	} else {
		return p
	}
}

func (pm *podManager) Watch(watcher chan<- *ie.PodEvent) {
	pm.watchers = append(pm.watchers, watcher)
}

func (pm *podManager) Run(podScheduler podscheduler.PodScheduler) error {
	pm.podScheduler = podScheduler

	klog.Info("starting pod updates watcher notification")
	go func() {
		for {
			select {
			case <-pm.ctx.Done():
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
func (pm *podManager) DeletePod(pod *v1.Pod) (*v1.Pod, error) {
	// remove pod from schedule queue, if it does not exit, it's no op
	pm.podScheduler.RemovePod(pod)

	podInStore, err := factory.GetFornaxPodCache(pm.podStore, util.Name(pod))
	if err != nil {
		return nil, err
	} else {
		if podInStore == nil {
			return nil, PodNotFoundError
		}
	}

	if util.PodIsRunning(podInStore) {
		return nil, PodNotTerminatedYetError
	}

	// pod does not have deletion timestamp, set it
	if podInStore.GetDeletionTimestamp() == nil {
		if pod.DeletionTimestamp != nil {
			podInStore.DeletionTimestamp = pod.GetDeletionTimestamp()
		} else {
			podInStore.DeletionTimestamp = util.NewCurrentMetaTime()
		}
	}

	_, err = factory.DeleteFornaxPod(pm.ctx, pm.podStore, util.Name(pod))
	if err != nil {
		return nil, err
	}
	pm.podUpdates <- &ie.PodEvent{
		Pod:  podInStore.DeepCopy(),
		Type: ie.PodEventTypeDelete,
	}

	return podInStore, nil
}

// TerminatePod is called by pod ower to terminate a pod, pod is move to terminating queue until a gracefulPeriod
// if pod is already scheduled, call node agent to terminate and  wait for node agent report back
// if pod not scheduled yet, just delete
// there could be a race condition with pod scheduler if node have not report back after sending to it
// pod will be terminate again by pod owner when node report this pod back,
// if pod exist, and pod does not have deletion timestamp, set it
func (pm *podManager) TerminatePod(podName string) error {
	podInStore, err := factory.GetFornaxPodCache(pm.podStore, podName)
	if err != nil {
		return err
	} else {
		if podInStore == nil {
			return PodNotFoundError
		}
	}

	// try best to remove pod from schedule queue, if it does not exit, it's no op
	pm.podScheduler.RemovePod(podInStore)

	// two cases,
	// 1/ pod not scheduled at all, then it's save to delete
	// 2/ pod is scheduled, but have not report back from node,
	// there could be a race condition when scheduler send a pod to node, but node have not report back,
	// we decided to just delete this pod, when node report it back and app owner will determine should pod de deleted again
	nodeId := util.GetPodFornaxNodeIdAnnotation(podInStore)
	if len(nodeId) == 0 {
		pm.DeletePod(podInStore)
	}

	if podInStore.GetDeletionTimestamp() == nil {
		podInStore.DeletionTimestamp = util.NewCurrentMetaTime()
	}

	if podInStore.DeletionGracePeriodSeconds == nil {
		gracePeriod := DefaultDeletionGracefulSeconds
		podInStore.DeletionGracePeriodSeconds = &gracePeriod
	}

	// pod is bound with node, let node agent terminate it before deletion
	if len(nodeId) > 0 && util.PodNotTerminated(podInStore) {
		err := pm.nodeAgentClient.TerminatePod(nodeId, podInStore)
		if err != nil {
			return err
		}
	}

	return nil
}

func (pm *podManager) HibernatePod(podName string) error {
	podInStore, err := factory.GetFornaxPodCache(pm.podStore, podName)
	if err != nil {
		return err
	} else {
		if podInStore == nil {
			return PodNotFoundError
		}
	}

	nodeId := util.GetPodFornaxNodeIdAnnotation(podInStore)
	if len(nodeId) > 0 && util.PodNotTerminated(podInStore) {
		err := pm.nodeAgentClient.HibernatePod(nodeId, podInStore)
		if err != nil {
			return err
		}
	}

	return nil
}

func (pm *podManager) createPodAndSendEvent(pod *v1.Pod) (*v1.Pod, error) {
	var eType ie.PodEventType
	switch {
	case util.PodIsTerminated(pod):
		eType = ie.PodEventTypeTerminate
	default:
		eType = ie.PodEventTypeCreate
	}
	p, err := factory.CreateFornaxPod(pm.ctx, pm.podStore, pod)
	if err != nil {
		return nil, err
	}
	newPod := p.DeepCopy()
	pm.podUpdates <- &ie.PodEvent{
		Pod:  newPod.DeepCopy(),
		Type: eType,
	}
	return newPod, nil
}

// AddPod is called when node agent report a newly implemented pod or application try to create a new pending pod
// if pod come from application, and in pending state, add it into scheduler pool.
// if a pod is reported back by node agent, check revision, skip update if no change
// if pod is terminated, delete it, otherwise add or update it.
// when pod in cache has delete timestamp, termiante pod reported by node
func (pm *podManager) AddOrUpdatePod(pod *v1.Pod) (*v1.Pod, error) {
	nodeId := util.GetPodFornaxNodeIdAnnotation(pod)
	podInStore, err := factory.GetFornaxPodCache(pm.podStore, util.Name(pod))
	if err != nil {
		return nil, err
	}
	if podInStore == nil {
		if util.PodIsTerminated(pod) {
			return pod, nil
		} else {
			newPod, err := pm.createPodAndSendEvent(pod)
			if err != nil {
				return nil, err
			}

			if len(nodeId) == 0 && util.PodIsPending(newPod) {
				pm.podScheduler.AddPod(newPod, 0*time.Second)
			}
			return newPod, nil
		}
	} else {
		if largerRv, _ := util.NodeRevisionLargerThan(pod, podInStore); !largerRv {
			return podInStore, nil
		}
		if len(nodeId) > 0 && podInStore.DeletionTimestamp != nil && util.PodNotTerminated(pod) {
			klog.InfoS("Terminate a running pod which was request to terminate", "pod", util.Name(pod))
			err := pm.nodeAgentClient.TerminatePod(nodeId, pod)
			if err != nil {
				return nil, err
			}
		}
		util.MergePod(pod, podInStore)
		if util.PodIsTerminated(pod) {
			factory.DeleteFornaxPod(pm.ctx, pm.podStore, util.Name(pod))
			pm.podUpdates <- &ie.PodEvent{Pod: podInStore.DeepCopy(), Type: ie.PodEventTypeTerminate}
		} else {
			podInStore, err = factory.UpdateFornaxPod(pm.ctx, pm.podStore, podInStore)
			if err != nil {
				return nil, err
			}
			pm.podUpdates <- &ie.PodEvent{Pod: podInStore.DeepCopy(), Type: ie.PodEventTypeUpdate}
		}

		if len(nodeId) == 0 && util.PodIsPending(podInStore) {
			pm.podScheduler.AddPod(podInStore, 0*time.Second)
		}
		return podInStore, nil
	}
}

func NewPodManager(ctx context.Context, podStore fornaxstore.ApiStorageInterface, nodeAgentProxy nodeagent.NodeAgentClient) *podManager {
	return &podManager{
		ctx:             ctx,
		podUpdates:      make(chan *ie.PodEvent, 1000),
		watchers:        []chan<- *ie.PodEvent{},
		podStore:        podStore,
		nodeAgentClient: nodeAgentProxy,
	}
}
