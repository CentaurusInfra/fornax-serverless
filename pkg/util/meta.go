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

package util

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

// NewCurrentMetaTime return a metav1.Time initilized with time.Now
func NewCurrentMetaTime() *metav1.Time {
	return &metav1.Time{
		Time: time.Now(),
	}
}

// NewCurrentMetaTimeNormallized return a metav1.Time initilized with time.Now, truncate to second
func NewCurrentMetaTimeNormallized() *metav1.Time {
	return &metav1.Time{
		Time: time.Now().Truncate(time.Second),
	}
}

// AddFinalizer modify passed object meta and add specified finalizer into its list,
// if there is already specified finalizer, no change
func AddFinalizer(ometa *metav1.ObjectMeta, finalizer string) {
	// add finalizer
	finalizers := []string{}
	for _, v := range ometa.Finalizers {
		if v != finalizer {
			finalizers = append(finalizers, v)
		}
	}
	finalizers = append(finalizers, finalizer)
	ometa.Finalizers = finalizers
}

// RemoveFinalizer modify passed object meta and remove specified finalizer from its list
func RemoveFinalizer(ometa *metav1.ObjectMeta, finalizer string) {
	// remove finalizer
	finalizers := []string{}
	for _, v := range ometa.Finalizers {
		if v != finalizer {
			finalizers = append(finalizers, v)
		}
	}
	ometa.Finalizers = finalizers
}

func Name(obj interface{}) string {
	name, _ := cache.MetaNamespaceKeyFunc(obj)
	return name
}

func MergeObjectMeta(oldMeta *metav1.ObjectMeta, newMeta *metav1.ObjectMeta) {
	oldMeta.ResourceVersion = newMeta.ResourceVersion

	labels := map[string]string{}
	for k, v := range oldMeta.GetLabels() {
		labels[k] = v
	}
	for k, v := range newMeta.GetLabels() {
		labels[k] = v
	}
	oldMeta.Labels = labels

	annotations := map[string]string{}
	for k, v := range oldMeta.GetAnnotations() {
		annotations[k] = v
	}
	for k, v := range newMeta.GetAnnotations() {
		annotations[k] = v
	}
	oldMeta.Annotations = annotations

	if newMeta.DeletionTimestamp != nil && oldMeta.DeletionTimestamp == nil {
		oldMeta.DeletionTimestamp = newMeta.DeletionTimestamp
	}

	if newMeta.DeletionGracePeriodSeconds != nil && oldMeta.DeletionGracePeriodSeconds == nil {
		oldMeta.DeletionGracePeriodSeconds = newMeta.DeletionGracePeriodSeconds
	}
}
