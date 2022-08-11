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
	"github.com/google/uuid"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	k8spodutil "k8s.io/kubernetes/pkg/api/v1/pod"
)

const (
	// The number of times we retry updating a Application's status.
	DefaultApplicationSyncErrorRecycleDuration = 10 * time.Second

	DefaultApplicationDesiredStateRecycleDuration = 1 * time.Second

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
	applicationPodPools map[string]*ApplicationPodPool
	apiServerClient     fornaxclient.Interface
	podUpdates          chan interface{}
	podInformer         ie.PodInfoProvider
	podManager          fornaxpod.PodManager

	syncHandler func(ctx context.Context, appKey string) error

	appAutoScaler ApplicationAutoScaler

	queue workqueue.RateLimitingInterface
}

func NewApplicationManager(ctx context.Context, podManager fornaxpod.PodManager, nodeInfoProvider ie.NodeInfoProvider, apiServerClient fornaxclient.Interface) *ApplicationManager {
	gvk := fornaxv1.SchemeGroupVersion.WithKind("Application")

	informerFactory := externalversions.NewSharedInformerFactory(apiServerClient, 10*time.Minute)
	applicationInformer := informerFactory.Core().V1().Applications()
	appc := &ApplicationManager{
		GroupVersionKind:    gvk,
		applicationPodPools: map[string]*ApplicationPodPool{},
		apiServerClient:     apiServerClient,
		podUpdates:          make(chan interface{}, 100),
		podInformer:         podManager,
		podManager:          podManager,
		appAutoScaler:       NewApplicationAutoScaler(),
		queue:               workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "fornaxv1.Application"),
	}
	nodeInfoProvider.Watch(appc.podUpdates)

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

	appc.podInformer = podManager
	appc.podInformer.Watch(appc.podUpdates)
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
				if podUpdate, ok := update.(*ie.PodEvent); ok {
					// do not handle daemon pod update, as daemon pods are not managed by application, node manager handle it
					appc.handlePodUpdateEvent(podUpdate)
				}
			}
		}
	}()

	for i := 0; i < DefaultNumOfApplicationWorkers; i++ {
		go wait.UntilWithContext(ctx, appc.worker, time.Second)
	}

}

func (appc *ApplicationManager) PrintAppSummary() {
	klog.InfoS("app summary:", "#app", len(appc.applicationPodPools), "application update queue", appc.queue.Len())
	for k, v := range appc.applicationPodPools {
		klog.InfoS("app summary", "app", k, "#pod", v.pods.Len())
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

	if _, found := appc.applicationPodPools[applicationKey]; !found {
		appc.applicationPodPools[applicationKey] = NewApplicationPodPool()
	}
	appc.enqueueApplication(applicationKey)
}

// callback when Application is updated
func (appc *ApplicationManager) updateApplication(old, cur interface{}) {
	oldCopy := old.(*fornaxv1.Application)
	newCopy := cur.(*fornaxv1.Application)

	applicationKey, err := cache.MetaNamespaceKeyFunc(oldCopy)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", oldCopy, err))
		return
	}
	if newCopy.UID != oldCopy.UID {
		appc.deleteApplication(cache.DeletedFinalStateUnknown{
			Key: applicationKey,
			Obj: oldCopy,
		})
	}

	if reflect.DeepEqual(oldCopy, newCopy) {
		klog.InfoS("Application does not have update", "appliation", applicationKey)
		return
	}
	appc.enqueueApplication(applicationKey)
}

func (appc *ApplicationManager) deleteApplication(obj interface{}) {
	appliation, ok := obj.(*fornaxv1.Application)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("couldn't get object from tombstone %#v", obj)
			return
		}
		appliation, ok = tombstone.Obj.(*fornaxv1.Application)
		if !ok {
			klog.Errorf("tombstone contained object that is not a Application %#v", obj)
			return
		}
	}

	if appliation.DeletionTimestamp == nil {
		klog.Errorf("application does not have deletion timestamp %#v", obj)
		return
	}

	applicationKey, err := cache.MetaNamespaceKeyFunc(appliation)
	if err != nil {
		klog.ErrorS(err, "Couldn't get key for application", "appliation", appliation)
		return
	}

	klog.Infof("Deleting application %s", applicationKey)

	appc.queue.Add(applicationKey)
}

func (appc *ApplicationManager) handlePodUpdateEvent(podEvent *ie.PodEvent) {
	klog.InfoS("Received a pod event", "pod", util.UniquePodName(podEvent.Pod), "type", podEvent.Type, "phase", podEvent.Pod.Status.Phase, "condition", k8spodutil.IsPodReady(podEvent.Pod))
	if _, found := podEvent.Pod.Labels[fornaxv1.LabelFornaxCoreNodeDaemon]; !found {
		switch podEvent.Type {
		case ie.PodEventTypeCreate:
			appc.addPod(podEvent.Pod)
		case ie.PodEventTypeDelete:
			appc.deletePod(podEvent.Pod)
		case ie.PodEventTypeUpdate:
			appc.addPod(podEvent.Pod)
		case ie.PodEventTypeTerminate:
			appc.deletePod(podEvent.Pod)
		}
	}
}

// When a pod is created or updated, find application that manages it and add this pod reference to app pods map, enqueue application
func (appc *ApplicationManager) addPod(pod *v1.Pod) {
	if pod.DeletionTimestamp != nil {
		appc.deletePod(pod)
		return
	}

	podName := fornaxutil.UniquePodName(pod)
	applicationKey, err := appc.getPodApplicationKey(pod)
	if err != nil {
		klog.ErrorS(err, "Failed to get applicaion of pod, try best to use pod label", podName)
		return
	}

	if applicationKey == nil {
		klog.InfoS("Pod does not belong to any application, this pod should be terminated and deleted", "pod", podName, "labels", pod.GetLabels())
	} else {
		if pool, found := appc.applicationPodPools[*applicationKey]; found {
			pool.addItem(podName)
		} else {
			pool = NewApplicationPodPool()
			appc.applicationPodPools[*applicationKey] = pool
			pool.addItem(podName)
		}
		appc.enqueueApplication(*applicationKey)
	}
}

// When a pod is deleted, find application that manages it and remove pod reference from app pod map
func (appc *ApplicationManager) deletePod(pod *v1.Pod) {
	podName := fornaxutil.UniquePodName(pod)
	if pod.DeletionTimestamp == nil {
		klog.InfoS("Pod does not have deletion timestamp, or pod is still alive, should add it", "pod", podName)
		appc.addPod(pod)
		return
	}

	applicationKey, err := appc.getPodApplicationKey(pod)
	if err != nil {
		klog.ErrorS(err, "Can not find application for pod", "pod", podName)
	}

	if applicationKey == nil {
		klog.InfoS("Pod does not belong to any application, this pod should have been terminated", "pod", podName, "labels", pod.GetLabels())
	} else {
		if pool, found := appc.applicationPodPools[*applicationKey]; found {
			pool.deleteItem(podName)
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

	klog.InfoS("Application queue lengh", "length", appc.queue.Len())
	err := appc.syncHandler(ctx, key.(string))
	if err == nil {
		appc.queue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("sync %q failed with %v", key, err))
	appc.queue.AddRateLimited(key)

	return true
}

func (appc *ApplicationManager) deleteApplicationPod(applicationKey string, pod *v1.Pod) error {
	podName := fornaxutil.UniquePodName(pod)
	klog.InfoS("Delete a application pod", "application", applicationKey, "pod", podName)
	_, err := appc.podManager.TerminatePod(pod)
	if err != nil {
		if err != fornaxpod.PodNotFoundError {
			klog.ErrorS(err, "Failed to delete Pod", "application", applicationKey, "pod", podName)
			return err
		} else {
			if pool, found := appc.applicationPodPools[applicationKey]; found {
				pool.deleteItem(podName)
			}
		}
	}

	return nil
}

func (appc *ApplicationManager) createApplicationPod(application *fornaxv1.Application) (*v1.Pod, error) {

	podTemplate := appc.getPodApplicationPodTemplate(application)
	// assign a unique name and uuid for pod
	uid := uuid.New()
	name := fmt.Sprintf("%s-%s-%d", application.Name, rand.String(16), uid.ClockSequence())
	podTemplate.UID = types.UID(uid.String())
	podTemplate.Name = name
	podTemplate.GenerateName = podTemplate.Name
	pod, err := appc.podManager.AddPod(podTemplate, "")
	if err != nil {
		return nil, err
	}

	return pod, nil
}

func (appc *ApplicationManager) syncApplicationPods(ctx context.Context, numOfDesiredPod int, activePods []*v1.Pod, application *fornaxv1.Application) error {
	diff := int(numOfDesiredPod) - len(activePods)
	applicationKey, err := cache.MetaNamespaceKeyFunc(application)
	if err != nil {
		klog.ErrorS(err, "Couldn't get key for application", "application", application)
		return err
	}
	pool, found := appc.applicationPodPools[applicationKey]
	if !found {
		klog.ErrorS(err, "Couldn't find application pod pool", "application", applicationKey)
		return err
	}

	applicationBurst := util.ApplicationBurst(application)
	if diff > 0 {
		if diff > applicationBurst {
			diff = applicationBurst
		}
		klog.InfoS("Creating pods", "application", applicationKey, "desired", numOfDesiredPod, "active", len(activePods))
		createdPods := []*v1.Pod{}
		createErrors := []error{}
		for i := 0; i < diff; i++ {
			pod, err := appc.createApplicationPod(application)
			if err != nil {
				klog.ErrorS(err, "Create pod failed", "application", applicationKey)
				if apierrors.HasStatusCause(err, v1.NamespaceTerminatingCause) {
					return nil
				}
				createErrors = append(createErrors, err)
				continue
			}
			pool.addItem(util.UniquePodName(pod))
			createdPods = append(createdPods, pod)
		}

		if diff != len(createdPods) {
			klog.ErrorS(err, "Application failed to create all needed pods", "application", applicationKey, "want", diff, "got", len(createdPods))
			return errors.NewAggregate(createErrors)
		}
	} else if diff < 0 {
		diff *= -1
		if diff > applicationBurst {
			diff = applicationBurst
		}
		klog.InfoS("Deleting pods", "application", applicationKey, "desired", numOfDesiredPod, "active", len(activePods))

		// Choose which Pods to delete, preferring those in earlier phases of startup.
		deleteErrors := []error{}
		podsToDelete := appc.getPodsToDelete(applicationKey, activePods, diff)
		for _, pod := range podsToDelete {
			err := appc.deleteApplicationPod(applicationKey, pod)
			if err != nil {
				deleteErrors = append(deleteErrors, err)
			}
		}

		if len(deleteErrors) > 0 {
			klog.ErrorS(err, "Application failed to delete all not needed pods", "application", applicationKey, "delete", diff, "failed", len(deleteErrors))
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

	activePods := []*v1.Pod{}
	deletedPods := []string{}
	if pool, found := appc.applicationPodPools[applicationKey]; found {
		// lock applicationPool to avoid concurrent syncApplication
		pool.appmu.Lock()
		defer pool.appmu.Unlock()

		pods := pool.getPodNames()
		for _, k := range pods {
			pod := appc.podManager.FindPod(k)
			if pod == nil {
				deletedPods = append(deletedPods, k)
			} else if pod.DeletionTimestamp == nil {
				// exclude pods have been requested to delete from active pods to avoid double count
				activePods = append(activePods, pod)
			}
		}
		// remove deleted pods
		for _, v := range deletedPods {
			pool.deleteItem(v)
		}
		klog.InfoS("app summary", "app", applicationKey, "#pod", pool.pods.Len(), "#active", len(activePods), "#deleted", len(deletedPods))
	} else {
		return fmt.Errorf("Application %s pod pool does not exit", applicationKey)
	}

	var syncErr error
	var numOfDesiredPod int
	namespace, name, err := cache.SplitMetaNamespaceKey(applicationKey)
	if err != nil {
		klog.ErrorS(err, "Application key is not a valid meta namespace key, skip", "application", applicationKey)
		return err
	}
	application, syncErr := appc.applicationLister.Applications(namespace).Get(name)
	if syncErr != nil {
		if apierrors.IsNotFound(syncErr) {
			klog.InfoS("Application not found, remove all pods", "application", applicationKey)
			numOfDesiredPod = 0
			syncErr = appc.cleanupDeletedApplicationPods(applicationKey)
			if syncErr == nil {
				// do not calculate status for a not found application
				return nil
			}
		}
	} else if application != nil {
		application = application.DeepCopy()
		if application.DeletionTimestamp == nil {
			numOfDesiredPod = appc.appAutoScaler.CalcDesiredPods(application, len(activePods))
			syncErr = appc.syncApplicationPods(ctx, numOfDesiredPod, activePods, application)
		} else {
			klog.InfoS("Application is deleted", "application", applicationKey, "deletionTime", application.DeletionTimestamp)
			numOfDesiredPod = 0
			syncErr = appc.cleanupDeletedApplicationPods(applicationKey)
		}

		newStatus := appc.calculateStatus(application, numOfDesiredPod, syncErr)
		application, syncErr = appc.updateApplicationStatus(application, newStatus)
		if err != nil {
			klog.InfoS("Application status update failed", "application", applicationKey)
		}
	}

	// Resync the Application if there is error or desired number does not match available number
	if syncErr != nil {
		klog.ErrorS(syncErr, "Failed to sync application, requeue", "application", applicationKey, "next")
		appc.queue.AddAfter(applicationKey, DefaultApplicationSyncErrorRecycleDuration)
	} else {
		activeCount := application.Status.TotalInstances - application.Status.DeletingInstances
		desireCount := application.Status.DesiredInstances
		if activeCount != desireCount {
			klog.InfoS("Application does not meet desired state, requeue", "application", applicationKey, "desired", desireCount, "active", activeCount)
			appc.queue.AddAfter(applicationKey, DefaultApplicationDesiredStateRecycleDuration)
		}
	}

	return syncErr
}

func (appc *ApplicationManager) cleanupDeletedApplicationPods(applicationKey string) error {
	klog.Infof("Cleanup all pods of application %s", applicationKey)
	// cleanup app pods
	deleteErrors := []error{}
	podsToDelete := []string{}
	if pool, found := appc.applicationPodPools[applicationKey]; found {
		pods := pool.getPodNames()
		for _, k := range pods {
			pod := appc.podManager.FindPod(k)
			if pod == nil {
				podsToDelete = append(podsToDelete, k)
			} else {
				err := appc.deleteApplicationPod(applicationKey, pod)
				if err != nil {
					deleteErrors = append(deleteErrors, err)
				}
			}
		}

		// these pods can be delete just
		for _, k := range podsToDelete {
			appc.applicationPodPools[applicationKey].deleteItem(k)
		}

		if len(deleteErrors) != 0 {
			return fmt.Errorf("Some pods failed to be deleted, num=%d", len(deleteErrors))
		}
	}
	return nil
}
