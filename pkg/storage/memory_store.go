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
	"path"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/klog/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/storage"
	apistorage "k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/etcd3"
	"k8s.io/apiserver/pkg/storage/etcd3/metrics"
)

type objState struct {
	obj   runtime.Object
	meta  *storage.ResponseMeta
	rev   int64
	stale bool
}

type Key struct {
	key string
	rev int64
}

type Keys []*Key

func (keys Keys) Len() int {
	return len(keys)
}

//so, sort latency from smaller to lager value
func (keys Keys) Less(i, j int) bool {
	return keys[i].rev < keys[i].rev
}

func (keys Keys) Swap(i, j int) {
	keys[i], keys[j] = keys[j], keys[i]
}

type memoryStore struct {
	// codec               runtime.Codec
	versioner storage.Versioner
	// transformer         value.Transformer
	mu                  sync.RWMutex
	kvs                 map[string]runtime.Object
	rev                 int64
	klist               []*Key
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

	recursive := opts.Recursive
	resourceVersion := opts.ResourceVersion
	match := opts.ResourceVersionMatch
	pred := opts.Predicate

	klog.InfoS("GWJ get list of obj", "key", key, "opts", opts)
	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return err
	}
	v, err := conversion.EnforcePtr(listPtr)
	if err != nil || v.Kind() != reflect.Slice {
		return fmt.Errorf("need ptr to slice: %v", err)
	}
	key = path.Join(ms.pathPrefix, key)

	// For recursive lists, we need to make sure the key ended with "/" so that we only
	// get children "directories". e.g. if we have key "/a", "/a/b", "/ab", getting keys
	// with prefix "/a" will return all three, while with prefix "/a/" will return only
	// "/a/b" which is the correct answer.
	if recursive && !strings.HasSuffix(key, "/") {
		key += "/"
	}
	keyPrefix := key

	// set the appropriate clientv3 options to filter the returned data set
	var limit int64 = pred.Limit
	if ms.pagingEnabled && pred.Limit > 0 {
	}

	newItemFunc := getNewItemFunc(listObj, v)

	var fromRV *uint64
	if len(resourceVersion) > 0 {
		parsedRV, err := ms.versioner.ParseResourceVersion(resourceVersion)
		if err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("invalid resource version: %v", err))
		}
		fromRV = &parsedRV
	}

	var returnedRV, continueRV, withRev int64
	var continueKey string
	switch {
	case recursive && ms.pagingEnabled && len(pred.Continue) > 0:
		continueKey, continueRV, err = decodeContinue(pred.Continue, keyPrefix)
		if err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("invalid continue token: %v", err))
		}

		if len(resourceVersion) > 0 && resourceVersion != "0" {
			return apierrors.NewBadRequest("specifying resource version is not allowed when using continue")
		}

		key = continueKey

		// If continueRV > 0, the LIST request needs a specific resource version.
		// continueRV==0 is invalid.
		// If continueRV < 0, the request is for the latest resource version.
		if continueRV > 0 {
			withRev = continueRV
			returnedRV = continueRV
		}
	case recursive && ms.pagingEnabled && pred.Limit > 0:
		if fromRV != nil {
			switch match {
			case metav1.ResourceVersionMatchNotOlderThan:
				// The not older than constraint is checked after we get a response from etcd,
				// and returnedRV is then set to the revision we get from the etcd response.
			case metav1.ResourceVersionMatchExact:
				returnedRV = int64(*fromRV)
				withRev = returnedRV
			case "": // legacy case
				if *fromRV > 0 {
					returnedRV = int64(*fromRV)
					withRev = returnedRV
				}
			default:
				return fmt.Errorf("unknown ResourceVersionMatch value: %v", match)
			}
		}

	default:
		if fromRV != nil {
			switch match {
			case metav1.ResourceVersionMatchNotOlderThan:
				// The not older than constraint is checked after we get a response from etcd,
				// and returnedRV is then set to the revision we get from the etcd response.
			case metav1.ResourceVersionMatchExact:
				returnedRV = int64(*fromRV)
				withRev = returnedRV
			case "": // legacy case
			default:
				return fmt.Errorf("unknown ResourceVersionMatch value: %v", match)
			}
		}

		if recursive {
		}
	}

	// loop until we have filled the requested limit from etcd or there are no more results
	var lastKey []byte
	var hasMore bool
	var getResp *clientv3.GetResponse
	var numFetched int
	var numEvald int
	// Because these metrics are for understanding the costs of handling LIST requests,
	// get them recorded even in error cases.
	defer func() {
		numReturn := v.Len()
		metrics.RecordStorageListMetrics(s.groupResourceString, numFetched, numEvald, numReturn)
	}()
	for {
	}

	// instruct the client to begin querying from immediately after the last key we returned
	// we never return a key that the client wouldn't be allowed to see
	if hasMore {
		// we want to start immediately after the last key
		next, err := encodeContinue(string(lastKey)+"\x00", keyPrefix, returnedRV)
		if err != nil {
			return err
		}
		var remainingItemCount *int64
		// getResp.Count counts in objects that do not match the pred.
		// Instead of returning inaccurate count for non-empty selectors, we return nil.
		// Only set remainingItemCount if the predicate is empty.
		if utilfeature.DefaultFeatureGate.Enabled(features.RemainingItemCount) {
			if pred.Empty() {
				c := int64(getResp.Count - pred.Limit)
				remainingItemCount = &c
			}
		}
		return ms.versioner.UpdateList(listObj, uint64(returnedRV), next, remainingItemCount)
	}

	// no continuation
	return s.versioner.UpdateList(listObj, uint64(returnedRV), "", nil)

	return nil
}

// GuaranteedUpdate implements storage.Interface
func (ms *memoryStore) GuaranteedUpdate(ctx context.Context, key string, out runtime.Object, ignoreNotFound bool, preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc, cachedExistingObject runtime.Object) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	klog.InfoS("GWJ update obj", "key", key, "obj", cachedExistingObject)
	_, err := conversion.EnforcePtr(out)
	if err != nil {
		return fmt.Errorf("unable to convert output object to pointer: %v", err)
	}
	key = path.Join(ms.pathPrefix, key)

	if existingObj, found := ms.kvs[key]; !found {
		if ignoreNotFound {
			return runtime.SetZeroValue(out)
		}
		return storage.NewKeyNotFoundError(key, 0)
	} else {
		currState, err := ms.getStateFromObject(existingObj)
		if err != nil {
			return err
		}

		if cachedExistingObject != nil {
			s, err := ms.getStateFromObject(cachedExistingObject)
			if err != nil {
				return err
			}
			if s.rev != currState.rev {
				return apierrors.NewBadRequest(fmt.Sprintf("resource version is staled: %d", s.rev))
			}
		}

		if preconditions != nil {
			if err := preconditions.Check(key, currState.obj); err != nil {
				return err
			}

			ret, _, err := ms.updateState(currState, tryUpdate)
			if err != nil {
				return err
			}
			ms.kvs[key] = ret
			out = out.DeepCopyObject()
		}
	}

	return nil
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

// appendListItem decodes and appends the object (if it passes filter) to v, which must be a slice.
func appendListItem(v reflect.Value, data []byte, rev uint64, pred storage.SelectionPredicate, codec runtime.Codec, versioner storage.Versioner, newItemFunc func() runtime.Object) error {
	obj, _, err := codec.Decode(data, nil, newItemFunc())
	if err != nil {
		return err
	}
	// being unable to set the version does not prevent the object from being extracted
	if err := versioner.UpdateObject(obj, rev); err != nil {
		klog.Errorf("failed to update object version: %v", err)
	}
	if matched, err := pred.Matches(obj); err == nil && matched {
		v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
	}
	return nil
}

func (ms *memoryStore) updateState(st *objState, userUpdate storage.UpdateFunc) (runtime.Object, uint64, error) {
	ret, ttlPtr, err := userUpdate(st.obj, *st.meta)
	if err != nil {
		return nil, 0, err
	}

	if err := ms.versioner.PrepareObjectForStorage(ret); err != nil {
		return nil, 0, fmt.Errorf("PrepareObjectForStorage failed: %v", err)
	}
	var ttl uint64
	if ttlPtr != nil {
		ttl = *ttlPtr
	}
	return ret, ttl, nil
}

var _ apistorage.Interface = &memoryStore{}
