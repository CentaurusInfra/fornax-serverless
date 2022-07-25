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
	"errors"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/podscheduler"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8spodutil "k8s.io/kubernetes/pkg/api/v1/pod"
)

type PodManager interface {
	CreatePod(*v1.Pod, string) (*v1.Pod, error)
	TerminatePod(*v1.Pod) (*v1.Pod, error)
	DeletePod(*v1.Pod) (*v1.Pod, error)
	FindPod(namespace, name string) *PodWithFornaxState
}

var _ PodManager = &podManager{}

type FornaxPodState string

var (
	PodNotFoundError           = errors.New("Pod does not exist")
	PodIsNotTerminatedError    = errors.New("Pod is still running, can not delete")
	PodIsAlreadyScheduledError = errors.New("Pod is scheduled, reject double schedule")
)

const (
	PodStatePendingSchedule FornaxPodState = "PendingSchedule"
	PodStatePendingImpl     FornaxPodState = "PendingImpl"
	PodStateRunning         FornaxPodState = "Running"
	PodStateTerminating     FornaxPodState = "Terminating"
)

type PodWithFornaxState struct {
	v1pod    *v1.Pod
	podState FornaxPodState
	nodeName string
}

type PodPool map[string]*PodWithFornaxState

type podManager struct {
	podsPerState   map[FornaxPodState]PodPool
	podsPerNode    map[string]PodPool
	podScheduler   podscheduler.PodScheduler
	nodeAgentProxy nodeagent.NodeAgentProxy
}

// FindPod implements PodManager
func (pm *podManager) FindPod(namespace string, name string) *PodWithFornaxState {
	p := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:         name,
			GenerateName: name,
			Namespace:    namespace,
		},
	}
	return pm.findPod(p)
}

func (pm *podManager) findPod(pod *v1.Pod) *PodWithFornaxState {
	for _, v := range pm.podsPerState {
		if p, found := v[util.UniquePodName(pod)]; found {
			return p
		}
	}

	return nil
}

// DeletePod is called to delete pod when node agent does not report a pod again
func (pm *podManager) DeletePod(pod *v1.Pod) (*v1.Pod, error) {
	p := pm.findPod(pod)
	if p == nil {
		// pod does not exist in pod manager, ignore it
		return nil, nil
	}

	existpod := p.v1pod

	// remove pod from schedule queue, if it does not exit, it's no op
	pm.podScheduler.RemovePod(existpod)

	// if pod exist, and pod does not have deletion timestamp, set it
	if p.v1pod.GetDeletionTimestamp() == nil {
		if pod.DeletionTimestamp != nil {
			existpod.DeletionTimestamp = pod.GetDeletionTimestamp()
		} else {
			existpod.DeletionTimestamp = &metav1.Time{
				Time: time.Now(),
			}
		}
	}

	if !util.IsInGracePeriod(existpod) {
		delete(pm.podsPerState[p.podState], util.UniquePodName(existpod))
	} else {
		delete(pm.podsPerState[p.podState], util.UniquePodName(existpod))
		pm.podsPerState[PodStateTerminating][util.UniquePodName(existpod)] = p
		// pod will be pruned by house keeping
	}
	return existpod, nil
}

// CreatePod create a new pod if it does not exist or update existing pod, it is called when node agent report a newly implemented pod or application try to create a new pending pod
func (pm *podManager) CreatePod(pod *v1.Pod, nodeName string) (*v1.Pod, error) {
	p := pm.findPod(pod)
	if p == nil {
		newPod := pod.DeepCopy()
		// pod does not exist in pod manager, add it into map and delete it
		// event it's terminating, still add it, and will delete next time when node do not report again
		if newPod.DeletionTimestamp != nil || newPod.Status.Phase == v1.PodFailed || newPod.Status.Phase == v1.PodSucceeded {
			pm.podsPerState[PodStateTerminating][util.UniquePodName(newPod)] = &PodWithFornaxState{
				v1pod:    newPod,
				podState: PodStateTerminating,
				nodeName: nodeName,
			}
		} else if len(newPod.Status.HostIP) > 0 {
			if k8spodutil.IsPodReady(newPod) {
				pm.podsPerState[PodStateRunning][util.UniquePodName(newPod)] = &PodWithFornaxState{
					v1pod:    newPod,
					podState: PodStateRunning,
					nodeName: nodeName,
				}
			} else {
				pm.podsPerState[PodStatePendingImpl][util.UniquePodName(newPod)] = &PodWithFornaxState{
					v1pod:    newPod,
					podState: PodStatePendingImpl,
					nodeName: nodeName,
				}
			}
		} else {
			pm.podsPerState[PodStatePendingSchedule][util.UniquePodName(newPod)] = &PodWithFornaxState{
				v1pod:    newPod,
				podState: PodStatePendingSchedule,
				nodeName: "",
			}
			pm.podScheduler.AddPod(newPod, 0*time.Second)
		}

		return newPod, nil
	} else {
		existPod := p.v1pod
		// pod already exist in pod manager, move it between different buckets, this is especially the case a scheduled pod is reported back by node agent
		if pod.DeletionTimestamp != nil || pod.Status.Phase == v1.PodFailed || pod.Status.Phase == v1.PodSucceeded {
			// pod is reported back by node agent, pod is terminated or failed in node agent side
			existPod.DeletionTimestamp = pod.DeletionTimestamp
			util.MergePodStatus(existPod, pod)
			delete(pm.podsPerState[p.podState], util.UniquePodName(pod))
			pm.podsPerState[PodStateTerminating][util.UniquePodName(pod)] = &PodWithFornaxState{
				v1pod:    existPod,
				podState: PodStateTerminating,
				nodeName: nodeName,
			}
		} else if len(pod.Status.HostIP) > 0 {
			// pod is reported back by node agent
			util.MergePodStatus(existPod, pod)
			if k8spodutil.IsPodReady(pod) {
				delete(pm.podsPerState[p.podState], util.UniquePodName(pod))
				pm.podsPerState[PodStateRunning][util.UniquePodName(pod)] = &PodWithFornaxState{
					v1pod:    existPod,
					podState: PodStateRunning,
					nodeName: nodeName,
				}
			} else {
				delete(pm.podsPerState[p.podState], util.UniquePodName(pod))
				pm.podsPerState[PodStatePendingImpl][util.UniquePodName(pod)] = &PodWithFornaxState{
					v1pod:    existPod,
					podState: PodStatePendingImpl,
					nodeName: nodeName,
				}
			}
		} else {
			// pod does not have host ip in status should be in pending schedule state,
			// this case is more likely pod scheduler call pod manager again to create a pending schedule pod twice
			if p.podState != PodStatePendingSchedule {
				return nil, PodIsAlreadyScheduledError
			}
			// keep new spec and ask scheduler to use new pod spec if pod is changed
			newPodWS := &PodWithFornaxState{
				v1pod:    pod.DeepCopy(),
				podState: PodStatePendingSchedule,
				nodeName: "",
			}
			pm.podsPerState[PodStatePendingSchedule][util.UniquePodName(pod)] = newPodWS
			pm.podScheduler.RemovePod(existPod)
			pm.podScheduler.AddPod(newPodWS.v1pod, 0)
		}
		return existPod, nil
	}
}

// TerminatePod notify node agent to terminate a pod
func (pm *podManager) TerminatePod(pod *v1.Pod) (*v1.Pod, error) {
	p := pm.findPod(pod)
	if p == nil {
		return nil, PodNotFoundError
	} else {
		// a not scheduled pod can be deleted
		if p.podState == PodStatePendingSchedule && len(p.v1pod.Status.HostIP) == 0 {
			delete(pm.podsPerState[PodStatePendingSchedule], util.UniquePodName(pod))
		}

		existPod := p.v1pod
		if existPod.DeletionTimestamp != nil {
			// pod is already terminated
		} else {
			pm.nodeAgentProxy.TerminatePod(p.nodeName, p.v1pod)
		}

		delete(pm.podsPerState[p.podState], util.UniquePodName(pod))
		pm.podsPerState[PodStateTerminating][util.UniquePodName(pod)] = p
		return p.v1pod, nil
	}
}

func NewPodManager(podScheduler podscheduler.PodScheduler, nodeAgentProxy nodeagent.NodeAgentProxy) *podManager {
	return &podManager{
		podsPerState:   map[FornaxPodState]PodPool{},
		nodeAgentProxy: nodeAgentProxy,
		podScheduler:   podScheduler,
	}
}
