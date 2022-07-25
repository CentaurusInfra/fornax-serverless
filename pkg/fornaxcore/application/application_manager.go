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
	"context"
	"fmt"
	"reflect"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxclient "centaurusinfra.io/fornax-serverless/pkg/client/clientset/versioned"
	"centaurusinfra.io/fornax-serverless/pkg/client/informers/externalversions"
	listerv1 "centaurusinfra.io/fornax-serverless/pkg/client/listers/core/v1"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxpod "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	fornaxutil "centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	// The number of times we retry updating a Application's status.
	DefaultRecycleApplicationDuration = 10 * time.Second

	// The number of workers sync application
	DefaultNumOfApplicationWorkers = 4
)

// ApplicationManager is responsible for synchronizing Application objects stored
// in the system with actual running pods.
type ApplicationManager struct {
	schema.GroupVersionKind

	applicationLister      listerv1.ApplicationLister
	aplicationListerSynced cache.InformerSynced
	applicationIndexer     cache.Indexer

	// A pods of each pods grouped by application key
	applicationPods map[string]map[string]*v1.Pod
	apiServerClient fornaxclient.Interface
	podUpdates      chan interface{}
	podInformer     ie.NodeInfoProvider
	podManager      fornaxpod.PodManager

	syncHandler func(ctx context.Context, appKey string) error

	appAutoScaler ApplicationAutoScaler

	// Controllers that need to be synced
	queue workqueue.RateLimitingInterface
}

func NewApplicationManager(podManager fornaxpod.PodManager, podInformer ie.NodeInfoProvider, apiServerClient fornaxclient.Interface) *ApplicationManager {
	gvk := fornaxv1.SchemeGroupVersion.WithKind("Application")

	informerFactory := externalversions.NewSharedInformerFactory(apiServerClient, 10*time.Minute)
	applicationInformer := informerFactory.Core().V1().Applications()
	appc := &ApplicationManager{
		GroupVersionKind: gvk,
		applicationPods:  map[string]map[string]*v1.Pod{},
		apiServerClient:  apiServerClient,
		podUpdates:       make(chan interface{}),
		podInformer:      podInformer,
		podManager:       podManager,
		appAutoScaler:    NewApplicationAutoScaler(),
		queue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "fornaxv1.Application"),
	}

	applicationInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    appc.addApplication,
		UpdateFunc: appc.updateApplication,
		DeleteFunc: appc.deleteApplication,
	})
	applicationInformer.Informer().AddIndexers(cache.Indexers{
		"controllerUID": func(obj interface{}) ([]string, error) {
			application, ok := obj.(*fornaxv1.Application)
			if !ok {
				return []string{}, nil
			}
			controllerRef := metav1.GetControllerOf(application)
			if controllerRef == nil {
				return []string{}, nil
			}
			return []string{string(controllerRef.UID)}, nil
		},
	})
	appc.applicationIndexer = applicationInformer.Informer().GetIndexer()
	appc.applicationLister = applicationInformer.Lister()
	appc.aplicationListerSynced = applicationInformer.Informer().HasSynced

	appc.podInformer = podInformer
	appc.syncHandler = appc.syncApplication

	return appc
}

// Run sync and watchs application and pod.
func (appc *ApplicationManager) Run(ctx context.Context) {
	defer utilruntime.HandleCrash()
	defer appc.queue.ShutDown()

	klog.Info("Starting fornaxv1 application controller")
	defer klog.Info("Shutting down fornaxv1 application controller")

	if !cache.WaitForNamedCacheSync(appc.Kind, ctx.Done(), appc.aplicationListerSynced) {
		return
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				break
			case update := <-appc.podUpdates:
				if podUpdate, ok := update.(*ie.PodUpdate); ok {
					switch podUpdate.Update {
					case ie.PodUpdateTypeCreate:
						appc.addPod(podUpdate.Pod)
					case ie.PodUpdateTypeDelete:
						appc.deletePod(podUpdate.Pod)
					case ie.PodUpdateTypeUpdate:
						appc.addPod(podUpdate.Pod)
					case ie.PodUpdateTypeTerminate:
						appc.deletePod(podUpdate.Pod)
					}
				}

			}
		}
	}()
	for i := 0; i < DefaultNumOfApplicationWorkers; i++ {
		go wait.UntilWithContext(ctx, appc.worker, time.Second)
	}

}

// getPodApplication returns Application of pod
func (appc *ApplicationManager) getPodApplication(pod *v1.Pod) (*fornaxv1.Application, error) {
	if applicationLabel, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreApplication]; !found {
		klog.Warningf("Pod %s does not have fornaxv1 application label:%s", fornaxutil.UniquePodName(pod), fornaxv1.LabelFornaxCoreApplication)
		return nil, nil
	} else {
		namespace, name, err := cache.SplitMetaNamespaceKey(applicationLabel)
		if err == nil {
			application, err := appc.applicationLister.Applications(namespace).Get(name)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return nil, nil
				}
				return nil, err
			}
			return application, nil
		} else {
			// check application with same label
			applications, err := appc.applicationLister.List(labels.SelectorFromValidatedSet(labels.Set{fornaxv1.LabelFornaxCoreApplication: applicationLabel}))
			if err != nil {
				return nil, err
			}
			if len(applications) > 1 {
				klog.Warning("More than one fornax application have same application label: %s", applicationLabel)
				// check pod ownerreferences
				if len(pod.GetOwnerReferences()) == 0 {
					klog.Warning("Pod %s does not have a valid owner reference, treat it as a orphan pod", fornaxutil.UniquePodName(pod))
					return nil, nil
				}

				for _, application := range applications {
					uid := application.GetObjectMeta().GetUID()
					for _, ref := range pod.GetOwnerReferences() {
						if uid == ref.UID {
							return application, nil
						}
					}
				}
				// can not determine which application pod belongs to, treat it as orphan pod
				return nil, nil
			}

			return applications[0], nil
		}
	}
}

func (appc *ApplicationManager) enqueueApplication(application *fornaxv1.Application) {
	key, err := cache.MetaNamespaceKeyFunc(application)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", application, err))
		return
	}

	appc.queue.Add(key)
}

func (appc *ApplicationManager) addApplication(obj interface{}) {
	application := obj.(*fornaxv1.Application)
	klog.V(4).Infof("Adding %s %s/%s", appc.Kind, application.Namespace, application.Name)
	appc.enqueueApplication(application)
}

// callback when Application is updated
func (appc *ApplicationManager) updateApplication(old, cur interface{}) {
	oldCopy := old.(*fornaxv1.Application)
	newCopy := cur.(*fornaxv1.Application)

	if newCopy.UID != oldCopy.UID {
		key, err := cache.MetaNamespaceKeyFunc(oldCopy)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", oldCopy, err))
			return
		}
		appc.deleteApplication(cache.DeletedFinalStateUnknown{
			Key: key,
			Obj: oldCopy,
		})
	}

	if !reflect.DeepEqual(oldCopy.Spec, newCopy.Spec) {
		klog.InfoS("Application is updated", "old", oldCopy.Spec, "new", newCopy.Spec)
	}
	appc.enqueueApplication(newCopy)
}

func (appc *ApplicationManager) deleteApplication(obj interface{}) {
	rs, ok := obj.(*fornaxv1.Application)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		rs, ok = tombstone.Obj.(*fornaxv1.Application)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Application %#v", obj))
			return
		}
	}

	key, err := cache.MetaNamespaceKeyFunc(rs)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", rs, err))
		return
	}

	klog.V(4).Infof("Deleting %s %q", appc.Kind, key)

	appc.queue.Add(key)
}

// When a pod is created or updated, find application that manages it and add this pod reference to app pods map, enqueue application
func (appc *ApplicationManager) addPod(pod *v1.Pod) {
	if pod.DeletionTimestamp != nil {
		appc.deletePod(pod)
		return
	}

	var applicationKey string
	application, err := appc.getPodApplication(pod)
	if err != nil {
		return
		// TODO, requeue pod update
	}
	if application == nil {
		klog.InfoS("Pod does not belong to any app, this pod should be terminated", "pod", fornaxutil.UniquePodName(pod), "labels", pod.GetLabels())
		pod, err = appc.podManager.TerminatePod(pod)
		if err != nil {
			return
			// TODO, requeue pod update
		}
		// try to use pod's application label as application key
		if label, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreApplication]; found {
			applicationKey = label
		} else {
			return
		}
	} else {
		applicationKey, err = cache.MetaNamespaceKeyFunc(application)
		if err != nil {
			klog.ErrorS(err, "Can not find application key", "application", application)
			return
		}
	}

	podKey := fornaxutil.UniquePodName(pod)
	if pods, found := appc.applicationPods[applicationKey]; found {
		if _, found := pods[podKey]; !found {
			pods[podKey] = pod
		}
	} else {
		appc.applicationPods[applicationKey] = map[string]*v1.Pod{podKey: pod}
	}
	appc.enqueueApplication(application)
}

// When a pod is deleted, find application that manages it and remove pod reference from app pod map
func (appc *ApplicationManager) deletePod(pod *v1.Pod) {
	if pod.DeletionTimestamp != nil {
		klog.InfoS("Pod does not have deletion timestamp, skip", "pod", fornaxutil.UniquePodName(pod))
		appc.addPod(pod)
		return
	}
	klog.InfoS("Pod deleted", "pod", fornaxutil.UniquePodName(pod), "timestamp", pod.DeletionTimestamp)

	var applicationKey string
	application, err := appc.getPodApplication(pod)
	if err != nil {
		return
		// TODO, requeue pod update
	}
	if application == nil {
		klog.InfoS("Pod does not belong to any app, skip it", "pod", fornaxutil.UniquePodName(pod), "labels", pod.GetLabels())
		// try to use pod's application label as application key
		if label, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreApplication]; found {
			applicationKey = label
		} else {
			return
		}
	} else {
		applicationKey, err = cache.MetaNamespaceKeyFunc(application)
		if err != nil {
			klog.ErrorS(err, "Can not find application key", "application", application)
			// not supposed to get here, as application already got
			return
		}
	}

	podKey := fornaxutil.UniquePodName(pod)
	if pods, found := appc.applicationPods[applicationKey]; found {
		delete(pods, podKey)
	}
	// enqueue application to evaluate application status
	appc.enqueueApplication(application)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (appc *ApplicationManager) worker(ctx context.Context) {
	for appc.processNextWorkItem(ctx) {
	}
}

func (appc *ApplicationManager) processNextWorkItem(ctx context.Context) bool {
	key, quit := appc.queue.Get()
	if quit {
		return false
	}
	defer appc.queue.Done(key)

	err := appc.syncHandler(ctx, key.(string))
	if err == nil {
		appc.queue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("sync %q failed with %v", key, err))
	appc.queue.AddRateLimited(key)

	return true
}

func (appc *ApplicationManager) syncApplicationPods(ctx context.Context, numOfDesiredPod int, activePods []*v1.Pod, application *fornaxv1.Application) error {
	diff := len(activePods) - numOfDesiredPod
	applicationBurst := util.ApplicationBurst(application)
	applicationKey, err := cache.MetaNamespaceKeyFunc(application)
	if err != nil {
		klog.ErrorS(err, "Couldn't get key for application", "application", application)
		return nil
	}

	if diff < 0 {
		diff *= -1
		if diff > applicationBurst {
			diff = applicationBurst
		}
		klog.InfoS("Creating pods", "application", applicationKey, "desired", numOfDesiredPod, "creating", diff)
		createdPods := []*v1.Pod{}
		createErrors := []error{}
		for i := 0; i < diff; i++ {
			// assign a different name,
			podTemplate := appc.getPodApplicationPodTemplate(application)
			podTemplate.Name = fmt.Sprintf("%s-%d-%d", podTemplate.Name, time.Now().Unix(), i)
			podTemplate.GenerateName = podTemplate.Name
			pod, err := appc.podManager.CreatePod(podTemplate, "")
			if err != nil {
				if apierrors.HasStatusCause(err, v1.NamespaceTerminatingCause) {
					// if the namespace is being terminated, we don't have to do
					// anything because any creation will fail
					return nil
				}
				createErrors = append(createErrors, err)
				continue
			}
			createdPods = append(createdPods, pod)
		}

		if failedPods := diff - len(createdPods); failedPods > 0 {
			err = errors.NewAggregate(createErrors)
			klog.ErrorS(err, "Application failed to create all wanted pods", "application", applicationKey, "desired", diff, "got ", len(createdPods))
			appc.queue.AddAfter(applicationKey, DefaultRecycleApplicationDuration)
		}
		return err
	} else if diff > 0 {
		if diff > applicationBurst {
			diff = applicationBurst
		}
		klog.InfoS("Deleting pods", "application", applicationKey, "desired", numOfDesiredPod, "deleting", diff)

		// Choose which Pods to delete, preferring those in earlier phases of startup.
		podsToDelete := appc.getPodsToDelete(activePods, diff)
		deleteErrors := []error{}
		for _, pod := range podsToDelete {
			if pod, err := appc.podManager.TerminatePod(pod); err != nil {
				klog.ErrorS(err, "Failed to delete Pod", "application", applicationKey, "pod", fornaxutil.UniquePodName(pod))
			}
		}
		if len(deleteErrors) > 0 {
			return errors.NewAggregate(deleteErrors)
		}
	}

	return nil
}

// getPodApplicationPodTemplate will translate application spec to a pod spec, it also call node port manager to allocate node port for exported each container port
func (appc *ApplicationManager) getPodApplicationPodTemplate(application *fornaxv1.Application) *v1.Pod {
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "",
			APIVersion: "",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "",
			GenerateName:    "",
			Namespace:       application.Namespace,
			UID:             "",
			ResourceVersion: "0",
			Generation:      0,
			CreationTimestamp: metav1.Time{
				Time: time.Now(),
			},
			DeletionTimestamp:          nil,
			DeletionGracePeriodSeconds: application.DeletionGracePeriodSeconds,
			Labels: map[string]string{
				fornaxv1.LabelFornaxCoreApplication: util.UniqueApplicationName(application),
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
			Containers:                    []v1.Container{*application.Spec.Container.DeepCopy()},
			EphemeralContainers:           []v1.EphemeralContainer{},
			RestartPolicy:                 v1.RestartPolicyNever,
			TerminationGracePeriodSeconds: nil,
			ActiveDeadlineSeconds:         nil,
			DNSPolicy:                     v1.DNSNone,
			NodeSelector:                  map[string]string{},
			ServiceAccountName:            "",
			DeprecatedServiceAccount:      "",
			AutomountServiceAccountToken:  nil,
			NodeName:                      "",
			HostNetwork:                   false,
			HostPID:                       false,
			HostIPC:                       false,
			ShareProcessNamespace:         new(bool),
			SecurityContext:               &v1.PodSecurityContext{},
			ImagePullSecrets:              []v1.LocalObjectReference{},
			Hostname:                      "",
			Subdomain:                     "",
			Affinity:                      &v1.Affinity{},
			SchedulerName:                 "",
			Tolerations:                   []v1.Toleration{},
			HostAliases:                   []v1.HostAlias{},
			PriorityClassName:             "",
			Priority:                      nil,
			DNSConfig:                     nil,
			ReadinessGates:                []v1.PodReadinessGate{},
			RuntimeClassName:              nil,
			EnableServiceLinks:            nil,
			PreemptionPolicy:              nil,
			Overhead:                      map[v1.ResourceName]resource.Quantity{},
			TopologySpreadConstraints:     []v1.TopologySpreadConstraint{},
			SetHostnameAsFQDN:             nil,
			OS:                            nil,
		},
		Status: v1.PodStatus{},
	}
	return pod
}

// syncApplication will check the Application, and make sure running pod of application meet desired number according scaling policy
func (appc *ApplicationManager) syncApplication(ctx context.Context, applicationkey string) error {
	// startTime := time.Now()
	defer func() {
		klog.InfoS("Finished syncing application", "application", applicationkey)
		// TODO post metrics
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(applicationkey)
	if err != nil {
		return err
	}

	var numOfDesiredPod int
	application, err := appc.applicationLister.Applications(namespace).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			numOfDesiredPod = 0
			err = appc.cleanupDeletedApplicationPods(applicationkey)
			if err != nil {
				appc.queue.AddAfter(applicationkey, DefaultRecycleApplicationDuration)
				return err
			}
		}
		// resync this app update
		appc.queue.AddAfter(applicationkey, DefaultRecycleApplicationDuration)
		return err
	}

	var syncApplicationErr error
	if pods, found := appc.applicationPods[applicationkey]; found {
		if application.DeletionTimestamp == nil {
			activePods := []*v1.Pod{}
			for _, pod := range pods {
				if pod.DeletionTimestamp == nil {
					activePods = append(activePods, pod)
				}
			}
			numOfDesiredPod = appc.appAutoScaler.CalcDesiredPods(application, activePods)
			syncApplicationErr = appc.syncApplicationPods(ctx, numOfDesiredPod, activePods, application)
		} else {
			numOfDesiredPod = 0
			err = appc.cleanupDeletedApplicationPods(applicationkey)
			if err != nil {
				// pod cleanup error, recycle this app
				appc.queue.AddAfter(applicationkey, DefaultRecycleApplicationDuration)
			}
		}
	}

	application = application.DeepCopy()
	newStatus := appc.calculateStatus(application, numOfDesiredPod, syncApplicationErr)

	updatedApplication, err := appc.updateApplicationStatus(appc.apiServerClient.CoreV1().Applications(application.Namespace), application, newStatus)
	if err != nil {
		return err
	}

	// Resync the Application if there is error or desired number does not match available number
	if syncApplicationErr != nil || updatedApplication.Status.AvailableInstances != updatedApplication.Status.DesiredInstances {
		appc.queue.AddAfter(applicationkey, DefaultRecycleApplicationDuration)
	}
	return syncApplicationErr
}

func (appc *ApplicationManager) cleanupDeletedApplicationPods(applicationKey string) error {
	klog.Infof("application %s has been deleted", applicationKey)
	// cleanup app pods
	var terminatingPodErr error
	if pods, found := appc.applicationPods[applicationKey]; found {
		for _, pod := range pods {
			if pod.DeletionTimestamp != nil {
				_, terminatingPodErr = appc.podManager.TerminatePod(pod)
			}
		}
		if terminatingPodErr != nil {
			return terminatingPodErr
		}
	}
	return nil
}

func (appc *ApplicationManager) getPodsToDelete(pods []*v1.Pod, numOfDesiredDelete int) []*v1.Pod {
	if numOfDesiredDelete < len(pods) {
		// TODO, use application session usage to determine least used pod
	}
	return pods[:numOfDesiredDelete]
}
