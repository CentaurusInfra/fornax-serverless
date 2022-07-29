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
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxclient "centaurusinfra.io/fornax-serverless/pkg/client/clientset/versioned"
	"centaurusinfra.io/fornax-serverless/pkg/client/informers/externalversions"
	listerv1 "centaurusinfra.io/fornax-serverless/pkg/client/listers/core/v1"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxpod "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	fornaxutil "centaurusinfra.io/fornax-serverless/pkg/util"
	"github.com/google/uuid"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	k8spodutil "k8s.io/kubernetes/pkg/api/v1/pod"
)

const (
	// The number of times we retry updating a Application's status.
	DefaultRecycleApplicationDuration = 10 * time.Second

	// The number of workers sync application
	DefaultNumOfApplicationWorkers = 4
)

type ApplicationPool struct {
	mu   sync.RWMutex
	pods map[string]bool
}

func (pool *ApplicationPool) addItem(identifier string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.pods[identifier] = true
}

func (pool *ApplicationPool) deleteItem(identifier string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.pods, identifier)
}

// ApplicationManager is responsible for synchronizing Application objects stored
// in the system with actual running pods.
type ApplicationManager struct {
	schema.GroupVersionKind

	applicationLister      listerv1.ApplicationLister
	aplicationListerSynced cache.InformerSynced
	applicationIndexer     cache.Indexer

	// A pods of each pods grouped by application key
	applicationPods map[string]*ApplicationPool
	apiServerClient fornaxclient.Interface
	podUpdates      chan interface{}
	podInformer     ie.NodeInfoProvider
	podManager      fornaxpod.PodManager

	syncHandler func(ctx context.Context, appKey string) error

	appAutoScaler ApplicationAutoScaler

	queue workqueue.RateLimitingInterface
}

func NewApplicationManager(ctx context.Context, podManager fornaxpod.PodManager, nodeInfoProvider ie.NodeInfoProvider, apiServerClient fornaxclient.Interface) *ApplicationManager {
	gvk := fornaxv1.SchemeGroupVersion.WithKind("Application")

	informerFactory := externalversions.NewSharedInformerFactory(apiServerClient, 10*time.Minute)
	applicationInformer := informerFactory.Core().V1().Applications()
	appc := &ApplicationManager{
		GroupVersionKind: gvk,
		applicationPods:  map[string]*ApplicationPool{},
		apiServerClient:  apiServerClient,
		podUpdates:       make(chan interface{}, 100),
		podInformer:      nodeInfoProvider,
		podManager:       podManager,
		appAutoScaler:    NewApplicationAutoScaler(),
		queue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "fornaxv1.Application"),
	}
	nodeInfoProvider.WatchNode(appc.podUpdates)

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

	appc.podInformer = nodeInfoProvider
	appc.syncHandler = appc.syncApplication

	informerFactory.Start(ctx.Done())
	return appc
}

// Run sync and watchs application and pod.
func (appc *ApplicationManager) Run(ctx context.Context) {

	klog.Info("Starting fornaxv1 application manager")

	go func() {
		defer utilruntime.HandleCrash()
		defer appc.queue.ShutDown()
		defer klog.Info("Shutting down fornaxv1 application manager")

		if !cache.WaitForNamedCacheSync(appc.Kind, ctx.Done(), appc.aplicationListerSynced) {
			return
		}

		for {
			select {
			case <-ctx.Done():
				break
			case update := <-appc.podUpdates:
				if podUpdate, ok := update.(*ie.PodUpdate); ok {
					// do not handle daemon pod update, as daemon pods are not managed by application, node manager handle it
					klog.InfoS("Received a pod update", "pod", util.UniquePodName(podUpdate.Pod), "update", podUpdate.Update, "state", podUpdate.Pod.Status.Phase, "condition", k8spodutil.IsPodReady(podUpdate.Pod))
					if _, found := podUpdate.Pod.Labels[fornaxv1.LabelFornaxCoreNodeDaemon]; !found {
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
				appc.PrintAppSummary()
			}
		}
	}()

	for i := 0; i < DefaultNumOfApplicationWorkers; i++ {
		go wait.UntilWithContext(ctx, appc.worker, time.Second)
	}

}

func (appc *ApplicationManager) PrintAppSummary() {
	klog.InfoS("app summary:", "#app", len(appc.applicationPods), "application update queue", appc.queue.Len())
	for k, v := range appc.applicationPods {
		klog.InfoS("app summary", "app", k, "#pod", len(v.pods))
	}
}

// getPodApplicationKey returns Application Key of pod using LabelFornaxCoreApplication
func (appc *ApplicationManager) getPodApplicationKey(pod *v1.Pod) (*string, error) {
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

			applicationKey, err := cache.MetaNamespaceKeyFunc(application)
			if err != nil {
				klog.ErrorS(err, "Can not find application key", "application", application)
				// not supposed to get here, as application is namespaced and should have name
				return nil, err
			}
			return &applicationKey, nil
		} else {
			// check application with same label
			applications, err := appc.applicationLister.List(labels.SelectorFromValidatedSet(labels.Set{fornaxv1.LabelFornaxCoreApplication: applicationLabel}))
			if err != nil {
				return nil, err
			}
			var ownerApp *fornaxv1.Application
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
				return nil, err
			}
			return &applicationKey, nil
		}
	}
}

func (appc *ApplicationManager) enqueueApplication(applicationKey string) {
	appc.queue.Add(applicationKey)
}

func (appc *ApplicationManager) addApplication(obj interface{}) {
	application := obj.(*fornaxv1.Application)
	applicationKey, err := cache.MetaNamespaceKeyFunc(application)
	if err != nil {
		klog.ErrorS(err, "Can not get application key", "application", application)
	}
	klog.Infof("Adding application %s", applicationKey)
	appc.enqueueApplication(applicationKey)
}

// callback when Application is updated
func (appc *ApplicationManager) updateApplication(old, cur interface{}) {
	oldCopy := old.(*fornaxv1.Application)
	newCopy := cur.(*fornaxv1.Application)

	key, err := cache.MetaNamespaceKeyFunc(oldCopy)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", oldCopy, err))
		return
	}
	if newCopy.UID != oldCopy.UID {
		appc.deleteApplication(cache.DeletedFinalStateUnknown{
			Key: key,
			Obj: oldCopy,
		})
	}

	if !reflect.DeepEqual(oldCopy.Spec, newCopy.Spec) {
		klog.InfoS("Application is updated", "old", oldCopy.Spec, "new", newCopy.Spec)
	}
	appc.enqueueApplication(key)
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

	klog.Infof("Deleting %s %q", appc.Kind, key)

	appc.queue.Add(key)
}

// When a pod is created or updated, find application that manages it and add this pod reference to app pods map, enqueue application
func (appc *ApplicationManager) addPod(pod *v1.Pod) {
	if pod.DeletionTimestamp != nil {
		appc.deletePod(pod)
		return
	}

	applicationKey, err := appc.getPodApplicationKey(pod)
	if err != nil {
		klog.ErrorS(err, "Failed to get applicaion of pod, try best to use pod label", fornaxutil.UniquePodName(pod))
		return
		// TODO, requeue pod update
	}

	if applicationKey == nil {
		klog.InfoS("Pod does not belong to any application, this pod should be terminated and deleted", "pod", fornaxutil.UniquePodName(pod), "labels", pod.GetLabels())
		// 	TODO, requeue pod update
		// pod, err = appc.podManager.DeletePod(pod)
		// if err != nil {
		// 	klog.ErrorS(err, "Failed to delete pod", "pod", fornaxutil.UniquePodName(pod))
		// }
	} else {
		podKey := fornaxutil.UniquePodName(pod)
		if v, found := appc.applicationPods[*applicationKey]; found {
			if _, found := v.pods[podKey]; !found {
				v.addItem(podKey)
			}
		} else {
			appc.applicationPods[*applicationKey] = &ApplicationPool{
				mu:   sync.RWMutex{},
				pods: map[string]bool{podKey: true},
			}
		}
		appc.enqueueApplication(*applicationKey)
	}
}

// When a pod is deleted, find application that manages it and remove pod reference from app pod map
func (appc *ApplicationManager) deletePod(pod *v1.Pod) {
	if pod.DeletionTimestamp == nil {
		klog.InfoS("Pod does not have deletion timestamp, should add it", "pod", fornaxutil.UniquePodName(pod))
		appc.addPod(pod)
		return
	}

	klog.InfoS("Application pod deleted", "pod", fornaxutil.UniquePodName(pod), "deletion timestamp", pod.DeletionTimestamp)
	applicationKey, err := appc.getPodApplicationKey(pod)
	if err != nil {
		klog.ErrorS(err, "Can not find application for pod", "pod", fornaxutil.UniquePodName(pod))
	}

	if applicationKey == nil {
		klog.InfoS("Pod does not belong to any application, this pod should have been terminated", "pod", fornaxutil.UniquePodName(pod), "labels", pod.GetLabels())
	} else {
		podKey := fornaxutil.UniquePodName(pod)
		if pool, found := appc.applicationPods[*applicationKey]; found {
			pool.deleteItem(podKey)
		}
		// enqueue application to evaluate application status
		appc.enqueueApplication(*applicationKey)
	}
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
	diff := int(numOfDesiredPod) - len(activePods)
	applicationKey, err := cache.MetaNamespaceKeyFunc(application)
	if err != nil {
		klog.ErrorS(err, "Couldn't get key for application", "application", application)
		return err
	}
	applicationBurst := util.ApplicationBurst(application)
	if diff > 0 {
		if diff > applicationBurst {
			diff = applicationBurst
		}
		klog.InfoS("Creating pods", "application", applicationKey, "desired", numOfDesiredPod, "available", len(activePods), "creating", diff)
		createdPods := []*v1.Pod{}
		createErrors := []error{}
		for i := 0; i < diff; i++ {
			podTemplate := appc.getPodApplicationPodTemplate(application)
			// assign a unique name and uuid for pod
			uid := uuid.New()
			podTemplate.UID = types.UID(uid.String())
			podTemplate.Name = fmt.Sprintf("%s-%d-%d", application.Name, uid.ClockSequence(), len(activePods)+i)
			podTemplate.GenerateName = podTemplate.Name
			pod, err := appc.podManager.CreatePod(podTemplate, "")
			if err != nil {
				klog.ErrorS(err, "Create pod failed", "application", applicationKey)
				if apierrors.HasStatusCause(err, v1.NamespaceTerminatingCause) {
					return nil
				}
				createErrors = append(createErrors, err)
				continue
			}
			createdPods = append(createdPods, pod)
			if podsMap, found := appc.applicationPods[applicationKey]; found {
				podsMap.addItem(util.UniquePodName(pod))
			} else {
				podsMap = &ApplicationPool{
					mu:   sync.RWMutex{},
					pods: map[string]bool{},
				}
				podsMap.addItem(util.UniquePodName(pod))
				appc.applicationPods[applicationKey] = podsMap
			}
		}
		klog.InfoS("Created Pods for application", "application", applicationKey, "created", len(createdPods))

		if failedPods := diff - len(createdPods); failedPods > 0 {
			err = errors.NewAggregate(createErrors)
			klog.ErrorS(err, "Application failed to create all wanted pods", "application", applicationKey, "want", diff, "got", len(createdPods))
			appc.queue.AddAfter(applicationKey, DefaultRecycleApplicationDuration)
		}
		return err
	} else if diff < 0 {
		diff *= -1
		if diff > applicationBurst {
			diff = applicationBurst
		}
		klog.InfoS("Deleting pods", "application", applicationKey, "desired", numOfDesiredPod, "available", len(activePods), "deleting", diff)

		// Choose which Pods to delete, preferring those in earlier phases of startup.
		podsToDelete := appc.getPodsToDelete(applicationKey, activePods, diff)
		deleteErrors := []error{}
		for _, pod := range podsToDelete {
			// pod not scheduled yet, it's safe to just delete, if there is a race condition with pod scheduler,
			// pod will be recreated and terminate again
			klog.InfoS("Deleting pod", "application", applicationKey, "pod", util.UniquePodName(pod))
			_, err := appc.podManager.DeletePod(pod)
			if err != nil && err != fornaxpod.PodNotFoundError {
				deleteErrors = append(deleteErrors, err)
				klog.ErrorS(err, "Failed to delete Pod", "application", applicationKey, "pod", fornaxutil.UniquePodName(pod))
			} else {
				appc.applicationPods[applicationKey].deleteItem(util.UniquePodName(pod))
			}
		}

		if len(deleteErrors) > 0 {
			return errors.NewAggregate(deleteErrors)
		}
	}

	return nil
}

// syncApplication will check the Application, and make sure running pod of application meet desired number according scaling policy
func (appc *ApplicationManager) syncApplication(ctx context.Context, applicationKey string) error {
	klog.InfoS("Syncing application", "application", applicationKey)
	// startTime := time.Now()
	defer func() {
		klog.InfoS("Finished syncing application", "application", applicationKey)
		// TODO post metrics
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(applicationKey)
	if err != nil {
		return err
	}

	activePods := []*v1.Pod{}
	deletedPods := []string{}
	if pool, found := appc.applicationPods[applicationKey]; found {
		for k := range pool.pods {
			pod := appc.podManager.FindPod(k)
			if pod == nil || util.IsPodTerminated(pod) {
				deletedPods = append(deletedPods, k)
			} else {
				activePods = append(activePods, pod)
			}
		}
		for _, v := range deletedPods {
			pool.deleteItem(v)
		}
	}

	// remove deleted pods
	var syncApplicationErr error
	var numOfDesiredPod int
	application, err := appc.applicationLister.Applications(namespace).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("Application not found, remove all pods", "application", applicationKey)
			numOfDesiredPod = 0
			err = appc.cleanupDeletedApplicationPods(applicationKey)
			if err != nil {
				syncApplicationErr = err
			}
		}
	} else {
		if application.DeletionTimestamp == nil {
			numOfDesiredPod = appc.appAutoScaler.CalcDesiredPods(application, len(activePods))
			syncApplicationErr = appc.syncApplicationPods(ctx, numOfDesiredPod, activePods, application)
		} else {
			klog.InfoS("Application is deleted", "application", applicationKey, "deletionTime", application.DeletionTimestamp)
			numOfDesiredPod = 0
			err = appc.cleanupDeletedApplicationPods(applicationKey)
			if err != nil {
				syncApplicationErr = err
			}
		}
	}

	if application != nil {
		application = application.DeepCopy()
		newStatus := appc.calculateStatus(application, numOfDesiredPod, syncApplicationErr)
		klog.InfoS("Application status after sync",
			"application", applicationKey,
			"desired", newStatus.DesiredInstances,
			"available", newStatus.AvailableInstances,
			"ready", newStatus.ReadyInstances,
			"idle", newStatus.IdleInstances)
		application, err = appc.updateApplicationStatus(appc.apiServerClient.CoreV1().Applications(application.Namespace), application, newStatus)
		if err != nil {
			klog.InfoS("Application status update failed", "application", applicationKey)
		}

		// Resync the Application if there is error or desired number does not match available number
		if syncApplicationErr != nil {
			klog.ErrorS(syncApplicationErr, "Failed to sync application, requeue", "application", applicationKey)
			appc.queue.AddAfter(applicationKey, DefaultRecycleApplicationDuration)
		} else {
			if application.Status.AvailableInstances != application.Status.DesiredInstances {
				klog.InfoS("Sync application did not meet desired state, requeue", "application", applicationKey)
			}
		}
	} else {
		klog.InfoS("Application is completed deleted", "application", applicationKey)
	}

	return syncApplicationErr
}

func (appc *ApplicationManager) cleanupDeletedApplicationPods(applicationKey string) error {
	klog.Infof("Cleanup all pods of application %s", applicationKey)
	// cleanup app pods
	deletePodErrors := []error{}
	podsToDelete := []string{}
	if pool, found := appc.applicationPods[applicationKey]; found {
		for k := range pool.pods {
			pod := appc.podManager.FindPod(k)
			if pod == nil {
				podsToDelete = append(podsToDelete, k)
			} else {
				_, err := appc.podManager.DeletePod(pod)
				if err != nil && err != fornaxpod.PodNotFoundError {
					deletePodErrors = append(deletePodErrors, err)
				} else {
					podsToDelete = append(podsToDelete, k)
				}
			}
		}

		// these pods can be delete just
		for _, k := range podsToDelete {
			appc.applicationPods[applicationKey].deleteItem(k)
		}

		if len(deletePodErrors) != 0 {
			return fmt.Errorf("Some pods failed to be deleted, num=%d", len(deletePodErrors))
		}
	}
	return nil
}
