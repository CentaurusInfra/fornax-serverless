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
	"math"
	"reflect"
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxclient "centaurusinfra.io/fornax-serverless/pkg/client/clientset/versioned"
	"centaurusinfra.io/fornax-serverless/pkg/client/informers/externalversions"
	listerv1 "centaurusinfra.io/fornax-serverless/pkg/client/listers/core/v1"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxpod "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/pod"
	fornaxsession "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/session"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	// The number of times we retry updating a Application's status.
	DefaultApplicationSyncErrorRecycleDuration = 10 * time.Second

	DefaultApplicationDesiredStateRecycleDuration = 1 * time.Second

	// The number of workers sync application
	DefaultNumOfApplicationWorkers = 4
)

type ApplicationPool struct {
	mu       sync.RWMutex
	pods     map[string]*ApplicationPod
	sessions map[string]*fornaxv1.ApplicationSession
}

func NewApplicationPool() *ApplicationPool {
	return &ApplicationPool{
		mu:       sync.RWMutex{},
		pods:     map[string]*ApplicationPod{},
		sessions: map[string]*fornaxv1.ApplicationSession{},
	}
}

// ApplicationManager is responsible for synchronizing Application objects stored
// in the system with actual running pods.
type ApplicationManager struct {
	appKind        schema.GroupVersionKind
	appSessionKind schema.GroupVersionKind

	applicationLister      listerv1.ApplicationLister
	aplicationListerSynced cache.InformerSynced
	applicationIndexer     cache.Indexer
	applicationQueue       workqueue.RateLimitingInterface

	applicationSessionLister      listerv1.ApplicationSessionLister
	aplicationSessionListerSynced cache.InformerSynced
	applicationSessionIndexer     cache.Indexer

	appmu sync.RWMutex

	// A pool of pods grouped by application key
	applicationPools map[string]*ApplicationPool
	apiServerClient  fornaxclient.Interface
	watcher          chan interface{}
	podManager       fornaxpod.PodManager
	sessionManager   fornaxsession.SessionManager

	syncHandler func(ctx context.Context, appKey string) error
}

// NewApplicationManager init ApplicationInformer and ApplicationSessionInformer,
// and start to listen to pod event from node
func NewApplicationManager(ctx context.Context, podManager fornaxpod.PodManager, sessionManager fornaxsession.SessionManager, apiServerClient fornaxclient.Interface) *ApplicationManager {
	appc := &ApplicationManager{
		applicationQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "fornaxv1.Application"),
		applicationPools: map[string]*ApplicationPool{},
		apiServerClient:  apiServerClient,
		watcher:          make(chan interface{}, 100),
		podManager:       podManager,
		sessionManager:   sessionManager,
	}
	appc.podManager.Watch(appc.watcher)
	appc.syncHandler = appc.syncApplication

	return appc
}

func (appc *ApplicationManager) deleteApplicationPool(applicationKey string) {
	appc.appmu.Lock()
	defer appc.appmu.Unlock()
	delete(appc.applicationPools, applicationKey)
}

func (appc *ApplicationManager) applicationList() []*ApplicationPool {
	appc.appmu.RLock()
	defer appc.appmu.RUnlock()
	apps := []*ApplicationPool{}
	for _, v := range appc.applicationPools {
		apps = append(apps, v)
	}

	return apps
}

func (appc *ApplicationManager) getApplicationPool(applicationKey string) *ApplicationPool {
	appc.appmu.RLock()
	defer appc.appmu.RUnlock()
	if pool, found := appc.applicationPools[applicationKey]; found {
		return pool
	} else {
		return nil
	}
}

func (appc *ApplicationManager) getOrCreateApplicationPool(applicationKey string) (pool *ApplicationPool) {
	pool = appc.getApplicationPool(applicationKey)
	if pool == nil {
		appc.appmu.Lock()
		defer appc.appmu.Unlock()
		pool = NewApplicationPool()
		appc.applicationPools[applicationKey] = pool
		return pool
	} else {
		return pool
	}
}

func (appc *ApplicationManager) initApplicationInformer(ctx context.Context, apiServerClient fornaxclient.Interface) {
	appInformerFactory := externalversions.NewSharedInformerFactory(apiServerClient, 10*time.Minute)

	applicationInformer := appInformerFactory.Core().V1().Applications()
	applicationInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    appc.onApplicationAddEvent,
		UpdateFunc: appc.onApplicationUpdateEvent,
		DeleteFunc: appc.onApplicationDeleteEvent,
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
	appInformerFactory.Start(ctx.Done())
}

func (appc *ApplicationManager) initApplicationSessionInformer(ctx context.Context, apiServerClient fornaxclient.Interface) {
	sessionInformerFactory := externalversions.NewSharedInformerFactory(apiServerClient, 10*time.Minute)
	applicationSessionInformer := sessionInformerFactory.Core().V1().ApplicationSessions()
	applicationSessionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    appc.onApplicationSessionAddEvent,
		UpdateFunc: appc.onApplicationSessionUpdateEvent,
		DeleteFunc: appc.onApplicationSessionDeleteEvent,
	})
	applicationSessionInformer.Informer().AddIndexers(cache.Indexers{
		"controllerUID": func(obj interface{}) ([]string, error) {
			session, ok := obj.(*fornaxv1.ApplicationSession)
			if !ok {
				return []string{}, nil
			}
			controllerRef := metav1.GetControllerOf(session)
			if controllerRef == nil {
				return []string{}, nil
			}
			return []string{string(controllerRef.UID)}, nil
		},
	})
	appc.applicationSessionIndexer = applicationSessionInformer.Informer().GetIndexer()
	appc.applicationSessionLister = applicationSessionInformer.Lister()
	appc.aplicationSessionListerSynced = applicationSessionInformer.Informer().HasSynced
	sessionInformerFactory.Start(ctx.Done())
}

// Run sync and watchs application, pod and session
func (appc *ApplicationManager) Run(ctx context.Context) {
	klog.Info("Starting fornaxv1 application manager")

	appc.initApplicationInformer(ctx, appc.apiServerClient)
	cache.WaitForNamedCacheSync(fornaxv1.ApplicationKind.Kind, ctx.Done(), appc.aplicationListerSynced)

	appc.initApplicationSessionInformer(ctx, appc.apiServerClient)
	cache.WaitForNamedCacheSync(fornaxv1.ApplicationSessionKind.Kind, ctx.Done(), appc.aplicationSessionListerSynced)

	go func() {
		defer utilruntime.HandleCrash()
		defer appc.applicationQueue.ShutDown()
		defer klog.Info("Shutting down fornaxv1 application manager")
		ticker := time.NewTicker(DefaultSessionOpenTimeoutDuration)

		for {
			select {
			case <-ctx.Done():
				break
			case update := <-appc.watcher:
				if pe, ok := update.(*ie.PodEvent); ok {
					appc.onPodEventFromNode(pe)
				}
			case <-ticker.C:
				appc.sessionHouseKeeping()
			}
		}
	}()

	for i := 0; i < DefaultNumOfApplicationWorkers; i++ {
		go wait.UntilWithContext(ctx, appc.worker, time.Second)
	}

}

func (appc *ApplicationManager) PrintAppSummary() {
	klog.InfoS("app summary:", "#app", len(appc.applicationPools), "application update queue", appc.applicationQueue.Len())
}

func (appc *ApplicationManager) enqueueApplication(applicationKey string) {
	appc.applicationQueue.Add(applicationKey)
}

// callback from Application informer when Application is created
func (appc *ApplicationManager) onApplicationAddEvent(obj interface{}) {
	application := obj.(*fornaxv1.Application)
	applicationKey := util.ResourceName(application)
	klog.Infof("Adding application %s", applicationKey)

	appc.getOrCreateApplicationPool(applicationKey)
	appc.enqueueApplication(applicationKey)
}

// callback from Application informer when Application is updated
func (appc *ApplicationManager) onApplicationUpdateEvent(old, cur interface{}) {
	oldCopy := old.(*fornaxv1.Application)
	newCopy := cur.(*fornaxv1.Application)

	applicationKey := util.ResourceName(newCopy)
	if newCopy.UID != oldCopy.UID {
		appc.onApplicationDeleteEvent(cache.DeletedFinalStateUnknown{
			Key: applicationKey,
			Obj: oldCopy,
		})
	}

	if reflect.DeepEqual(oldCopy, newCopy) {
		return
	}
	appc.enqueueApplication(applicationKey)
}

// callback from application informer when Application is deleted
func (appc *ApplicationManager) onApplicationDeleteEvent(obj interface{}) {
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
		appliation.DeletionTimestamp = util.NewCurrentMetaTime()
	}

	applicationKey := util.ResourceName(appliation)

	klog.Infof("Deleting application %s", applicationKey)

	appc.applicationQueue.Add(applicationKey)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (appc *ApplicationManager) worker(ctx context.Context) {
	for appc.processNextWorkItem(ctx) {
	}
}

func (appc *ApplicationManager) processNextWorkItem(ctx context.Context) bool {
	key, quit := appc.applicationQueue.Get()
	if quit {
		return false
	}
	defer appc.applicationQueue.Done(key)

	klog.InfoS("Application queue lengh", "length", appc.applicationQueue.Len())
	err := appc.syncHandler(ctx, key.(string))
	if err == nil {
		appc.applicationQueue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("sync %q failed with %v", key, err))
	appc.applicationQueue.AddRateLimited(key)

	return true
}

func (appc *ApplicationManager) getApplication(applicationKey string) (*fornaxv1.Application, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(applicationKey)
	if err != nil {
		klog.ErrorS(err, "Application key is not a valid meta namespace key, skip", "application", applicationKey)
		return nil, err
	}
	return appc.applicationLister.Applications(namespace).Get(name)
}

func (appc *ApplicationManager) cleanupDeletedApplication(applicationKey string) error {
	klog.InfoS("Application is deleted, remove all pods and sessions", "application", applicationKey)
	err := appc.cleanupPodOfApplication(applicationKey)
	if err != nil {
		return err
	}
	err = appc.cleanupSessionOfApplication(applicationKey)
	if err != nil {
		return err
	}
	return nil
}

// syncApplication check application sessions and assign session to idle pods,
// it make sure running pod of application meet desired number according session state
// if a application is being deleted, it cleanup session and pods of this application
func (appc *ApplicationManager) syncApplication(ctx context.Context, applicationKey string) error {
	klog.InfoS("Syncing application", "application", applicationKey)
	// startTime := time.Now()
	defer func() {
		klog.InfoS("Finished syncing application", "application", applicationKey)
		// TODO post metrics
	}()

	var syncErr error
	var numOfDesiredPod int
	var action fornaxv1.DeploymentAction
	application, syncErr := appc.getApplication(applicationKey)
	if syncErr != nil {
		if apierrors.IsNotFound(syncErr) {
			syncErr = appc.cleanupDeletedApplication(applicationKey)
			numOfDesiredPod = 0
			action = fornaxv1.DeploymentActionDeleteInstance
			if syncErr != nil {
				// do not calculate status for a not found application, just retry
				appc.applicationQueue.AddAfter(applicationKey, DefaultApplicationSyncErrorRecycleDuration)
				return nil
			}
		}
	} else if application != nil {
		if application.DeletionTimestamp == nil {
			// sync session assign pending session to exist running pods firstly and cleanup timedout sessions,
			syncErr = appc.syncApplicationSessions(application, applicationKey)
			// determine how many pods required for remaining sessions
			if syncErr == nil {
				idlePods := appc.getIdleApplicationPods(applicationKey)
				totalSessionNum, pendingSessionNum := appc.getTotalAndPendingSessionNum(applicationKey)
				numOfDesiredPod = appc.CalculateDesiredPods(application, len(idlePods), pendingSessionNum)
				klog.InfoS("Application summary", "total sessions", totalSessionNum, "pending sessions", pendingSessionNum, "idle pods", len(idlePods), "desired idle pods", numOfDesiredPod)
				if numOfDesiredPod > len(idlePods) {
					action = fornaxv1.DeploymentActionCreateInstance
				} else if numOfDesiredPod < len(idlePods) {
					action = fornaxv1.DeploymentActionDeleteInstance
				}
				syncErr = appc.syncApplicationPods(application, numOfDesiredPod, idlePods)
			}
		} else {
			klog.InfoS("Application is deleted", "application", applicationKey, "deletionTime", application.DeletionTimestamp)
			numOfDesiredPod = 0
			action = fornaxv1.DeploymentActionDeleteInstance
			syncErr = appc.cleanupDeletedApplication(applicationKey)
		}

		newStatus := appc.calculateStatus(application, numOfDesiredPod, action, syncErr)
		if newStatus != nil && !reflect.DeepEqual(application.Status, *newStatus) {
			_, err := appc.updateApplicationStatus(application, newStatus)
			if err != nil {
				klog.InfoS("Application status update failed", "application", applicationKey)
				syncErr = err
			}
		}
	}

	// Resync the Application if there is error or does not meet desired number
	if syncErr != nil {
		klog.ErrorS(syncErr, "Failed to sync application, requeue", "application", applicationKey, "next")
		appc.applicationQueue.AddAfter(applicationKey, DefaultApplicationSyncErrorRecycleDuration)
	} else {
		// if a application does not have any pod or session, remove it from application pool to save memory
		pool := appc.getApplicationPool(applicationKey)
		if pool != nil && pool.podLength() == 0 && pool.sessionLength() == 0 {
			// remove this application from pool
			appc.deleteApplicationPool(applicationKey)
		}
	}

	return syncErr
}

func (appc *ApplicationManager) CalculateDesiredPods(application *fornaxv1.Application, idlePodNum int, sessionNum int) int {
	desiredCount := idlePodNum
	sessionSupported := idlePodNum
	idleSessionNum := int(sessionSupported) - sessionNum

	if application.Spec.ScalingPolicy.ScalingPolicyType == fornaxv1.ScalingPolicyTypeIdleSessionNum {
		lowThresholdNum := int(application.Spec.ScalingPolicy.IdleSessionNumThreshold.LowWaterMark)
		if idleSessionNum < lowThresholdNum {
			desiredCount = idlePodNum + int(math.Ceil(float64(lowThresholdNum-idleSessionNum)))
		}

		highThresholdNum := int(application.Spec.ScalingPolicy.IdleSessionNumThreshold.HighWaterMark)
		if idleSessionNum > highThresholdNum {
			desiredCount = idlePodNum - int(math.Floor(float64(idleSessionNum-highThresholdNum)))
		}
	}

	if application.Spec.ScalingPolicy.ScalingPolicyType == fornaxv1.ScalingPolicyTypeIdleSessionPercent {
		lowThreshold := int(application.Spec.ScalingPolicy.IdleSessionPercentThreshold.LowWaterMark)
		lowThresholdNum := sessionSupported * lowThreshold / 100
		if idleSessionNum < lowThreshold {
			desiredCount = idlePodNum + int(math.Ceil(float64(lowThresholdNum-idleSessionNum)))
		}

		highThreshold := int(application.Spec.ScalingPolicy.IdleSessionPercentThreshold.HighWaterMark)
		highThresholdNum := sessionSupported * highThreshold / 100
		if idleSessionNum > highThreshold {
			desiredCount = idlePodNum - int(math.Floor(float64(idleSessionNum-highThresholdNum)))
		}
	}

	// desiredCount must between maximum and minmum instances
	if desiredCount <= int(application.Spec.ScalingPolicy.MinimumInstance) {
		return int(application.Spec.ScalingPolicy.MinimumInstance)
	} else if desiredCount >= int(application.Spec.ScalingPolicy.MaximumInstance) {
		return int(application.Spec.ScalingPolicy.MaximumInstance)
	} else {
		return desiredCount
	}
}
