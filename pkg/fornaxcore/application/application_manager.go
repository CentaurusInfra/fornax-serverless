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
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxstore "centaurusinfra.io/fornax-serverless/pkg/store"
	storefactory "centaurusinfra.io/fornax-serverless/pkg/store/factory"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	apistorage "k8s.io/apiserver/pkg/storage"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	// The number of times we retry updating an application's status.
	DefaultApplicationSyncErrorRecycleDuration = 10 * time.Second

	// The number of application workers
	DefaultNumOfApplicationWorkers = 4
)

type ApplicationPool struct {
	appName     string
	mu          sync.RWMutex
	podsByState map[ApplicationPodState]map[string]*ApplicationPod
	sessions    map[ApplicationSessionState]map[string]*ApplicationSession
}

func NewApplicationPool(appName string) *ApplicationPool {
	return &ApplicationPool{
		appName: appName,
		mu:      sync.RWMutex{},
		podsByState: map[ApplicationPodState]map[string]*ApplicationPod{
			PodStatePending:   {},
			PodStateIdle:      {},
			PodStateAllocated: {},
			PodStateDeleting:  {},
		},
		sessions: map[ApplicationSessionState]map[string]*ApplicationSession{
			SessionStatePending:  {},
			SessionStateStarting: {},
			SessionStateRunning:  {},
			SessionStateDeleting: {},
		},
	}
}

// ApplicationManager is responsible for synchronizing Application objects stored
// in the system with actual running pods.
type ApplicationManager struct {
	ctx context.Context
	mu  sync.RWMutex

	appKind          schema.GroupVersionKind
	appSessionKind   schema.GroupVersionKind
	applicationQueue workqueue.RateLimitingInterface

	applicationPools map[string]*ApplicationPool

	applicationStore fornaxstore.ApiStorageInterface
	appUpdateChannel <-chan fornaxstore.WatchEventWithOldObj

	podUpdateChannel     chan *ie.PodEvent
	podManager           ie.PodManagerInterface
	sessionManager       ie.SessionManagerInterface
	sessionUpdateChannel <-chan fornaxstore.WatchEventWithOldObj

	applicationStatusManager *ApplicationStatusManager
}

// NewApplicationManager init ApplicationInformer and ApplicationSessionInformer,
// and start to listen to pod event from node
func NewApplicationManager(ctx context.Context, podManager ie.PodManagerInterface, sessionManager ie.SessionManagerInterface, appStore fornaxstore.ApiStorageInterface) *ApplicationManager {
	am := &ApplicationManager{
		ctx:              ctx,
		applicationQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "fornaxv1.Application"),
		applicationPools: map[string]*ApplicationPool{},
		podUpdateChannel: make(chan *ie.PodEvent, 1000),
		podManager:       podManager,
		sessionManager:   sessionManager,
		applicationStore: appStore,
	}
	am.podManager.Watch(am.podUpdateChannel)

	return am
}

func (am *ApplicationManager) deleteApplicationPool(applicationKey string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	delete(am.applicationPools, applicationKey)
}

func (am *ApplicationManager) applicationList() map[string]*ApplicationPool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	apps := map[string]*ApplicationPool{}
	for k, v := range am.applicationPools {
		apps[k] = v
	}

	return apps
}

func (am *ApplicationManager) getApplicationPool(applicationKey string) *ApplicationPool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	if pool, found := am.applicationPools[applicationKey]; found {
		return pool
	} else {
		return nil
	}
}

func (am *ApplicationManager) getOrCreateApplicationPool(applicationKey string) (pool *ApplicationPool) {
	pool = am.getApplicationPool(applicationKey)
	if pool == nil {
		am.mu.Lock()
		defer am.mu.Unlock()
		pool = NewApplicationPool(applicationKey)
		am.applicationPools[applicationKey] = pool
		return pool
	} else {
		return pool
	}
}

func (am *ApplicationManager) initApplicationInformer(ctx context.Context) error {
	wi, err := am.applicationStore.WatchWithOldObj(ctx, fornaxv1.ApplicationGrvKey, apistorage.ListOptions{
		ResourceVersion:      "0",
		ResourceVersionMatch: "",
		Predicate:            apistorage.Everything,
		Recursive:            true,
		ProgressNotify:       true,
	})
	if err != nil {
		return err
	}
	am.appUpdateChannel = wi.ResultChanWithPrevobj()
	return nil
}

func (am *ApplicationManager) onApplicationEventFromStorage(we fornaxstore.WatchEventWithOldObj) {
	switch we.Type {
	case watch.Added:
		am.onApplicationAddEvent(we.Object)
	case watch.Modified:
		am.onApplicationUpdateEvent(we.OldObject, we.Object)
	case watch.Deleted:
		am.onApplicationDeleteEvent(we.Object)
	}
}

// Run sync and watchs application, pod and session
func (am *ApplicationManager) Run(ctx context.Context) {
	klog.Info("Starting fornaxv1 application manager")

	am.applicationStatusManager = NewApplicationStatusManager(am.applicationStore)
	am.initApplicationInformer(ctx)
	go func() {
		for {
			select {
			case <-ctx.Done():
				break
			case we := <-am.appUpdateChannel:
				am.onApplicationEventFromStorage(we)
			}
		}
	}()

	am.initApplicationSessionInformer(ctx)

	for i := 0; i < DefaultNumOfApplicationWorkers; i++ {
		go wait.UntilWithContext(ctx, am.worker, time.Second)

		go func() {
			defer klog.Info("Shutting down fornaxv1 application pod manager")
			for {
				select {
				case <-ctx.Done():
					break
				case update := <-am.podUpdateChannel:
					am.onPodEventFromNode(update)
				}
			}
		}()

		go func() {
			defer klog.Info("Shutting down fornaxv1 application session manager")
			for {
				select {
				case <-ctx.Done():
					break
				case we := <-am.sessionUpdateChannel:
					am.onSessionEventFromStorage(we)
				}
			}
		}()

	}

	go func() {
		defer utilruntime.HandleCrash()
		defer am.applicationQueue.ShutDown()
		ticker := time.NewTicker(HouseKeepingDuration)
		for {
			select {
			case <-ctx.Done():
				break
			case <-ticker.C:
				am.HouseKeeping()
			}
		}
	}()
	klog.Info("Fornaxv1 application manager started")
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
	// only sync when deleting or spec change
	// application status are updated when session or pod changed, pod and session status change will trigger sync in their own event
	if (newCopy.DeletionTimestamp != nil && oldCopy.DeletionTimestamp == nil) || !reflect.DeepEqual(oldCopy.Spec, newCopy.Spec) {
		klog.InfoS("Updating application", "app", applicationKey, "deleting", newCopy.DeletionTimestamp != nil)
		am.enqueueApplication(applicationKey)
	}
}

// callback from application informer when Application is deleted
func (am *ApplicationManager) onApplicationDeleteEvent(obj interface{}) {
	appliation := obj.(*fornaxv1.Application)
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

	err := am.syncApplication(ctx, key.(string))
	if err == nil {
		am.applicationQueue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("sync %q failed with %v", key, err))
	am.applicationQueue.AddRateLimited(key)

	return true
}

func (am *ApplicationManager) cleanupDeletedApplication(pool *ApplicationPool) error {
	klog.InfoS("Cleanup a deleting Application, close all remaining session then deleting pod", "application", pool.appName)
	numOfSession := pool.sessionLength()
	if numOfSession > 0 {
		err := am.cleanupSessionOfApplication(pool)
		if err != nil {
			return err
		}
	} else {
		err := am.cleanupPodOfApplication(pool)
		if err != nil {
			return err
		}
	}

	// if a application does not have any pod or session, remove it from application pool to save memory
	if pool.podLength() == 0 && pool.sessionLength() == 0 {
		klog.InfoS("No remaining pod and session for deleting application, cleanup is done", "application", pool.appName)
		am.deleteApplicationPool(pool.appName)
	}
	return nil
}

// syncApplication check application sessions and assign session to idle pods,
// it make sure running pod of application meet desired number according session state
// if a application is being deleted, it cleanup session and pods of this application
func (am *ApplicationManager) syncApplication(ctx context.Context, applicationKey string) (syncErr error) {
	klog.InfoS("Syncing application", "application", applicationKey)
	st := time.Now().UnixMicro()
	defer func() {
		et := time.Now().UnixMicro()
		klog.InfoS("Done syncing application", "application", applicationKey, "took-micro", et-st)
		// TODO post metrics
	}()
	pool := am.getApplicationPool(applicationKey)
	if pool == nil {
		return nil
	}

	var numOfDesiredPod int
	var action fornaxv1.DeploymentAction
	application, syncErr := storefactory.GetApplicationCache(am.applicationStore, applicationKey)
	if syncErr != nil {
		if apierrors.IsNotFound(syncErr) {
			numOfDesiredPod = 0
			action = fornaxv1.DeploymentActionDeleteInstance
			syncErr = am.cleanupDeletedApplication(pool)
			// as application is not found in storage, just return and skip update status
			if syncErr != nil {
				am.applicationQueue.AddAfter(applicationKey, DefaultApplicationSyncErrorRecycleDuration)
				return nil
			}
		}
	} else if application != nil {
		if application.DeletionTimestamp == nil {
			// 1, assign pending session to idle pods firstly and cleanup timedout and deleting sessions
			syncErr = am.deployApplicationSessions(pool, application)

			// 2, find how many more pods required for remaining pending sessions
			if syncErr == nil {
				sessionSummary := pool.summarySession()
				numOfOccupiedPod, numOfPendingPod, numOfIdlePod := pool.activePodNums()
				numOfUnoccupiedPod := numOfPendingPod + numOfIdlePod
				numOfPendingSession := sessionSummary.pendingCount
				numOfDesiredUnoccupiedPod := am.calculateDesiredIdlePods(application, numOfOccupiedPod, numOfUnoccupiedPod, numOfPendingSession)
				numOfDesiredPod = numOfOccupiedPod + numOfDesiredUnoccupiedPod
				klog.InfoS("Syncing application pod", "application", applicationKey, "pending-sessions", numOfPendingSession, "active-pods", numOfOccupiedPod+numOfUnoccupiedPod, "pending-pods", numOfPendingPod, "idle-pods", numOfIdlePod, "desired-pending+idle-pods", numOfDesiredUnoccupiedPod)
				if numOfDesiredUnoccupiedPod > numOfUnoccupiedPod {
					action = fornaxv1.DeploymentActionCreateInstance
				} else if numOfDesiredUnoccupiedPod < numOfUnoccupiedPod {
					action = fornaxv1.DeploymentActionDeleteInstance
				}
				// pending session will need pods immediately, the rest of pods can be created as a standby pod
				desiredAddition := numOfDesiredUnoccupiedPod - numOfUnoccupiedPod
				syncErr = am.deployApplicationPods(pool, application, desiredAddition)

				// take care of timeout and deleting pods
				am.pruneDeadPods(pool)
			}
		} else {
			numOfDesiredPod = 0
			action = fornaxv1.DeploymentActionDeleteInstance
			syncErr = am.cleanupDeletedApplication(pool)
		}

		newStatus := am.calculateStatus(pool, application, numOfDesiredPod, action, syncErr)
		am.applicationStatusManager.UpdateApplicationStatus(application, newStatus)
	}

	// Requeue the Application if there is error, if no error but total pods number does not meet desired number,
	// when event of pods created/deleted in this sync come back from nodes will trigger next sync, finally meet desired state
	if syncErr != nil {
		klog.ErrorS(syncErr, "Failed to sync application, requeue", "application", applicationKey)
		am.applicationQueue.AddAfter(applicationKey, DefaultApplicationSyncErrorRecycleDuration)
	}

	return syncErr
}

// pruneDeadPods check pending and deleting pods,
// if a pending pod was assigned to a node but did not report back after a time limit, deleted it
// if a deleting pod was assigned to node to teminate but node did not report back after a time limit, deleted it again until node report back or dead node deleted
// any pod can not find from pod manager will be just deleted
func (am *ApplicationManager) pruneDeadPods(pool *ApplicationPool) {
	pendingPods := pool.podListOfState(PodStatePending)
	deletingPods := pool.podListOfState(PodStateDeleting)
	pendingTimeoutCutoff := time.Now().Add(-1 * DefaultPodPendingTimeoutDuration)
	for _, ap := range pendingPods {
		// double check with podManager to avoid race condition when pod is reported back just this moment
		pod := am.podManager.FindPod(ap.podName)
		if pod != nil {
			if util.PodIsRunning(pod) {
				continue
			}
			// only delete pending pods which scheduled to a node but did not report back by node,
			// delete a pod not scheduled to a node, will trigger creating another unnecessary pending pod
			if pod.Status.StartTime != nil && pod.Status.StartTime.Time.Before(pendingTimeoutCutoff) && len(pod.Status.HostIP) > 0 {
				am.deleteApplicationPod(pool, ap.podName)
			}
		} else {
			pool.deletePod(ap.podName)
		}
	}

	for _, ap := range deletingPods {
		// retry
		am.deleteApplicationPod(pool, ap.podName)
	}
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

func (am *ApplicationManager) calculateStatus(pool *ApplicationPool, application *fornaxv1.Application, desiredCount int, action fornaxv1.DeploymentAction, deploymentErr error) *fornaxv1.ApplicationStatus {
	newStatus := application.Status.DeepCopy()
	poolSummary := pool.summaryPod(am.podManager)

	if application.Status.DesiredInstances == int32(desiredCount) &&
		application.Status.TotalInstances == poolSummary.totalCount &&
		application.Status.IdleInstances == poolSummary.idleCount &&
		application.Status.DeletingInstances == poolSummary.deletingCount &&
		application.Status.PendingInstances == poolSummary.pendingCount &&
		application.Status.AllocatedInstances == poolSummary.occupiedCount {
		return newStatus
	}

	newStatus.DesiredInstances = int32(desiredCount)
	newStatus.TotalInstances = poolSummary.totalCount
	newStatus.PendingInstances = poolSummary.pendingCount
	newStatus.DeletingInstances = poolSummary.deletingCount
	newStatus.IdleInstances = poolSummary.idleCount
	newStatus.AllocatedInstances = poolSummary.occupiedCount

	// this will make status huge, and finally fail a etcd request, need to find another way to save these history
	// if action == fornaxv1.DeploymentActionCreateInstance || action == fornaxv1.DeploymentActionDeleteInstance {
	//  if deploymentErr != nil {
	//    newStatus.DeploymentStatus = fornaxv1.DeploymentStatusFailure
	//  } else {
	//    newStatus.DeploymentStatus = fornaxv1.DeploymentStatusSuccess
	//  }
	//
	//  message := fmt.Sprintf("deploy application instance, total: %d, desired: %d, pending: %d, deleting: %d, ready: %d, idle: %d",
	//    newStatus.TotalInstances,
	//    newStatus.DesiredInstances,
	//    newStatus.PendingInstances,
	//    newStatus.DeletingInstances,
	//    newStatus.ReadyInstances,
	//    newStatus.IdleInstances)
	//
	//  if deploymentErr != nil {
	//    message = fmt.Sprintf("%s, error: %s", message, deploymentErr.Error())
	//  }
	//
	//  deploymentHistory := fornaxv1.DeploymentHistory{
	//    Action: action,
	//    UpdateTime: metav1.Time{
	//      Time: time.Now(),
	//    },
	//    Reason:  "sync application",
	//    Message: message,
	//  }
	//  newStatus.History = append(newStatus.History, deploymentHistory)
	// }

	return newStatus
}

func (am *ApplicationManager) HouseKeeping() error {
	appPools := am.applicationList()
	klog.Info("Application house keeping")
	for appKey, pool := range appPools {
		podSummary := pool.summaryPod(am.podManager)
		sessionSummary := pool.summarySession()
		klog.InfoS("Application summary", "app", appKey, "pod", podSummary, "session", sessionSummary)

		// starting and pending session could timeout
		if sessionSummary.pendingCount > 0 || sessionSummary.startingCount > 0 || sessionSummary.deletingCount > 0 {
			// let syncApplication to take care of timeout sessions, session deployment logic should be handled only in application worker to avoid race condition
			am.enqueueApplication(appKey)
		}

		if podSummary.pendingCount > 0 || podSummary.deletingCount > 0 {
			am.enqueueApplication(appKey)
		}
	}

	return nil
}
