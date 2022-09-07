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
	listerv1 "centaurusinfra.io/fornax-serverless/pkg/client/listers/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	// The number of times we retry updating a Application's status.
	UPDATE_RETRIES = 2
)

type ApplicationStatusUpdate struct {
	name   string
	status *fornaxv1.ApplicationStatus
}

type ApplicationStatusManager struct {
	applicationLister listerv1.ApplicationLister
	statusUpdate      chan ApplicationStatusUpdate
	appStatus         map[string]*fornaxv1.ApplicationStatus
}

func NewApplicationStatusManager(appLister listerv1.ApplicationLister) *ApplicationStatusManager {
	return &ApplicationStatusManager{
		applicationLister: appLister,
		statusUpdate:      make(chan ApplicationStatusUpdate, 500),
		appStatus:         map[string]*fornaxv1.ApplicationStatus{},
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
			case update := <-asm.statusUpdate:
				// it assume application status is calcualted by application sync is always sequencially
				// when there are multiple status of same application in channel ,
				// the later one is supposed to be newer status, so, we only keep newer status,
				// after received all updates in channel, update latest status of application
				asm.appStatus[update.name] = update.status
				klog.Infof("Try to update application", "status", *update.status)
				remainingLen := len(asm.statusUpdate)
				for i := 0; i < remainingLen; i++ {
					update := <-asm.statusUpdate
					asm.appStatus[update.name] = update.status
				}

				hasError := false
				updatedApps := []string{}
				for app, status := range asm.appStatus {
					if app == FornaxCore_ApplicationStatusManager_Retry {
						// FornaxCore_ApplicationStatusManager_Retry app is fake application sinal used to trigger retry
						continue
					}
					err := asm.updateApplicationStatus(app, status)
					if err == nil {
						updatedApps = append(updatedApps, app)
					} else {
						hasError = true
						klog.ErrorS(err, "Failed to update application status", "application", app)
					}
				}
				for _, v := range updatedApps {
					delete(asm.appStatus, v)
				}

				time.Sleep(50 * time.Millisecond)
				// a trick to retry, all failed status update are still in map, send a fake update to retry,
				// it's bit risky, if some guy put a lot of event into channel before we can put a retry signal, it will stuck
				// checking channel current length must be zero could mitigate a bit
				if hasError {
					if len(asm.statusUpdate) == 0 {
						asm.statusUpdate <- ApplicationStatusUpdate{
							name:   FornaxCore_ApplicationStatusManager_Retry,
							status: &fornaxv1.ApplicationStatus{},
						}
					}
				}
			}
		}
	}()
}

func (asm *ApplicationStatusManager) UpdateApplicationStatus(application *fornaxv1.Application, newStatus *fornaxv1.ApplicationStatus) {
	asm.statusUpdate <- ApplicationStatusUpdate{
		name:   util.Name(application),
		status: newStatus.DeepCopy(),
	}
	klog.Infof("Send application status message", "status", *newStatus, "len", len(asm.statusUpdate))
}

// updateApplicationStatus attempts to update the Status of the given Application and return updated Application
func (asm *ApplicationStatusManager) updateApplicationStatus(applicationKey string, newStatus *fornaxv1.ApplicationStatus) error {
	var updateErr error
	var updatedApplication *fornaxv1.Application

	apiServerClient := util.GetFornaxCoreApiClient()
	application, updateErr := GetApplication(apiServerClient, applicationKey)
	if updateErr != nil {
		if apierrors.IsNotFound(updateErr) {
			return nil
		}
		return updateErr
	}

	if reflect.DeepEqual(application.Status, *newStatus) {
		klog.Infof("Application status has no change", "newStatus", *newStatus, "oldStatus", application.Status)
		return nil
	}

	client := apiServerClient.CoreV1().Applications(application.Namespace)
	for i := 0; i <= UPDATE_RETRIES; i++ {
		klog.Infof(fmt.Sprintf("Updating application status for %s, ", util.Name(application)) +
			fmt.Sprintf("totalInstances %d->%d, ", application.Status.TotalInstances, newStatus.TotalInstances) +
			fmt.Sprintf("desiredInstances %d->%d, ", application.Status.DesiredInstances, newStatus.DesiredInstances) +
			fmt.Sprintf("pendingInstances %d->%d, ", application.Status.PendingInstances, newStatus.PendingInstances) +
			fmt.Sprintf("deletingInstances %d->%d, ", application.Status.DeletingInstances, newStatus.DeletingInstances) +
			fmt.Sprintf("runningInstances %d->%d, ", application.Status.RunningInstances, newStatus.RunningInstances) +
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
