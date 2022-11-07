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

func MergeObjectMeta(fromMeta, toMeta *metav1.ObjectMeta) {
	toMeta.ResourceVersion = fromMeta.ResourceVersion

	toMeta.Labels = fromMeta.GetLabels()

	toMeta.Annotations = fromMeta.GetAnnotations()

	if fromMeta.DeletionTimestamp != nil && toMeta.DeletionTimestamp == nil {
		toMeta.DeletionTimestamp = fromMeta.DeletionTimestamp
	}

	if fromMeta.DeletionGracePeriodSeconds != nil && toMeta.DeletionGracePeriodSeconds == nil {
		toMeta.DeletionGracePeriodSeconds = fromMeta.DeletionGracePeriodSeconds
	}
}
