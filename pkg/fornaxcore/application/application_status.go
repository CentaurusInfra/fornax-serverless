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
	listerv1 "centaurusinfra.io/fornax-serverless/pkg/client/listers/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	// The number of times we retry updating a Application's status.
	UPDATE_RETRIES = 2
)

type ApplicationStatusChangeMap struct {
	changes map[string]*fornaxv1.ApplicationStatus
	mu      sync.Mutex
}

// it assume application status is calcualted by application sync is always sequencially
// when there are multiple status of same application,
// the later one is supposed to be newer status, so, we only keep newer status,
// update applciation with latest status of application
func (ascm *ApplicationStatusChangeMap) addStatusChange(name string, status *fornaxv1.ApplicationStatus, replace bool) {
	ascm.mu.Lock()
	defer ascm.mu.Unlock()
	if _, found := ascm.changes[name]; found {
		if replace {
			ascm.changes[name] = status
		}
	} else {
		ascm.changes[name] = status
	}
}

func (ascm *ApplicationStatusChangeMap) getAndRemoveStatusChangeSnapshot() map[string]*fornaxv1.ApplicationStatus {
	ascm.mu.Lock()
	defer ascm.mu.Unlock()
	updatedStatus := map[string]*fornaxv1.ApplicationStatus{}
	for name, v := range ascm.changes {
		updatedStatus[name] = v
	}

	for name := range updatedStatus {
		delete(ascm.changes, name)
	}
	return updatedStatus
}

type ApplicationStatusManager struct {
	applicationLister listerv1.ApplicationLister
	statusUpdateCh    chan string
	statusChanges     *ApplicationStatusChangeMap
	kubeConfig        *rest.Config
}

func NewApplicationStatusManager(appLister listerv1.ApplicationLister) *ApplicationStatusManager {
	return &ApplicationStatusManager{
		applicationLister: appLister,
		statusUpdateCh:    make(chan string, 500),
		statusChanges: &ApplicationStatusChangeMap{
			changes: map[string]*fornaxv1.ApplicationStatus{},
			mu:      sync.Mutex{},
		},
		kubeConfig: util.GetFornaxCoreKubeConfig(),
	}
}

// Run receive application status from channel and update applications use api service client
func (asm *ApplicationStatusManager) Run(ctx context.Context) {
	klog.Info("Starting fornaxv1 application status manager")

	go func() {
		defer klog.Info("Shutting down fornaxv1 application status manager")

		FornaxCore_ApplicationStatusManager_Retry := "FornaxCore_ApplicationStatusManager_StatusUpdate_Retry"
		for {
			select {
			case <-ctx.Done():
				break
			case <-asm.statusUpdateCh:
				// consume all signal in channel
				remainingLen := len(asm.statusUpdateCh)
				for i := 0; i < remainingLen; i++ {
					<-asm.statusUpdateCh
				}
				sessionStatuses := asm.statusChanges.getAndRemoveStatusChangeSnapshot()
				failedApps := map[string]*fornaxv1.ApplicationStatus{}
				for app, status := range sessionStatuses {
					err := asm.updateApplicationStatus(app, status)
					if err != nil {
						failedApps[app] = status
						klog.ErrorS(err, "Failed to update application status", "application", app)
					}
				}

				// a trick to retry, all failed status update are still in map, send a fake update to retry,
				// it's bit risky, if some guy put a lot of event into channel before we can put a retry signal, it will stuck
				// checking channel current length must be zero could mitigate a bit
				for name, status := range failedApps {
					// use false replace flag, when there is new status in map, do not replace it
					asm.statusChanges.addStatusChange(name, status, false)
					if len(asm.statusUpdateCh) == 0 {
						asm.statusUpdateCh <- FornaxCore_ApplicationStatusManager_Retry
					}
				}
				if len(failedApps) > 0 {
					time.Sleep(10 * time.Millisecond)
				}
			}
		}
	}()
}

func (asm *ApplicationStatusManager) UpdateApplicationStatus(application *fornaxv1.Application, newStatus *fornaxv1.ApplicationStatus) {
	asm.statusChanges.addStatusChange(util.Name(application), newStatus, true)
	if len(asm.statusUpdateCh) == 0 {
		asm.statusUpdateCh <- util.Name(application)
	}
	return
}

// updateApplicationStatus attempts to update the Status of the given Application and return updated Application
func (asm *ApplicationStatusManager) updateApplicationStatus(applicationKey string, newStatus *fornaxv1.ApplicationStatus) error {
	var updateErr error
	var updatedApplication *fornaxv1.Application

	apiServerClient := util.GetFornaxCoreApiClient(asm.kubeConfig)
	application, updateErr := GetApplication(apiServerClient, applicationKey)
	if updateErr != nil {
		if apierrors.IsNotFound(updateErr) {
			return nil
		}
		return updateErr
	}

	if reflect.DeepEqual(application.Status, *newStatus) {
		return nil
	}

	client := apiServerClient.CoreV1().Applications(application.Namespace)
	for i := 0; i <= UPDATE_RETRIES; i++ {
		klog.Infof(fmt.Sprintf("Updating application status for %s, ", util.Name(application)) +
			fmt.Sprintf("totalInstances %d->%d, ", application.Status.TotalInstances, newStatus.TotalInstances) +
			fmt.Sprintf("desiredInstances %d->%d, ", application.Status.DesiredInstances, newStatus.DesiredInstances) +
			fmt.Sprintf("pendingInstances %d->%d, ", application.Status.PendingInstances, newStatus.PendingInstances) +
			fmt.Sprintf("deletingInstances %d->%d, ", application.Status.DeletingInstances, newStatus.DeletingInstances) +
			fmt.Sprintf("allocatedInstances %d->%d, ", application.Status.AllocatedInstances, newStatus.AllocatedInstances) +
			fmt.Sprintf("idleInstances %d->%d, ", application.Status.IdleInstances, newStatus.IdleInstances))

		updatedApplication = application.DeepCopy()
		updatedApplication.Status = *newStatus
		updatedApplication, updateErr = client.UpdateStatus(context.TODO(), updatedApplication, metav1.UpdateOptions{})
		if updateErr == nil {
			break
		} else {
			// set it again, returned updatedApplication could be polluted if there is error
			updatedApplication = application.DeepCopy()
			updatedApplication.Status = *newStatus
		}

		if updateErr != nil {
			return updateErr
		}
	}

	// update finalizer
	finalizers := updatedApplication.Finalizers
	if newStatus.TotalInstances == 0 {
		util.RemoveFinalizer(&updatedApplication.ObjectMeta, fornaxv1.FinalizerApplicationPod)
	} else {
		util.AddFinalizer(&updatedApplication.ObjectMeta, fornaxv1.FinalizerApplicationPod)
	}
	if len(updatedApplication.Finalizers) != len(finalizers) {
		for i := 0; i <= UPDATE_RETRIES; i++ {
			_, updateErr = client.Update(context.TODO(), updatedApplication, metav1.UpdateOptions{})
			if updateErr == nil {
				break
			}
		}
	}
	return updateErr
}
