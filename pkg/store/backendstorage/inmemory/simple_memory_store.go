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

package inmemory

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"centaurusinfra.io/fornax-serverless/pkg/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/klog/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/storage"
	apistorage "k8s.io/apiserver/pkg/storage"
)

type MemoryStore struct {
	watchEventCache *watchCache
	versioner       storage.Versioner
	revmu           sync.RWMutex
	freezeWorld     sync.RWMutex
	kvs             *memoryStoreMap
	kvList          objList
	groupResource   schema.GroupResource
	pagingEnabled   bool
	watchers        []*memoryStoreWatcher
}

var (
	_MemoryRev           = uint64(1)
	_InMemoryStoresMutex = &sync.RWMutex{}
	_InMemoryStores      = map[string]*MemoryStore{}
)

// NewMemoryStore return a singleton storage.Interface for a groupResource
func NewMemoryStore(groupResource schema.GroupResource, pagingEnabled bool) *MemoryStore {
	_InMemoryStoresMutex.Lock()
	defer _InMemoryStoresMutex.Unlock()
	key := groupResource.String()
	klog.InfoS("New or Get a in memory store for", "resource", key)
	if si, found := _InMemoryStores[key]; found {
		return si
	} else {
		si = &MemoryStore{
			watchEventCache: &watchCache{
				size:    10000,
				events:  []*objEvent{},
				cacheMu: sync.RWMutex{},
			},
			versioner:     store.APIObjectVersioner{},
			revmu:         sync.RWMutex{},
			freezeWorld:   sync.RWMutex{},
			kvs:           &memoryStoreMap{mu: sync.RWMutex{}, kvs: map[string]objMapOrObj{}},
			kvList:        []*objWithIndex{},
			groupResource: groupResource,
			pagingEnabled: pagingEnabled,
			watchers:      []*memoryStoreWatcher{},
		}
		_InMemoryStores[key] = si
		return si
	}
}

// Load initialize memory and load persisted data
func (ms *MemoryStore) Load() error {
	ms.freezeWorld.Lock()
	defer ms.freezeWorld.Unlock()
	// load data from backend storage for initialized state
	return nil
}

// Stop cleanup memory
func (ms *MemoryStore) Stop() error {
	ms.freezeWorld.Lock()
	defer ms.freezeWorld.Unlock()
	// TODO unload data into backend storage
	return nil
}

// Count implements storage.Interface
func (ms *MemoryStore) Count(key string) (int64, error) {
	count, err := ms.kvs.count(strings.Split(key, "/"))
	klog.InfoS("GWJ count object", "key", key, "count", count)
	return count, err
}

// Create implements storage.Interface
func (ms *MemoryStore) Create(ctx context.Context, key string, obj runtime.Object, out runtime.Object, ttl uint64) error {
	klog.InfoS("GWJ create object", "key", key)
	ms.freezeWorld.RLock()
	defer ms.freezeWorld.RUnlock()
	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return err
	}

	if err := ms.versioner.PrepareObjectForStorage(obj); err != nil {
		return fmt.Errorf("PrepareObjectForStorage failed: %v", err)
	}

	keys := strings.Split(key, "/")
	if o := ms.kvs.get(keys); o != nil {
		return storage.NewKeyExistsError(key, 0)
	} else {
		rev, index, err := ms.reserveRevAndSlot()
		if err != nil {
			return err
		}

		ms.versioner.UpdateObject(obj, rev)
		objWi := &objWithIndex{
			key:   key,
			obj:   obj.DeepCopyObject(),
			index: index,
		}
		err = ms.kvs.put(keys, objWi, 0)
		if err != nil {
			return err
		}
		ms.kvList[index] = objWi
		outVal.Set(reflect.ValueOf(obj).Elem())

		event := &objEvent{
			key:       key,
			obj:       out.DeepCopyObject(),
			oldObj:    nil,
			rev:       rev,
			isDeleted: false,
			isCreated: true,
		}
		ms.sendEvent(event)
	}
	return nil
}

// Delete implements storage.Interface
func (ms *MemoryStore) Delete(ctx context.Context, key string, out runtime.Object, preconditions *storage.Preconditions, validateDeletion storage.ValidateObjectFunc, cachedExistingObject runtime.Object) error {
	klog.InfoS("GWJ delete object", "key", key)
	ms.freezeWorld.RLock()
	defer ms.freezeWorld.RUnlock()

	keys := strings.Split(key, "/")
	if existingObj := ms.kvs.get(keys); existingObj == nil {
		return storage.NewKeyNotFoundError(key, 0)
	} else {
		rev, err := func() (uint64, error) {
			ms.revmu.Lock()
			defer ms.revmu.Unlock()
			currState, err := getStateFromObject(ms.versioner, existingObj.obj)
			if err != nil {
				return 0, err
			}

			if cachedExistingObject != nil {
				_, err := getStateFromObject(ms.versioner, cachedExistingObject)
				if err != nil {
					return 0, err
				}
			} else {
				_, err := getStateFromObject(ms.versioner, existingObj.obj)
				if err != nil {
					return 0, err
				}
			}

			if preconditions != nil {
				if err := preconditions.Check(key, currState.obj); err != nil {
					return 0, err
				}
			}

			if err := validateDeletion(ctx, currState.obj); err != nil {
				return 0, err
			}

			rev := atomic.AddUint64(&_MemoryRev, 1)
			return rev, nil
		}()
		if err != nil {
			return err
		}

		err = ms.kvs.del(keys)
		if err != nil {
			return err
		}
		ms.kvList[existingObj.index] = nil
		out = existingObj.obj.DeepCopyObject()

		event := &objEvent{
			key:       key,
			obj:       nil,
			oldObj:    out.DeepCopyObject(),
			rev:       rev,
			isDeleted: true,
			isCreated: false,
		}
		ms.sendEvent(event)
	}

	return nil
}

// Get implements storage.Interface
func (ms *MemoryStore) Get(ctx context.Context, key string, opts storage.GetOptions, out runtime.Object) error {
	// klog.InfoS("GWJ get object", "key", key, "opts", opts)
	ms.freezeWorld.RLock()
	defer ms.freezeWorld.RUnlock()
	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return fmt.Errorf("unable to convert output object to pointer: %v", err)
	}

	keys := strings.Split(key, "/")
	if existingObj := ms.kvs.get(keys); existingObj == nil {
		if opts.IgnoreNotFound {
			return runtime.SetZeroValue(out)
		}
		return storage.NewKeyNotFoundError(key, 0)
	} else {
		existingObjState, err := getStateFromObject(ms.versioner, existingObj.obj)
		if err != nil {
			return storage.NewInternalError(err.Error())
		}

		if err := ms.validateMinimumResourceVersion(ms.versioner, opts.ResourceVersion, existingObjState.rev); err != nil {
			return err
		}

		outVal.Set(reflect.ValueOf(existingObj.obj).Elem())
	}
	return nil
}

// GetList implements k8s storage.Interface
func (ms *MemoryStore) GetList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	// klog.InfoS("GWJ get list of object", "key", key, "opts", opts)
	ms.freezeWorld.RLock()
	defer ms.freezeWorld.RUnlock()

	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return err
	}
	listRetVal, err := conversion.EnforcePtr(listPtr)
	if err != nil || listRetVal.Kind() != reflect.Slice {
		return fmt.Errorf("need ptr to slice: %v", err)
	}

	recursive := opts.Recursive
	if !recursive {
		return ms.getSingleObjectAsList(ctx, key, opts, listObj)
	}
	if recursive && !strings.HasSuffix(key, "/") {
		key += "/"
	}
	keyPrefix := key

	resourceVersion := opts.ResourceVersion
	match := opts.ResourceVersionMatch
	pred := opts.Predicate
	var fromRV *uint64
	if len(resourceVersion) > 0 {
		parsedRV, err := ms.versioner.ParseResourceVersion(resourceVersion)
		if err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("invalid resource version: %v", err))
		}
		fromRV = &parsedRV
	}

	returnedRV := uint64(1)
	withRV := uint64(0)
	searchingIndex := uint64(0)
	var continueKey string
	switch {
	case len(pred.Continue) > 0:
		if len(resourceVersion) > 0 && resourceVersion != "0" {
			return apierrors.NewBadRequest("specifying resource version is not allowed when using continue")
		}

		var continueRV int64
		continueKey, continueRV, err = decodeContinue(pred.Continue, keyPrefix)
		if err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("invalid continue token: %v", err))
		}
		// klog.InfoS("GWJ get list with", "continueRV", continueRV, "continueKey", continueKey)
		if continueRV > 0 {
			withRV = uint64(continueRV)
			returnedRV = withRV
		} else if continueRV == 0 {
			return apierrors.NewBadRequest("0 continue resource version is not allowed when using continue")
		}
		// use continueKey to find object and check if its rv is same as continue rv, then use this object index to search sorted object list
		// if object with continueKey is updated since last return, use continueRV do a binary search to get a index
		continueKeys := strings.Split(continueKey, "/")
		if obj := ms.kvs.get(continueKeys); obj != nil {
			objRV, _ := ms.versioner.ObjectResourceVersion(obj.obj)
			if objRV == withRV {
				searchingIndex = obj.index
			} else {
				searchingIndex = ms.binarySearchInObjList(withRV)
			}
		}
	default:
		// no continue key, but with resource version, start to search from provided resource version
		if fromRV != nil && *fromRV > 0 {
			withRV = *fromRV
			returnedRV = *fromRV
			searchingIndex = ms.binarySearchInObjList(*fromRV)
		}
	}

	// can not find valid searching index from list, return empty list
	if searchingIndex >= uint64(len(ms.kvList)) {
		return ms.versioner.UpdateList(listObj, uint64(returnedRV), "", nil)
	}

	// range sorted revision list from searching index until meet requested limit or there are no more items
	limit := int64(math.MaxInt64)
	if ms.pagingEnabled && pred.Limit > 0 {
		limit = pred.Limit
	}
	remainingItemCount := int64(0)
	hasMore := false
	lastKey := ""
	lastRev := withRV
	listBufferLen := uint64(len(ms.kvList))
	// klog.InfoS("GWJ get list from", "withRV", withRV, "index", searchingIndex, "list size", listBufferLen)
	for i := searchingIndex; i < listBufferLen; i++ {
		v := ms.kvList[i]
		if v != nil {
			rv, _ := ms.versioner.ObjectResourceVersion(v.obj)
			lastKey = v.key
			lastRev = rv
			if !strings.HasPrefix(lastKey, keyPrefix) {
				continue
			}
			switch match {
			case metav1.ResourceVersionMatchNotOlderThan:
				if rv >= withRV {
					appendListItem(listRetVal, v.obj, rv, pred, ms.versioner)
				}
			case metav1.ResourceVersionMatchExact:
				if rv > withRV {
					appendListItem(listRetVal, v.obj, rv, pred, ms.versioner)
				}
			case "":
				if rv > withRV {
					// append
					appendListItem(listRetVal, v.obj, rv, pred, ms.versioner)
				}
			default:
				return fmt.Errorf("unknown ResourceVersionMatch value: %v", match)
			}
		}
		// have got enough items, check if there are still items in list
		if int64(listRetVal.Len()) == limit {
			if i < listBufferLen-1 {
				remainingItemCount = int64(listBufferLen - 1 - i)
				hasMore = true
			}
			break
		}
	}

	// klog.InfoS("GWJ get list return a list", "len", listRetVal.Len(), "lastkey", lastKey, "last rev", lastRev, "hasMore", hasMore)
	if hasMore {
		continueToken, err := encodeContinue(lastKey, keyPrefix, lastRev)
		if err != nil {
			return err
		}
		if pred.Empty() {
			return ms.versioner.UpdateList(listObj, returnedRV, continueToken, &remainingItemCount)
		} else {
			return ms.versioner.UpdateList(listObj, returnedRV, continueToken, nil)
		}
	}

	// no more items, use empty string as continue token
	return ms.versioner.UpdateList(listObj, returnedRV, "", nil)
}

// GuaranteedUpdate implements k8s storage.Interface
func (ms *MemoryStore) GuaranteedUpdate(ctx context.Context, key string, out runtime.Object, ignoreNotFound bool, preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc, cachedExistingObject runtime.Object) error {
	klog.InfoS("GWJ update object", "key", key)
	ms.freezeWorld.RLock()
	defer ms.freezeWorld.RUnlock()
	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return fmt.Errorf("unable to convert output object to pointer: %v", err)
	}

	keys := strings.Split(key, "/")
	if curObjWi := ms.kvs.get(keys); curObjWi == nil {
		if ignoreNotFound {
			return runtime.SetZeroValue(out)
		}
		return storage.NewKeyNotFoundError(key, 0)
	} else {
		curObj := curObjWi.obj.DeepCopyObject()
		currState, err := getStateFromObject(ms.versioner, curObjWi.obj)
		if err != nil {
			return err
		}

		if cachedExistingObject != nil {
			s, err := getStateFromObject(ms.versioner, cachedExistingObject)
			if err != nil {
				return err
			}
			if s.rev != currState.rev {
				klog.Warningf("provided cached existing object resource version is staled: cached RV %d, current RV %d", s.rev, currState.rev)
			}
		}

		if preconditions != nil {
			if err := preconditions.Check(key, currState.obj); err != nil {
				return err
			}
		}

		ret, _, err := updateState(ms.versioner, currState, tryUpdate)
		if err != nil {
			return err
		}

		rev, index, err := ms.reserveRevAndSlot()
		if err != nil {
			return err
		}

		if err := ms.versioner.UpdateObject(ret, rev); err != nil {
			return err
		}
		newObjWi := &objWithIndex{
			key:   key,
			obj:   ret.DeepCopyObject(),
			index: index,
		}
		err = ms.kvs.put(keys, newObjWi, currState.rev)
		if err != nil {
			return err
		}
		ms.kvList[curObjWi.index] = nil
		ms.kvList[newObjWi.index] = newObjWi
		outVal.Set(reflect.ValueOf(ret).Elem())

		event := &objEvent{
			key:       key,
			obj:       out.DeepCopyObject(),
			oldObj:    curObj,
			rev:       rev,
			isDeleted: false,
			isCreated: false,
		}
		ms.sendEvent(event)
	}

	return nil
}

// Versioner implements k8s storage.Interface
func (ms *MemoryStore) Versioner() storage.Versioner {
	return ms.versioner
}

// Watch implements k8s storage.Interface
func (ms *MemoryStore) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	return ms.watch(ctx, key, opts, false)
}

// WatchWithOldObj implements FornaxStorage
func (ms *MemoryStore) WatchWithOldObj(ctx context.Context, key string, opts storage.ListOptions) (store.WatchWithOldObjInterface, error) {
	return ms.watch(ctx, key, opts, true)
}

// EnsureUpdateAndDelete implements FornaxStorage, it update object and delete it if object has delete timestamp and empty finalizer, delete it
func (ms *MemoryStore) EnsureUpdateAndDelete(ctx context.Context, key string, ignoreNotFound bool, preconditions *storage.Preconditions, updatedObj runtime.Object, output runtime.Object) error {
	err := ms.GuaranteedUpdate(ctx, key, output, ignoreNotFound, preconditions, ms.getTryUpdateFunc(updatedObj), nil)
	if err != nil {
		return err
	}

	if shouldDeleteDuringUpdate(output) {
		err = ms.Delete(ctx, key, output, preconditions, func(ctx context.Context, obj runtime.Object) error { return nil }, output)
	}

	return nil
}

func (ms *MemoryStore) getTryUpdateFunc(updating runtime.Object) apistorage.UpdateFunc {
	return func(existing runtime.Object, res storage.ResponseMeta) (runtime.Object, *uint64, error) {
		existingVersion, err := ms.versioner.ObjectResourceVersion(existing)
		if err != nil {
			return nil, nil, err
		}
		updatingVersion, err := ms.versioner.ObjectResourceVersion(updating)
		if existingVersion != updatingVersion {
			return nil, nil, fmt.Errorf("object is already updated to a newer version, get it and update again")
		}
		return updating.DeepCopyObject(), nil, nil
	}
}

func (ms *MemoryStore) watch(ctx context.Context, key string, opts storage.ListOptions, withOldObj bool) (*memoryStoreWatcher, error) {
	klog.InfoS("GWJ watch object", "key", key, "ops", opts, "resource", ms.groupResource.String())
	rev, err := ms.versioner.ParseResourceVersion(opts.ResourceVersion)
	if err != nil {
		return nil, err
	}

	// start to watch new events
	watcher := NewMemoryStoreWatcher(ctx, key, opts)
	ms.watchers = append(ms.watchers, watcher)

	objEvents := []*objEvent{}
	if rev > 1 {
		objEvents, err = ms.getObjEventsAfterRev(key, rev, opts)
		// find all obj event which are greater than passed rev and call watcher to run with these existing events
		if err != nil {
			return nil, err
		}
	} else {
		rev = atomic.LoadUint64(&_MemoryRev)
	}
	go watcher.run(rev, objEvents, withOldObj)
	return watcher, nil
}

func (ms *MemoryStore) getObjEventsAfterRev(key string, rev uint64, opts storage.ListOptions) ([]*objEvent, error) {
	ms.freezeWorld.RLock()
	defer ms.freezeWorld.RUnlock()

	prefix := key
	if opts.Recursive && !strings.HasSuffix(key, "/") {
		prefix += "/"
	}

	rev, err := ms.versioner.ParseResourceVersion(opts.ResourceVersion)
	if err != nil {
		return nil, err
	}

	objEvents := []*objEvent{}
	uindex := ms.binarySearchInObjList(rev)
	for i := uindex; i < uint64(len(ms.kvList)); i++ {
		v := ms.kvList[i]
		if v == nil || !strings.HasPrefix(v.key, prefix) {
			continue
		}
		oState, _ := getStateFromObject(ms.versioner, v.obj)
		e := &objEvent{
			key:       v.key,
			obj:       v.obj,
			oldObj:    nil,
			rev:       oState.rev,
			isDeleted: false,
			isCreated: true,
		}
		switch opts.ResourceVersionMatch {
		case metav1.ResourceVersionMatchNotOlderThan:
			if oState.rev >= uint64(rev) {
				objEvents = append(objEvents, e)
			}
		case metav1.ResourceVersionMatchExact:
			if oState.rev > uint64(rev) {
				objEvents = append(objEvents, e)
			}
		case "":
			// append
			if oState.rev > uint64(rev) {
				objEvents = append(objEvents, e)
			}
		default:
			return nil, fmt.Errorf("Unknown ResourceVersionMatch value: %s", opts.ResourceVersionMatch)
		}
	}

	return objEvents, nil
}

func (ms *MemoryStore) binarySearchInObjList(rv uint64) uint64 {
	ms.freezeWorld.RLock()
	defer ms.freezeWorld.RUnlock()
	f := func(i int) bool {
		obj := ms.kvList[(i)%len(ms.kvList)]
		if obj == nil {
			return false
		}
		objRV, _ := getStateFromObject(ms.versioner, obj.obj)
		return objRV.rev >= rv
	}
	index := uint64(sort.Search(len(ms.kvList), f))
	return index
}

// send objEvent to watchers and remove watcher who has failure to receive event
func (ms *MemoryStore) sendEvent(event *objEvent) {
	ms.watchEventCache.addObjEvents(event)

	watchers := []*memoryStoreWatcher{}
	for _, v := range ms.watchers {
		if !v.stopped {
			v.incomingChan <- event
			watchers = append(watchers, v)
		}
	}
	ms.watchers = watchers
	ms.watchEventCache.shrink()
}

// sorted klist has empty slots spreaded when items are deleted and updated, if len of klist is more than a threshold longer, we want to shrink array to avoid memory waste
func (ms *MemoryStore) shrink() {
	ms.freezeWorld.Lock()
	defer ms.freezeWorld.Unlock()
	c, _ := ms.kvs.count([]string{})
	if int64(len(ms.kvList)) > c+NilSlotMemoryShrinkThrehold {
		ms.kvList = ms.kvList.shrink(c)
	}
}

// occupy a revision number and a position in sorted resource version list
func (ms *MemoryStore) reserveRevAndSlot() (uint64, uint64, error) {
	ms.revmu.Lock()
	defer ms.revmu.Unlock()
	rev := atomic.AddUint64(&_MemoryRev, 1)
	uindex := uint64(len(ms.kvList))
	ms.kvList = append(ms.kvList, nil)
	return rev, uindex, nil
}

func (ms *MemoryStore) getSingleObjectAsList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	resourceVersion := opts.ResourceVersion
	match := opts.ResourceVersionMatch
	pred := opts.Predicate

	var fromRV *uint64
	if len(resourceVersion) > 0 {
		parsedRV, err := ms.versioner.ParseResourceVersion(resourceVersion)
		if err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("invalid resource version: %v", err))
		}
		fromRV = &parsedRV
	}

	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return err
	}
	listRetVal, err := conversion.EnforcePtr(listPtr)
	if err != nil || listRetVal.Kind() != reflect.Slice {
		return fmt.Errorf("need ptr to slice: %v", err)
	}

	keys := strings.Split(key, "/")
	if obj := ms.kvs.get(keys); obj == nil {
		return ms.versioner.UpdateList(listObj, atomic.LoadUint64(&_MemoryRev), "", nil)
	} else {
		rv, err := ms.versioner.ObjectResourceVersion(obj.obj)
		if err != nil {
			return err
		}
		if fromRV != nil {
			switch match {
			case metav1.ResourceVersionMatchNotOlderThan:
				if rv >= *fromRV {
					appendListItem(listRetVal, obj.obj, rv, pred, ms.versioner)
				}
			case metav1.ResourceVersionMatchExact:
				if rv == *fromRV {
					appendListItem(listRetVal, obj.obj, rv, pred, ms.versioner)
				}
			case "":
				if rv > *fromRV {
					// append
					appendListItem(listRetVal, obj.obj, rv, pred, ms.versioner)
				}
			default:
				return fmt.Errorf("unknown ResourceVersionMatch value: %v", match)
			}
		} else {
			appendListItem(listRetVal, obj.obj, rv, pred, ms.versioner)
		}
		return ms.versioner.UpdateList(listObj, rv, "", nil)
	}
}

var _ apistorage.Interface = &MemoryStore{}
