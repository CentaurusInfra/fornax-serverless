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

package application

import (
	"fmt"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxpod "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	"github.com/google/uuid"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	k8spodutil "k8s.io/kubernetes/pkg/api/v1/pod"
)

type ApplicationPodState uint8

const (
	DefaultPodPendingTimeoutDuration                     = 10 * time.Second
	PodStatePending                  ApplicationPodState = 0 // pod is pending schedule, waiting for node ack
	PodStateAllocated                ApplicationPodState = 1 // pod is assigned to a session
	PodStateDeleting                 ApplicationPodState = 2 // pod is being deleted
	PodStateIdle                     ApplicationPodState = 3 // pod is available to assign a session
)

type ApplicationPod struct {
	podName  string
	state    ApplicationPodState
	sessions map[string]bool
}

func NewApplicationPod(podName string, state ApplicationPodState) *ApplicationPod {
	return &ApplicationPod{
		podName:  podName,
		state:    state,
		sessions: map[string]bool{},
	}
}

func (am *ApplicationManager) onPodEventFromNode(podEvent *ie.PodEvent) {
	klog.InfoS("Received a pod event", "pod", util.Name(podEvent.Pod), "type", podEvent.Type, "phase", podEvent.Pod.Status.Phase, "condition", k8spodutil.IsPodReady(podEvent.Pod))
	if _, found := podEvent.Pod.Labels[fornaxv1.LabelFornaxCoreNodeDaemon]; !found {
		switch podEvent.Type {
		case ie.PodEventTypeCreate:
			am.handlePodAddUpdateFromNode(podEvent.Pod)
		case ie.PodEventTypeDelete:
			am.handlePodDeleteFromNode(podEvent.Pod)
		case ie.PodEventTypeUpdate:
			am.handlePodAddUpdateFromNode(podEvent.Pod)
		case ie.PodEventTypeTerminate:
			am.handlePodDeleteFromNode(podEvent.Pod)
		}
	}
}

// When a pod is created or updated, add this pod reference to app pods pool
func (am *ApplicationManager) handlePodAddUpdateFromNode(pod *v1.Pod) {
	podName := util.Name(pod)
	applicationKey, err := am.getPodApplicationKey(pod)
	if err != nil {
		klog.ErrorS(err, "Can not find application for pod, try best to use label", "pod", podName)
		if label, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreApplication]; !found {
			applicationKey = label
		}
	}

	if len(applicationKey) == 0 {
		klog.InfoS("Pod does not belong to any application, terminated it", "pod", podName, "labels", pod.GetLabels())
		am.podManager.TerminatePod(pod)
		return
	} else {
		pool := am.getOrCreateApplicationPool(applicationKey)
		ap := pool.getPod(podName)
		if ap != nil && ap.state == PodStateDeleting {
			// this pod was requested to terminate, and node did not receive termination or failed to do it, try it again
			am.deleteApplicationPod(pool, pod, true)
			return
		}
		if ap != nil && ap.state == PodStateAllocated {
			// this pod is assigned to session by FornaxCore, but node have not report back yet, or message lost, skip
			// after session setup timeout, this pod will be released
			return
		}
		if util.PodIsPending(pod) {
			if ap != nil && ap.state == PodStatePending {
				// this pod is just created by application itself, waiting for pod scheduled, no need to sync
				return
			}
			pool.addOrUpdatePod(podName, PodStatePending, []string{})
		} else if util.PodIsRunning(pod) {
			if _, yes := util.PodHasSession(pod); yes {
				pool.addOrUpdatePod(podName, PodStateAllocated, util.GetPodSessionNames(pod))
			} else {
				pool.addOrUpdatePod(podName, PodStateIdle, []string{})
			}
		} else {
			// do not add terminated pod
		}
	}
	am.enqueueApplication(applicationKey)
}

// When a pod is deleted, find application that manages it and remove pod reference from its pod pool
func (am *ApplicationManager) handlePodDeleteFromNode(pod *v1.Pod) {
	podName := util.Name(pod)
	if pod.DeletionTimestamp == nil {
		klog.InfoS("Pod does not have deletion timestamp, or pod is still alive, should add it", "pod", podName)
		am.handlePodAddUpdateFromNode(pod)
		return
	}

	applicationKey, err := am.getPodApplicationKey(pod)
	if err != nil {
		klog.ErrorS(err, "Can not find application for pod, try best to use label", "pod", podName)
		if label, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreApplication]; !found {
			applicationKey = label
		}
	}

	if len(applicationKey) == 0 {
		klog.InfoS("Pod does not belong to any application, skip a deleted pod", "pod", podName, "labels", pod.GetLabels())
		return
	} else {
		pool := am.getApplicationPool(applicationKey)
		if pool == nil {
			return
		}
		am.cleanupSessionOnDeletedPod(pool, podName)
		pool.deletePod(podName)
	}
	// enqueue application to evaluate application status
	am.enqueueApplication(applicationKey)
}

func (am *ApplicationManager) deleteApplicationPod(pool *ApplicationPool, pod *v1.Pod, retry bool) error {
	podName := util.Name(pod)

	podState := pool.getPod(podName)
	if podState == nil {
		return nil
	}

	if podState.state == PodStateDeleting && !retry {
		// already in deleting, skip unless forcely retry
		return nil
	}

	err := am.podManager.TerminatePod(pod)
	if err != nil {
		if err == fornaxpod.PodNotFoundError {
			pool.deletePod(podName)
		} else {
			klog.ErrorS(err, "Failed to delete application pod", "application", pool.appName, "pod", podName)
			return err
		}
	} else {
		pool.addOrUpdatePod(podName, PodStateDeleting, []string{})
		klog.InfoS("Delete a application pod", "application", pool.appName, "pod", podName)
	}

	return nil
}

func (am *ApplicationManager) createApplicationPod(application *fornaxv1.Application) (*v1.Pod, error) {
	uid := uuid.New()
	name := fmt.Sprintf("%s-%s-%d", application.Name, rand.String(16), uid.ClockSequence())
	podTemplate := am.getPodApplicationPodTemplate(uid, name, application)
	pod, err := am.podManager.AddPod("", podTemplate)
	if err != nil {
		return nil, err
	}

	return pod, nil
}

// getPodApplicationPodTemplate will translate application container spec to a pod spec,
// it add application specific environment variables
// to enable container to setup session connection with node and client
func (am *ApplicationManager) getPodApplicationPodTemplate(uid uuid.UUID, name string, application *fornaxv1.Application) *v1.Pod {
	enableServiceLinks := false
	setHostnameAsFQDN := false
	mountServiceAccount := false
	shareProcessNamespace := false
	preemptionPolicy := v1.PreemptNever
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			GenerateName:    name,
			UID:             types.UID(uid.String()),
			Namespace:       application.Namespace,
			ResourceVersion: "0",
			Generation:      1,
			CreationTimestamp: metav1.Time{
				Time: time.Now(),
			},
			DeletionTimestamp:          nil,
			DeletionGracePeriodSeconds: application.DeletionGracePeriodSeconds,
			Labels: map[string]string{
				fornaxv1.LabelFornaxCoreApplication: util.Name(application),
			},
			Annotations: map[string]string{},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         application.APIVersion,
					Kind:               application.Kind,
					Name:               application.Name,
					UID:                application.UID,
					Controller:         nil,
					BlockOwnerDeletion: nil,
				},
			},
			Finalizers: []string{},
		},
		Spec: v1.PodSpec{
			Volumes:                       []v1.Volume{},
			InitContainers:                []v1.Container{},
			EphemeralContainers:           []v1.EphemeralContainer{},
			RestartPolicy:                 v1.RestartPolicyNever,
			TerminationGracePeriodSeconds: nil,
			ActiveDeadlineSeconds:         nil,
			DNSPolicy:                     v1.DNSNone,
			NodeSelector:                  map[string]string{},
			ServiceAccountName:            "",
			DeprecatedServiceAccount:      "",
			AutomountServiceAccountToken:  &mountServiceAccount,
			NodeName:                      "",
			HostNetwork:                   false,
			HostPID:                       false,
			HostIPC:                       false,
			ShareProcessNamespace:         &shareProcessNamespace,
			SecurityContext:               &v1.PodSecurityContext{},
			ImagePullSecrets:              []v1.LocalObjectReference{},
			Hostname:                      "",
			Subdomain:                     default_config.DefaultDomainName,
			Affinity:                      &v1.Affinity{},
			Tolerations:                   []v1.Toleration{},
			HostAliases:                   []v1.HostAlias{},
			// PriorityClassName:             "",
			// Priority:                      nil,
			// DNSConfig:                     nil,
			ReadinessGates: []v1.PodReadinessGate{
				{
					ConditionType: v1.ContainersReady,
				},
			},
			// RuntimeClassName:          nil,
			EnableServiceLinks:        &enableServiceLinks,
			PreemptionPolicy:          &preemptionPolicy,
			Overhead:                  map[v1.ResourceName]resource.Quantity{},
			TopologySpreadConstraints: []v1.TopologySpreadConstraint{},
			SetHostnameAsFQDN:         &setHostnameAsFQDN,
			OS:                        &v1.PodOS{},
		},
		Status: v1.PodStatus{
			Phase: v1.PodPending,
		},
	}
	containers := []v1.Container{}
	for _, v := range application.Spec.Containers {
		cont := v.DeepCopy()
		cont.Env = append(cont.Env, v1.EnvVar{
			Name:  fornaxv1.LabelFornaxCorePod,
			Value: util.Name(pod),
		})
		cont.Env = append(cont.Env, v1.EnvVar{
			Name:  fornaxv1.LabelFornaxCoreApplication,
			Value: util.Name(application),
		})
		cont.Env = append(cont.Env, v1.EnvVar{
			Name: fornaxv1.LabelFornaxCoreSessionService,
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "status.hostIP",
				},
			},
		})
		containers = append(containers, *cont)
	}
	pod.Spec.Containers = containers
	return pod
}

// given a list pods, pick up which can be deleted with less cost, priority is
// 1, pods not find in podManager
// 2, pods still in pending state
// 3, idle pods
func (am *ApplicationManager) getPodsToBeDelete(pool *ApplicationPool, numOfDesiredDelete int) []*v1.Pod {
	podsToDelete := []*v1.Pod{}
	candidates := 0

	pendingPods := pool.podsOfState(PodStatePending)
	// add pod not yet scheduled
	for _, p := range pendingPods {
		pod := am.podManager.FindPod(p.podName)
		if pod == nil || len(pod.Status.HostIP) == 0 {
			podsToDelete = append(podsToDelete, pod)
			candidates += 1
			if candidates == numOfDesiredDelete {
				return podsToDelete
			}
		}
	}

	// add pod not yet scheduled
	for _, p := range pendingPods {
		pod := am.podManager.FindPod(p.podName)
		if pod == nil || len(pod.Status.HostIP) == 0 {
			podsToDelete = append(podsToDelete, pod)
			candidates += 1
			if candidates == numOfDesiredDelete {
				return podsToDelete
			}
		}
	}

	// add pod still pending node agent return status
	for _, p := range pendingPods {
		pod := am.podManager.FindPod(p.podName)
		if pod == nil || (len(pod.Status.HostIP) >= 0) {
			podsToDelete = append(podsToDelete, pod)
			candidates += 1
			if candidates == numOfDesiredDelete {
				return podsToDelete
			}
		}
	}

	// add pod status is unknown from running idle pods
	idlePods := pool.podsOfState(PodStateIdle)
	for _, p := range idlePods {
		pod := am.podManager.FindPod(p.podName)
		if pod == nil || pod.Status.Phase == v1.PodUnknown {
			podsToDelete = append(podsToDelete, pod)
			candidates += 1
			if candidates == numOfDesiredDelete {
				return podsToDelete
			}
		}
	}

	// pick any running pod
	for _, p := range idlePods {
		pod := am.podManager.FindPod(p.podName)
		if pod == nil || pod.Status.Phase == v1.PodRunning {
			podsToDelete = append(podsToDelete, pod)
			candidates += 1
			if candidates == numOfDesiredDelete {
				return podsToDelete
			}
		}
	}
	return podsToDelete
}

func (am *ApplicationManager) syncApplicationPods(applicationKey string, application *fornaxv1.Application, numOfDesiredPod, numOfIdlePod int) error {
	var err error

	desiredAddition := numOfDesiredPod - numOfIdlePod
	applicationBurst := util.ApplicationScalingBurst(application)
	if desiredAddition > 0 {
		if desiredAddition > applicationBurst {
			desiredAddition = applicationBurst
		}

		klog.InfoS("Creating pods", "application", applicationKey, "addition", desiredAddition)
		appPool := am.getOrCreateApplicationPool(applicationKey)
		createdPods := []*v1.Pod{}
		createErrors := []error{}
		for i := 0; i < desiredAddition; i++ {
			pod, err := am.createApplicationPod(application)
			if err != nil {
				klog.ErrorS(err, "Create pod failed", "application", applicationKey)
				if apierrors.HasStatusCause(err, v1.NamespaceTerminatingCause) {
					return nil
				}
				createErrors = append(createErrors, err)
				continue
			}
			appPool.addOrUpdatePod(util.Name(pod), PodStatePending, []string{})
			createdPods = append(createdPods, pod)
		}

		if desiredAddition != len(createdPods) {
			klog.ErrorS(err, "Application failed to create all needed pods", "application", applicationKey, "want", desiredAddition, "got", len(createdPods))
			return errors.NewAggregate(createErrors)
		}
	} else if desiredAddition < 0 {
		desiredSubstraction := desiredAddition * -1
		if desiredSubstraction > applicationBurst {
			desiredSubstraction = applicationBurst
		}
		klog.InfoS("Deleting pods", "application", applicationKey, "substraction", desiredSubstraction)

		// Choose which Pods to delete, preferring those in earlier phases of startup.
		deleteErrors := []error{}
		appPool := am.getOrCreateApplicationPool(applicationKey)
		podsToDelete := am.getPodsToBeDelete(appPool, desiredSubstraction)
		for _, pod := range podsToDelete {
			err := am.deleteApplicationPod(appPool, pod, false)
			if err != nil {
				deleteErrors = append(deleteErrors, err)
			}

			if len(deleteErrors) > 0 {
				klog.ErrorS(err, "Application failed to delete all not needed pods", "application", applicationKey, "delete", desiredSubstraction, "failed", len(deleteErrors))
				return errors.NewAggregate(deleteErrors)
			}
		}
	}

	return nil
}

// getPodApplicationKey returns Application Key of pod using LabelFornaxCoreApplication
func (am *ApplicationManager) getPodApplicationKey(pod *v1.Pod) (string, error) {
	if applicationLabel, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreApplication]; !found {
		klog.Warningf("Pod %s does not have fornaxv1 application label:%s", util.Name(pod), fornaxv1.LabelFornaxCoreApplication)
		return "", nil
	} else {
		namespace, name, err := cache.SplitMetaNamespaceKey(applicationLabel)
		if err == nil {
			application, err := am.applicationLister.Applications(namespace).Get(name)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return "", nil
				}
				return "", err
			}

			applicationKey, err := cache.MetaNamespaceKeyFunc(application)
			if err != nil {
				// not supposed to get here, as application is namespaced always, just handle function err anyway
				klog.ErrorS(err, "Can not find application key", "application", application)
				return "", err
			}
			return applicationKey, nil
		} else {
			// check application with same label
			applications, err := am.applicationLister.List(labels.SelectorFromValidatedSet(labels.Set{fornaxv1.LabelFornaxCoreApplication: applicationLabel}))
			if err != nil {
				return "", err
			}
			var ownerApp *fornaxv1.Application
			if len(applications) > 1 {
				klog.Warning("More than one fornax application have same application label: %s", applicationLabel)
				// check pod ownerreferences
				if len(pod.GetOwnerReferences()) == 0 {
					klog.Warning("Pod %s does not have a valid owner reference, treat it as a orphan pod", util.Name(pod))
					return "", nil
				}

				for _, application := range applications {
					uid := application.GetObjectMeta().GetUID()
					for _, ref := range pod.GetOwnerReferences() {
						if uid == ref.UID {
							ownerApp = application
							break
						}
					}
					if ownerApp != nil {
						break
					}
				}
			} else {
				ownerApp = applications[0]
			}
			applicationKey, err := cache.MetaNamespaceKeyFunc(ownerApp)
			if err != nil {
				// not supposed to get here, as application is namespaced and should have name
				klog.ErrorS(err, "Can not find application key", "application", ownerApp)
				return "", err
			}
			return applicationKey, nil
		}
	}
}

// cleanupPodOfApplication if a application is being deleted,
// terminate all pods which are still alive and delete pods from application pod pool if it does not exist anymore in Pod Manager
// when alive pods reported as terminated by Node Agent, then application can be eventually deleted
func (am *ApplicationManager) cleanupPodOfApplication(pool *ApplicationPool) error {
	deleteErrors := []error{}
	podsToDelete := []string{}
	pods := pool.podList()
	for _, k := range pods {
		pod := am.podManager.FindPod(k.podName)
		if pod == nil {
			podsToDelete = append(podsToDelete, k.podName)
		} else {
			err := am.deleteApplicationPod(pool, pod, false)
			if err != nil {
				deleteErrors = append(deleteErrors, err)
			}
		}
	}

	// these pods can be delete just as no longer exist in Pod Manager
	for _, k := range podsToDelete {
		pool.deletePod(k)
	}

	if len(deleteErrors) != 0 {
		return fmt.Errorf("Some pods failed to be deleted, num=%d", len(deleteErrors))
	}
	return nil
}
