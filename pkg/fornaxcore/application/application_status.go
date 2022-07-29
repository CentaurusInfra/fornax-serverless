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
	fornaxclient "centaurusinfra.io/fornax-serverless/pkg/client/clientset/versioned/typed/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	k8spodutil "k8s.io/kubernetes/pkg/api/v1/pod"
)

const (
	// The number of times we retry updating a Application's status.
	StatusUpdateRetries = 1
)

// updateApplicationStatus attempts to update the Status.Replicas of the given Application, with a single GET/PUT retry.
func (appc *ApplicationManager) updateApplicationStatus(client fornaxclient.ApplicationInterface, application *fornaxv1.Application, newStatus *fornaxv1.ApplicationStatus) (*fornaxv1.Application, error) {
	if application.Status.DesiredInstances == newStatus.DesiredInstances &&
		application.Status.AvailableInstances == newStatus.AvailableInstances &&
		application.Status.IdleInstances == newStatus.IdleInstances &&
		application.Status.ReadyInstances == newStatus.ReadyInstances {
		return application, nil
	}

	applicationKey, _ := cache.MetaNamespaceKeyFunc(application)
	var getErr, updateErr error
	var updatedApplication *fornaxv1.Application
	klog.Infof(fmt.Sprintf("Updating application status for %s, ", applicationKey) +
		fmt.Sprintf("desiredInstances %d->%d, ", application.Status.DesiredInstances, newStatus.DesiredInstances) +
		fmt.Sprintf("availableInstances %d->%d, ", application.Status.AvailableInstances, newStatus.AvailableInstances) +
		fmt.Sprintf("readyInstances %d->%d, ", application.Status.ReadyInstances, newStatus.ReadyInstances) +
		fmt.Sprintf("idleInstances %d->%d, ", application.Status.IdleInstances, newStatus.IdleInstances))

	for i := 0; i <= StatusUpdateRetries; i++ {
		application.Status = *newStatus
		updatedApplication, updateErr = client.UpdateStatus(context.TODO(), application, metav1.UpdateOptions{})
		if updateErr == nil {
			return updatedApplication, nil
		}
	}

	// if we get here, it means update failed, but get application in case update actually succeeded
	if application, getErr = client.Get(context.TODO(), application.Name, metav1.GetOptions{}); getErr != nil {
		return application, getErr
	} else {
		return application, getErr
	}
}

func (appc *ApplicationManager) calculateStatus(application *fornaxv1.Application, desiredNumber int, deploymentErr error) *fornaxv1.ApplicationStatus {
	newStatus := application.Status.DeepCopy()
	newStatus.DesiredInstances = int32(desiredNumber)

	availableCount := 0
	idleCount := 0
	readyCount := 0
	applicationKey, err := cache.MetaNamespaceKeyFunc(application)
	if err != nil {
		return nil
	}

	if pool, found := appc.applicationPods[applicationKey]; found {
		for k := range pool.pods {
			pod := appc.podManager.FindPod(k)
			if pod != nil && pod.DeletionTimestamp == nil {
				availableCount += 1
				if k8spodutil.IsPodReady(pod) {
					readyCount += 1
				}
				// TODO, change this logic after session management is added
				idleCount += 1
			}
		}
	}

	newStatus.AvailableInstances = int32(availableCount)
	newStatus.IdleInstances = int32(idleCount)
	newStatus.ReadyInstances = int32(readyCount)
	if deploymentErr != nil {
		newStatus.DeploymentStatus = fornaxv1.DeploymentStatusPartialSuccess
	} else {
		newStatus.DeploymentStatus = fornaxv1.DeploymentStatusSuccess
	}

	if desiredNumber > 0 && availableCount == 0 {
		newStatus.DeploymentStatus = fornaxv1.DeploymentStatusFailure
	}

	var action fornaxv1.DeploymentAction
	if desiredNumber >= availableCount {
		action = fornaxv1.DeploymentActionCreateInstance
	} else {
		action = fornaxv1.DeploymentActionDeleteInstance
	}

	message := fmt.Sprintf("sync application, desired: %d, available: %d, ready: %d, idle: %d",
		newStatus.DesiredInstances,
		newStatus.AvailableInstances,
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

	return newStatus
}
