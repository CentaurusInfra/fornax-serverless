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

package store

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	apistorage "k8s.io/apiserver/pkg/storage"
)

type WatchEventWithOldObj struct {
	Type      watch.EventType
	Object    runtime.Object
	OldObject runtime.Object
}

type WatchWithOldObjInterface interface {
	Stop()
	ResultChanWithPrevobj() <-chan WatchEventWithOldObj
}

type ApiStorageInterface interface {
	apistorage.Interface
	WatchWithOldObj(ctx context.Context, key string, opts storage.ListOptions) (WatchWithOldObjInterface, error)
	EnsureUpdateAndDelete(ctx context.Context, key string, ignoreNotFound bool, preconditions *storage.Preconditions, updatedObj runtime.Object, output runtime.Object) error
}

func IsObjectNotFoundErr(err error) bool {
	if serr, ok := err.(*apistorage.StorageError); ok && serr.Code == apistorage.ErrCodeKeyNotFound {
		return true
	}
	return false
}
