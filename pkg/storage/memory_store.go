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

package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/storage"
	apistorage "k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/etcd3"
)

type objState struct {
	obj   runtime.Object
	meta  *storage.ResponseMeta
	rev   int64
	stale bool
}

type memoryStore struct {
	// codec               runtime.Codec
	versioner storage.Versioner
	// transformer         value.Transformer
	mu                  sync.RWMutex
	kvs                 map[string]runtime.Object
	pathPrefix          string
	groupResource       schema.GroupResource
	groupResourceString string
	pagingEnabled       bool
	watcher             *memoryStoreWatcher
}

func NewMemoryStore(newFunc func() runtime.Object, prefix string, groupResource schema.GroupResource, pagingEnabled bool) storage.Interface {
	return &memoryStore{
		versioner:           etcd3.APIObjectVersioner{},
		mu:                  sync.RWMutex{},
		kvs:                 map[string]runtime.Object{},
		pathPrefix:          prefix,
		groupResource:       groupResource,
		groupResourceString: groupResource.String(),
		pagingEnabled:       pagingEnabled,
		watcher:             &memoryStoreWatcher{},
	}
}

// Count implements storage.Interface
func (ms *memoryStore) Count(key string) (int64, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return int64(len(ms.kvs)), nil
}

// Create implements storage.Interface
func (ms *memoryStore) Create(ctx context.Context, key string, obj runtime.Object, out runtime.Object, ttl uint64) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if _, found := ms.kvs[key]; found {
		return storage.NewKeyExistsError(key, 0)
	} else {
		if version, err := ms.versioner.ObjectResourceVersion(obj); err == nil && version != 0 {
			return errors.New("resourceVersion should not be set on objects to be created")
		}
		if err := ms.versioner.PrepareObjectForStorage(obj); err != nil {
			return fmt.Errorf("PrepareObjectForStorage failed: %v", err)
		}
		ms.kvs[key] = obj
		out = obj.DeepCopyObject()
	}
	return nil
}

// Delete implements storage.Interface
func (ms *memoryStore) Delete(ctx context.Context, key string, out runtime.Object, preconditions *storage.Preconditions, validateDeletion storage.ValidateObjectFunc, cachedExistingObject runtime.Object) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if existingObj, found := ms.kvs[key]; !found {
		return storage.NewKeyNotFoundError(key, 0)
	} else {
		currState, err := ms.getStateFromObject(existingObj)
		if err != nil {
			return err
		}

		if cachedExistingObject != nil {
			_, err := ms.getStateFromObject(cachedExistingObject)
			if err != nil {
				return err
			}
		} else {
			_, err := ms.getStateFromObject(existingObj)
			if err != nil {
				return err
			}
		}

		if preconditions != nil {
			if err := preconditions.Check(key, currState.obj); err != nil {
				return err
			}

			if err := validateDeletion(ctx, currState.obj); err != nil {
				return err
			}

			delete(ms.kvs, key)
			out = existingObj.DeepCopyObject()
		}
	}
	return nil
}

// Get implements storage.Interface
func (ms *memoryStore) Get(ctx context.Context, key string, opts storage.GetOptions, out runtime.Object) error {
	ms.mu.RLock()
	if existingObj, found := ms.kvs[key]; !found {
		if opts.IgnoreNotFound {
			return runtime.SetZeroValue(out)
		}
		return storage.NewKeyNotFoundError(key, 0)
	} else {
		existingObjState, err := ms.getStateFromObject(existingObj)
		if err != nil {
			return storage.NewInternalError(err.Error())
		}

		if err := ms.validateMinimumResourceVersion(opts.ResourceVersion, existingObjState.meta.ResourceVersion); err != nil {
			return err
		}

		out = existingObj.DeepCopyObject()
	}
	return nil
}

// GetList implements storage.Interface
func (ms *memoryStore) GetList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	panic("unimplemented")
}

// GuaranteedUpdate implements storage.Interface
func (ms *memoryStore) GuaranteedUpdate(ctx context.Context, key string, ptrToType runtime.Object, ignoreNotFound bool, preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc, cachedExistingObject runtime.Object) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	panic("unimplemented")
}

// Versioner implements storage.Interface
func (ms *memoryStore) Versioner() storage.Versioner {
	return ms.versioner
}

// Watch implements storage.Interface
func (ms *memoryStore) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	return &memoryStoreWatcher{}, nil
}

func (ms *memoryStore) getStateFromObject(obj runtime.Object) (*objState, error) {
	state := &objState{
		obj:  obj,
		meta: &storage.ResponseMeta{},
	}

	rv, err := ms.versioner.ObjectResourceVersion(obj)
	if err != nil {
		return nil, fmt.Errorf("couldn't get resource version: %v", err)
	}
	state.rev = int64(rv)
	state.meta.ResourceVersion = uint64(state.rev)

	// Compute the serialized form - for that we need to temporarily clean
	// its resource version field (those are not stored in etcd).
	if err := ms.versioner.PrepareObjectForStorage(obj); err != nil {
		return nil, fmt.Errorf("PrepareObjectForStorage failed: %v", err)
	}
	if err := ms.versioner.UpdateObject(state.obj, uint64(rv)); err != nil {
		klog.Errorf("failed to update object version: %v", err)
	}
	return state, nil
}

// validateMinimumResourceVersion returns a 'too large resource' version error when the provided minimumResourceVersion is
// greater than the most recent actualRevision available from storage.
func (ms *memoryStore) validateMinimumResourceVersion(minimumResourceVersion string, actualRevision uint64) error {
	if minimumResourceVersion == "" {
		return nil
	}
	minimumRV, err := ms.versioner.ParseResourceVersion(minimumResourceVersion)
	if err != nil {
		return apierrors.NewBadRequest(fmt.Sprintf("invalid resource version: %v", err))
	}
	// Enforce the storage.Interface guarantee that the resource version of the returned data
	// "will be at least 'resourceVersion'".
	if minimumRV > actualRevision {
		return storage.NewTooLargeResourceVersionError(minimumRV, actualRevision, 0)
	}
	return nil
}

var _ apistorage.Interface = &memoryStore{}
