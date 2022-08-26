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
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	// The number of times we retry updating a Application's status.
	UPDATE_RETRIES = 1
)

// updateApplicationStatus attempts to update the Status of the given Application and return updated Application
func (appc *ApplicationManager) updateApplicationStatus(application *fornaxv1.Application, newStatus *fornaxv1.ApplicationStatus) (*fornaxv1.Application, error) {
	client := appc.apiServerClient.CoreV1().Applications(application.Namespace)

	klog.Infof(fmt.Sprintf("Updating application status for %s, ", util.Name(application)) +
		fmt.Sprintf("totalInstances %d->%d, ", application.Status.TotalInstances, newStatus.TotalInstances) +
		fmt.Sprintf("desiredInstances %d->%d, ", application.Status.DesiredInstances, newStatus.DesiredInstances) +
		fmt.Sprintf("pendingInstances %d->%d, ", application.Status.PendingInstances, newStatus.PendingInstances) +
		fmt.Sprintf("deletingInstances %d->%d, ", application.Status.DeletingInstances, newStatus.DeletingInstances) +
		fmt.Sprintf("readyInstances %d->%d, ", application.Status.ReadyInstances, newStatus.ReadyInstances) +
		fmt.Sprintf("idleInstances %d->%d, ", application.Status.IdleInstances, newStatus.IdleInstances))

	var getErr, updateErr error
	updatedApplication := application.DeepCopy()
	updatedApplication.Status = *newStatus
	for i := 0; i <= UPDATE_RETRIES; i++ {
		updatedApplication, updateErr = client.UpdateStatus(context.TODO(), updatedApplication, metav1.UpdateOptions{})
		if updateErr == nil {
			break
		}
	}

	if updateErr != nil {
		return nil, updateErr
	}

	// update finalizer
	finalizer := application.Finalizers
	if newStatus.TotalInstances == 0 {
		util.RemoveFinalizer(&updatedApplication.ObjectMeta, fornaxv1.FinalizerApplicationPod)
	} else {
		util.AddFinalizer(&updatedApplication.ObjectMeta, fornaxv1.FinalizerApplicationPod)
	}
	if len(finalizer) != len(application.Finalizers) {
		for i := 0; i <= UPDATE_RETRIES; i++ {
			updatedApplication, updateErr = client.Update(context.TODO(), updatedApplication, metav1.UpdateOptions{})
			if updateErr == nil {
				break
			}
		}
	}

	if application, getErr = client.Get(context.TODO(), application.Name, metav1.GetOptions{}); getErr != nil {
		return application, getErr
	} else {
		return application, getErr
	}
}

func (appc *ApplicationManager) calculateStatus(application *fornaxv1.Application, desiredCount int, action fornaxv1.DeploymentAction, deploymentErr error) *fornaxv1.ApplicationStatus {
	applicationKey, err := cache.MetaNamespaceKeyFunc(application)
	if err != nil {
		return nil
	}

	var poolSummary ApplicationPodSummary
	if pool := appc.getApplicationPool(applicationKey); pool != nil {
		poolSummary = pool.summaryPod(appc.podManager)
	} else {
		return nil
	}

	if application.Status.DesiredInstances == int32(desiredCount) &&
		application.Status.TotalInstances == poolSummary.totalCount &&
		application.Status.IdleInstances == poolSummary.idleCount &&
		application.Status.DeletingInstances == poolSummary.deletingCount &&
		application.Status.PendingInstances == poolSummary.pendingCount &&
		application.Status.ReadyInstances == poolSummary.readyCount {
		return nil
	}

	newStatus := application.Status.DeepCopy()
	newStatus.DesiredInstances = int32(desiredCount)
	newStatus.TotalInstances = poolSummary.totalCount
	newStatus.PendingInstances = poolSummary.pendingCount
	newStatus.DeletingInstances = poolSummary.deletingCount
	newStatus.IdleInstances = poolSummary.idleCount
	newStatus.ReadyInstances = poolSummary.readyCount

	if action == fornaxv1.DeploymentActionCreateInstance || action == fornaxv1.DeploymentActionDeleteInstance {
		if deploymentErr != nil {
			newStatus.DeploymentStatus = fornaxv1.DeploymentStatusFailure
		} else {
			newStatus.DeploymentStatus = fornaxv1.DeploymentStatusSuccess
		}

		message := fmt.Sprintf("deploy application instance, total: %d, desired: %d, pending: %d, deleting: %d, ready: %d, idle: %d",
			newStatus.TotalInstances,
			newStatus.DesiredInstances,
			newStatus.PendingInstances,
			newStatus.DeletingInstances,
			newStatus.ReadyInstances,
			newStatus.IdleInstances)

		if deploymentErr != nil {
			message = fmt.Sprintf("%s, error: %s", message, deploymentErr.Error())
		}

		deploymentHistory := fornaxv1.DeploymentHistory{
			Action: action,
			UpdateTime: metav1.Time{
				Time: time.Now(),
			},
			Reason:  "sync application",
			Message: message,
		}
		newStatus.History = append(newStatus.History, deploymentHistory)
	}

	return newStatus
}
