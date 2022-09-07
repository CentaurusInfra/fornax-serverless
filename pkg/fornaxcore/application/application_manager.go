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

func GetApplication(apiServerClient fornaxclient.Interface, applicationKey string) (*fornaxv1.Application, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(applicationKey)
	if err != nil {
		klog.ErrorS(err, "Application key is not a valid meta namespace key, skip", "application", applicationKey)
		return nil, err
	}
	return apiServerClient.CoreV1().Applications(namespace).Get(context.Background(), name, metav1.GetOptions{})
}

func GetApplicationCache(lister listerv1.ApplicationLister, applicationKey string) (*fornaxv1.Application, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(applicationKey)
	if err != nil {
		klog.ErrorS(err, "Application key is not a valid meta namespace key, skip", "application", applicationKey)
		return nil, err
	}
	return lister.Applications(namespace).Get(name)
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
	updateChannel    chan interface{}
	podManager       ie.PodManagerInterface
	sessionManager   ie.SessionManagerInterface

	applicationStatusManager *ApplicationStatusManager

	syncHandler func(ctx context.Context, appKey string) error
}

// NewApplicationManager init ApplicationInformer and ApplicationSessionInformer,
// and start to listen to pod event from node
func NewApplicationManager(ctx context.Context, podManager ie.PodManagerInterface, sessionManager ie.SessionManagerInterface) *ApplicationManager {
	am := &ApplicationManager{
		applicationQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "fornaxv1.Application"),
		applicationPools: map[string]*ApplicationPool{},
		updateChannel:    make(chan interface{}, 500),
		podManager:       podManager,
		sessionManager:   sessionManager,
	}
	am.podManager.Watch(am.updateChannel)
	am.sessionManager.Watch(am.updateChannel)
	am.syncHandler = am.syncApplication

	return am
}

func (am *ApplicationManager) deleteApplicationPool(applicationKey string) {
	am.appmu.Lock()
	defer am.appmu.Unlock()
	delete(am.applicationPools, applicationKey)
}

func (am *ApplicationManager) applicationList() map[string]*ApplicationPool {
	am.appmu.RLock()
	defer am.appmu.RUnlock()
	apps := map[string]*ApplicationPool{}
	for k, v := range am.applicationPools {
		apps[k] = v
	}

	return apps
}

func (am *ApplicationManager) getApplicationPool(applicationKey string) *ApplicationPool {
	am.appmu.RLock()
	defer am.appmu.RUnlock()
	if pool, found := am.applicationPools[applicationKey]; found {
		return pool
	} else {
		return nil
	}
}

func (am *ApplicationManager) getOrCreateApplicationPool(applicationKey string) (pool *ApplicationPool) {
	pool = am.getApplicationPool(applicationKey)
	if pool == nil {
		am.appmu.Lock()
		defer am.appmu.Unlock()
		pool = NewApplicationPool()
		am.applicationPools[applicationKey] = pool
		return pool
	} else {
		return pool
	}
}

func (am *ApplicationManager) initApplicationInformer(ctx context.Context) {
	apiServerClient := util.GetFornaxCoreApiClient()
	appInformerFactory := externalversions.NewSharedInformerFactory(apiServerClient, 10*time.Minute)

	applicationInformer := appInformerFactory.Core().V1().Applications()
	applicationInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    am.onApplicationAddEvent,
		UpdateFunc: am.onApplicationUpdateEvent,
		DeleteFunc: am.onApplicationDeleteEvent,
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
	am.applicationIndexer = applicationInformer.Informer().GetIndexer()
	am.applicationLister = applicationInformer.Lister()
	am.aplicationListerSynced = applicationInformer.Informer().HasSynced
	am.applicationStatusManager = NewApplicationStatusManager(am.applicationLister)
	am.applicationStatusManager.Run(ctx)
	appInformerFactory.Start(ctx.Done())
}

func (am *ApplicationManager) initApplicationSessionInformer(ctx context.Context) {
	apiServerClient := util.GetFornaxCoreApiClient()
	sessionInformerFactory := externalversions.NewSharedInformerFactory(apiServerClient, 10*time.Minute)
	applicationSessionInformer := sessionInformerFactory.Core().V1().ApplicationSessions()
	applicationSessionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    am.onApplicationSessionAddEvent,
		UpdateFunc: am.onApplicationSessionUpdateEvent,
		DeleteFunc: am.onApplicationSessionDeleteEvent,
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
	am.applicationSessionIndexer = applicationSessionInformer.Informer().GetIndexer()
	am.applicationSessionLister = applicationSessionInformer.Lister()
	am.aplicationSessionListerSynced = applicationSessionInformer.Informer().HasSynced
	sessionInformerFactory.Start(ctx.Done())
}

// Run sync and watchs application, pod and session
func (am *ApplicationManager) Run(ctx context.Context) {
	klog.Info("Starting fornaxv1 application manager")

	am.initApplicationInformer(ctx)
	cache.WaitForNamedCacheSync(fornaxv1.ApplicationKind.Kind, ctx.Done(), am.aplicationListerSynced)

	am.initApplicationSessionInformer(ctx)
	cache.WaitForNamedCacheSync(fornaxv1.ApplicationSessionKind.Kind, ctx.Done(), am.aplicationSessionListerSynced)

	go func() {
		defer utilruntime.HandleCrash()
		defer am.applicationQueue.ShutDown()
		defer klog.Info("Shutting down fornaxv1 application manager")
		ticker := time.NewTicker(HouseKeepingDuration)

		for {
			select {
			case <-ctx.Done():
				break
			case update := <-am.updateChannel:
				if pe, ok := update.(*ie.PodEvent); ok {
					am.onPodEventFromNode(pe)
				}
				if se, ok := update.(*ie.SessionEvent); ok {
					am.onSessionEventFromNode(se)
				}
			case <-ticker.C:
				am.sessionHouseKeeping()
			}
		}
	}()

	for i := 0; i < DefaultNumOfApplicationWorkers; i++ {
		go wait.UntilWithContext(ctx, am.worker, time.Second)
	}
}

func (am *ApplicationManager) enqueueApplication(applicationKey string) {
	am.applicationQueue.Add(applicationKey)
}

// callback from Application informer when Application is created
func (am *ApplicationManager) onApplicationAddEvent(obj interface{}) {
	application := obj.(*fornaxv1.Application)
	applicationKey := util.Name(application)
	klog.Infof("Creating application %s", applicationKey)

	am.getOrCreateApplicationPool(applicationKey)
	am.enqueueApplication(applicationKey)
}

// callback from Application informer when Application is updated
func (am *ApplicationManager) onApplicationUpdateEvent(old, cur interface{}) {
	oldCopy := old.(*fornaxv1.Application)
	newCopy := cur.(*fornaxv1.Application)

	applicationKey := util.Name(newCopy)
	if newCopy.UID != oldCopy.UID {
		am.onApplicationDeleteEvent(cache.DeletedFinalStateUnknown{
			Key: applicationKey,
			Obj: oldCopy,
		})
	}

	// application status are update when session or pod changed, its own status change does not need sync,
	// only sync when deleting or spec change
	if (newCopy.DeletionTimestamp != nil && oldCopy.DeletionTimestamp == nil) || !reflect.DeepEqual(oldCopy.Spec, newCopy.Spec) {
		klog.Infof("Updating application %s", applicationKey)
		am.enqueueApplication(applicationKey)
	}
}

// callback from application informer when Application is deleted
func (am *ApplicationManager) onApplicationDeleteEvent(obj interface{}) {
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

	applicationKey := util.Name(appliation)
	klog.Infof("Deleting application %s", applicationKey)
	am.applicationQueue.Add(applicationKey)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (am *ApplicationManager) worker(ctx context.Context) {
	for am.processNextWorkItem(ctx) {
	}
}

func (am *ApplicationManager) processNextWorkItem(ctx context.Context) bool {
	key, quit := am.applicationQueue.Get()
	if quit {
		return false
	}
	defer am.applicationQueue.Done(key)

	klog.InfoS("Application queue lengh", "length", am.applicationQueue.Len())
	err := am.syncHandler(ctx, key.(string))
	if err == nil {
		am.applicationQueue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("sync %q failed with %v", key, err))
	am.applicationQueue.AddRateLimited(key)

	return true
}

func (am *ApplicationManager) cleanupDeletedApplication(applicationKey string) error {
	klog.InfoS("Cleanup a deleting Application, remove all session then pod", "application", applicationKey)
	pool := am.getApplicationPool(applicationKey)
	if pool != nil {
		if pool.sessionLength() > 0 {
			err := am.cleanupSessionOfApplication(applicationKey)
			if err != nil {
				return err
			}
		} else {
			err := am.cleanupPodOfApplication(applicationKey)
			if err != nil {
				return err
			}
		}

		// if a application does not have any pod or session, remove it from application pool to save memory
		if pool.podLength() == 0 && pool.sessionLength() == 0 {
			klog.InfoS("Application pool is empty, remove", "application", applicationKey)
			// remove this application from pool
			am.deleteApplicationPool(applicationKey)
		}
	}
	return nil
}

// syncApplication check application sessions and assign session to idle pods,
// it make sure running pod of application meet desired number according session state
// if a application is being deleted, it cleanup session and pods of this application
func (am *ApplicationManager) syncApplication(ctx context.Context, applicationKey string) error {
	klog.InfoS("Syncing application", "application", applicationKey)
	// startTime := time.Now()
	defer func() {
		klog.InfoS("Finished syncing application", "application", applicationKey)
		// TODO post metrics
	}()

	var syncErr error
	var numOfDesiredPod int
	var action fornaxv1.DeploymentAction
	application, syncErr := GetApplicationCache(am.applicationLister, applicationKey)
	if syncErr != nil {
		if apierrors.IsNotFound(syncErr) {
			syncErr = am.cleanupDeletedApplication(applicationKey)
			numOfDesiredPod = 0
			action = fornaxv1.DeploymentActionDeleteInstance
			if syncErr != nil {
				am.applicationQueue.AddAfter(applicationKey, DefaultApplicationSyncErrorRecycleDuration)
				return nil
			}
		}
	} else if application != nil {
		if application.DeletionTimestamp == nil {
			// 1, sync session assign pending session to exist running pods firstly and cleanup timedout sessions,
			syncErr = am.syncApplicationSessions(applicationKey, application)

			// 2, determine how many pods required for remaining pending sessions
			if syncErr == nil {
				occupiedPods, idlePendingPods, idleRunningPods := am.groupApplicationPods(applicationKey)
				numOfIdlePod := len(idleRunningPods) + len(idlePendingPods)
				numOfOccupiedPod := len(occupiedPods)
				_, numOfPendingSession := am.getTotalAndPendingSessionNum(applicationKey)
				numOfDesiredIdlePod := am.calculateDesiredIdlePods(application, numOfOccupiedPod, numOfIdlePod, numOfPendingSession)
				numOfDesiredPod = numOfOccupiedPod + numOfDesiredIdlePod
				klog.InfoS("Sync application pod", "pending sessions", numOfPendingSession, "#curr-pods", numOfOccupiedPod+numOfIdlePod, "idle-pods", numOfIdlePod, "desired-idle-pods", numOfDesiredIdlePod)
				if numOfDesiredIdlePod > numOfIdlePod {
					action = fornaxv1.DeploymentActionCreateInstance
				} else if numOfDesiredIdlePod < numOfIdlePod {
					action = fornaxv1.DeploymentActionDeleteInstance
				}
				syncErr = am.syncApplicationPods(applicationKey, application, numOfDesiredIdlePod, numOfIdlePod, append(idlePendingPods, idleRunningPods...))
			}
		} else {
			numOfDesiredPod = 0
			action = fornaxv1.DeploymentActionDeleteInstance
			syncErr = am.cleanupDeletedApplication(applicationKey)
		}

		newStatus := am.calculateStatus(application, numOfDesiredPod, action, syncErr)
		am.applicationStatusManager.UpdateApplicationStatus(application, newStatus)
	}

	// Resync the Application if there is error, if no error but total pods number does not meet desired number,
	// when event of pods created/deleted in this sync come back from nodes will trigger next sync, finally meet desired state
	if syncErr != nil {
		klog.ErrorS(syncErr, "Failed to sync application, requeue", "application", applicationKey)
		am.applicationQueue.AddAfter(applicationKey, DefaultApplicationSyncErrorRecycleDuration)
	}

	return syncErr
}

func (am *ApplicationManager) calculateDesiredIdlePods(application *fornaxv1.Application, occupiedPodNum, idlePodNum int, sessionNum int) int {
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

	numOfDesiredPod := desiredCount + occupiedPodNum
	// total number must between maximum and minmum instances
	if numOfDesiredPod <= int(application.Spec.ScalingPolicy.MinimumInstance) {
		desiredCount = int(application.Spec.ScalingPolicy.MinimumInstance) - occupiedPodNum
	} else if numOfDesiredPod >= int(application.Spec.ScalingPolicy.MaximumInstance) {
		desiredCount = int(application.Spec.ScalingPolicy.MaximumInstance) - occupiedPodNum
		// not able to add more, as already reach maxinum instances
		if desiredCount <= 0 {
			desiredCount = idlePodNum
		}
	}
	return desiredCount
}

func (am *ApplicationManager) calculateStatus(application *fornaxv1.Application, desiredCount int, action fornaxv1.DeploymentAction, deploymentErr error) *fornaxv1.ApplicationStatus {
	newStatus := application.Status.DeepCopy()
	applicationKey, err := cache.MetaNamespaceKeyFunc(application)
	if err != nil {
		return newStatus
	}

	var poolSummary ApplicationPodSummary
	if pool := am.getApplicationPool(applicationKey); pool != nil {
		poolSummary = pool.summaryPod(am.podManager)
	}

	if application.Status.DesiredInstances == int32(desiredCount) &&
		application.Status.TotalInstances == poolSummary.totalCount &&
		application.Status.IdleInstances == poolSummary.idleCount &&
		application.Status.DeletingInstances == poolSummary.deletingCount &&
		application.Status.PendingInstances == poolSummary.pendingCount &&
		application.Status.RunningInstances == poolSummary.runningCount {
		return newStatus
	}

	newStatus.DesiredInstances = int32(desiredCount)
	newStatus.TotalInstances = poolSummary.totalCount
	newStatus.PendingInstances = poolSummary.pendingCount
	newStatus.DeletingInstances = poolSummary.deletingCount
	newStatus.IdleInstances = poolSummary.idleCount
	newStatus.RunningInstances = poolSummary.runningCount

	// this will make status huge, and finally fail a etcd request, need to find another way to save these history
	// if action == fornaxv1.DeploymentActionCreateInstance || action == fornaxv1.DeploymentActionDeleteInstance {
	// 	if deploymentErr != nil {
	// 		newStatus.DeploymentStatus = fornaxv1.DeploymentStatusFailure
	// 	} else {
	// 		newStatus.DeploymentStatus = fornaxv1.DeploymentStatusSuccess
	// 	}
	//
	// 	message := fmt.Sprintf("deploy application instance, total: %d, desired: %d, pending: %d, deleting: %d, ready: %d, idle: %d",
	// 		newStatus.TotalInstances,
	// 		newStatus.DesiredInstances,
	// 		newStatus.PendingInstances,
	// 		newStatus.DeletingInstances,
	// 		newStatus.ReadyInstances,
	// 		newStatus.IdleInstances)
	//
	// 	if deploymentErr != nil {
	// 		message = fmt.Sprintf("%s, error: %s", message, deploymentErr.Error())
	// 	}
	//
	// 	deploymentHistory := fornaxv1.DeploymentHistory{
	// 		Action: action,
	// 		UpdateTime: metav1.Time{
	// 			Time: time.Now(),
	// 		},
	// 		Reason:  "sync application",
	// 		Message: message,
	// 	}
	// 	newStatus.History = append(newStatus.History, deploymentHistory)
	// }

	return newStatus
}

func (am *ApplicationManager) printAppSummary() {
	klog.InfoS("app summary:", "#app", len(am.applicationPools), "application update queue", am.applicationQueue.Len())
}
