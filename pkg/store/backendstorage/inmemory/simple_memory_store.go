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
	"errors"
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
	"k8s.io/apiserver/pkg/storage/etcd3"
)

const (
	NilSlotMemoryShrinkThrehold = 1000
)

type objEvent struct {
	key       string
	obj       runtime.Object
	oldObj    runtime.Object
	rev       uint64
	isDeleted bool
	isCreated bool
}

type objState struct {
	obj   runtime.Object
	meta  *storage.ResponseMeta
	rev   uint64
	stale bool
}

type objWithIndex struct {
	key   string
	obj   runtime.Object
	index uint64
}

type objList []*objWithIndex

func (list objList) shrink(length uint64) objList {
	newList := make([]*objWithIndex, length)
	i := uint64(0)
	for _, v := range list {
		if v != nil {
			newList[i] = v
			v.index = i
			i += 1
		}
	}
	return newList
}

type MemoryStore struct {
	versioner           storage.Versioner
	mu                  sync.RWMutex
	kvs                 map[string]*objWithIndex
	klist               objList
	groupResource       schema.GroupResource
	groupResourceString string
	pagingEnabled       bool
	watchers            []*memoryStoreWatcher
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
			versioner:           etcd3.APIObjectVersioner{},
			mu:                  sync.RWMutex{},
			kvs:                 map[string]*objWithIndex{},
			klist:               []*objWithIndex{},
			groupResource:       groupResource,
			groupResourceString: groupResource.String(),
			pagingEnabled:       pagingEnabled,
			watchers:            []*memoryStoreWatcher{},
		}
		_InMemoryStores[key] = si
		return si
	}
}

// Load initialize memory and load persisted data
func (ms *MemoryStore) Load() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return nil
}

// Stop cleanup memory
func (ms *MemoryStore) Stop() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return nil
}

// Count implements storage.Interface
func (ms *MemoryStore) Count(key string) (int64, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return int64(len(ms.kvs)), nil
}

// Create implements storage.Interface
func (ms *MemoryStore) Create(ctx context.Context, key string, obj runtime.Object, out runtime.Object, ttl uint64) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return err
	}

	klog.InfoS("GWJ create object", "key", key)
	if _, found := ms.kvs[key]; found {
		return storage.NewKeyExistsError(key, 0)
	} else {
		if version, err := ms.versioner.ObjectResourceVersion(obj); err == nil && version != 0 {
			return errors.New("resourceVersion should not be set on objects to be created")
		}
		if err := ms.versioner.PrepareObjectForStorage(obj); err != nil {
			return fmt.Errorf("PrepareObjectForStorage failed: %v", err)
		}
		rv := atomic.AddUint64(&_MemoryRev, 1)
		uindex := uint64(len(ms.klist))
		ms.versioner.UpdateObject(obj, uint64(rv))
		objWi := &objWithIndex{
			key:   key,
			obj:   obj.DeepCopyObject(),
			index: uindex,
		}
		ms.kvs[key] = objWi
		ms.klist = append(ms.klist, objWi)
		// sorted klist has empty slots spreaded when items are deleted and updated, if len of klist is more than a threshold longer, we want to shrink array to avoid memory waste
		if len(ms.klist) > len(ms.kvs)+NilSlotMemoryShrinkThrehold {
			ms.klist = ms.klist.shrink(uint64(len(ms.kvs)))
		}
		outVal.Set(reflect.ValueOf(obj).Elem())
		event := &objEvent{
			key:       key,
			obj:       out.DeepCopyObject(),
			oldObj:    nil,
			rev:       rv,
			isDeleted: false,
			isCreated: true,
		}
		ms.sendEvent(event)
	}
	return nil
}

// Delete implements storage.Interface
func (ms *MemoryStore) Delete(ctx context.Context, key string, out runtime.Object, preconditions *storage.Preconditions, validateDeletion storage.ValidateObjectFunc, cachedExistingObject runtime.Object) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	klog.InfoS("GWJ delete object", "key", key)
	if existingObj, found := ms.kvs[key]; !found {
		return storage.NewKeyNotFoundError(key, 0)
	} else {
		currState, err := getStateFromObject(ms.versioner, existingObj.obj)
		if err != nil {
			return err
		}

		if cachedExistingObject != nil {
			_, err := getStateFromObject(ms.versioner, cachedExistingObject)
			if err != nil {
				return err
			}
		} else {
			_, err := getStateFromObject(ms.versioner, existingObj.obj)
			if err != nil {
				return err
			}
		}

		if preconditions != nil {
			if err := preconditions.Check(key, currState.obj); err != nil {
				return err
			}
		}

		if err := validateDeletion(ctx, currState.obj); err != nil {
			return err
		}

		rv := atomic.AddUint64(&_MemoryRev, 1)
		delete(ms.kvs, key)
		ms.klist[existingObj.index] = nil
		// sorted klist has some empty slot, kvs has exact num of obj, if len of klist is a threshold longer, we want to shrink array to avoid memory waste
		if len(ms.klist) > len(ms.kvs)+NilSlotMemoryShrinkThrehold {
			ms.klist = ms.klist.shrink(uint64(len(ms.kvs)))
		}
		out = existingObj.obj.DeepCopyObject()
		event := &objEvent{
			key:       key,
			obj:       nil,
			oldObj:    out.DeepCopyObject(),
			rev:       rv,
			isDeleted: true,
			isCreated: false,
		}
		ms.sendEvent(event)
	}

	return nil
}

// Get implements storage.Interface
func (ms *MemoryStore) Get(ctx context.Context, key string, opts storage.GetOptions, out runtime.Object) error {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return fmt.Errorf("unable to convert output object to pointer: %v", err)
	}

	if existingObj, found := ms.kvs[key]; !found {
		if opts.IgnoreNotFound {
			return runtime.SetZeroValue(out)
		}
		return storage.NewKeyNotFoundError(key, 0)
	} else {
		existingObjState, err := getStateFromObject(ms.versioner, existingObj.obj)
		if err != nil {
			return storage.NewInternalError(err.Error())
		}

		if err := ms.validateMinimumResourceVersion(ms.versioner, opts.ResourceVersion, existingObjState.meta.ResourceVersion); err != nil {
			return err
		}

		outVal.Set(reflect.ValueOf(existingObj.obj).Elem())
	}
	return nil
}

// GetList implements storage.Interface
func (ms *MemoryStore) GetList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	klog.InfoS("GWJ get list of object", "key", key)
	totalCount := int64(len(ms.kvs))
	recursive := opts.Recursive
	resourceVersion := opts.ResourceVersion
	match := opts.ResourceVersionMatch
	pred := opts.Predicate

	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return err
	}
	listRetVal, err := conversion.EnforcePtr(listPtr)
	if err != nil || listRetVal.Kind() != reflect.Slice {
		return fmt.Errorf("need ptr to slice: %v", err)
	}

	if recursive && !strings.HasSuffix(key, "/") {
		key += "/"
	}
	keyPrefix := key

	var limit uint64 = math.MaxInt64
	if ms.pagingEnabled && pred.Limit > 0 {
		limit = uint64(pred.Limit)
	} else {
		limit = uint64(len(ms.kvs))
	}

	var fromRV *uint64
	if len(resourceVersion) > 0 {
		parsedRV, err := ms.versioner.ParseResourceVersion(resourceVersion)
		if err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("invalid resource version: %v", err))
		}
		fromRV = &parsedRV
	}

	var returnedRV, withRV uint64
	var continueKey string
	switch {
	case len(pred.Continue) > 0:
		var continueRV int64
		continueKey, continueRV, err = decodeContinue(pred.Continue, keyPrefix)
		if err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("invalid continue token: %v", err))
		}

		if len(resourceVersion) > 0 && resourceVersion != "0" {
			return apierrors.NewBadRequest("specifying resource version is not allowed when using continue")
		}

		if continueRV > 0 {
			withRV = uint64(continueRV)
			returnedRV = uint64(continueRV)
		} else if continueRV == 0 {
			return apierrors.NewBadRequest("0 continue resource version is not allowed when using continue")
		}
	default:
		if fromRV != nil {
			if *fromRV > 0 {
				withRV = *fromRV
			} else {
				// query from beginning
				withRV = 0
			}
		}
	}

	var uindex uint64 = 0
	if len(continueKey) > 0 {
		// use continueKey to check if rv is same as passed rv
		if obj, found := ms.kvs[continueKey]; found {
			objRV, _ := ms.versioner.ObjectResourceVersion(obj.obj)
			if objRV == uint64(withRV) {
				uindex = obj.index
			}
		}
	}

	// object with continueKey could be updated since last return, use withRV do a binary search
	if uindex == 0 {
		if withRV != 0 {
			uindex = ms.binarySearch(withRV)
		} else {
			uindex = uint64(0)
		}
	}

	if uindex >= uint64(len(ms.klist)) {
		// revsion >= withRV has been removed
		returnedRV = atomic.LoadUint64(&_MemoryRev)
		resp := ms.versioner.UpdateList(listObj, uint64(returnedRV), "", nil)
		return resp
	}

	// loop until we have filled the requested limit from etcd or there are no more results
	hasMore := false
	lastKey := ""
	returnedRV = atomic.LoadUint64(&_MemoryRev)
	listBufferLen := uint64(len(ms.klist))
	for i := uindex; i < listBufferLen; i++ {
		v := ms.klist[i]
		if v != nil {
			rv, _ := ms.versioner.ObjectResourceVersion(v.obj)
			returnedRV = rv
			lastKey = v.key
			if !strings.HasPrefix(lastKey, keyPrefix) {
				continue
			}
			switch match {
			case metav1.ResourceVersionMatchNotOlderThan:
				if rv > uint64(withRV) {
					appendListItem(listRetVal, v.obj, rv, pred, ms.versioner)
				}
			case metav1.ResourceVersionMatchExact:
				appendListItem(listRetVal, v.obj, rv, pred, ms.versioner)
			case "":
				// append
				appendListItem(listRetVal, v.obj, rv, pred, ms.versioner)
			default:
				return fmt.Errorf("unknown ResourceVersionMatch value: %v", match)
			}
		}
		// have got enough items, check if there are still items in list
		if uint64(listRetVal.Len()) == limit {
			if i < listBufferLen {
				hasMore = true
			}
			break
		}
	}

	if hasMore {
		next, err := encodeContinue(lastKey, keyPrefix, returnedRV)
		if err != nil {
			return err
		}
		var remainingItemCount *int64
		if pred.Empty() {
			c := int64(totalCount - pred.Limit)
			remainingItemCount = &c
		}
		resp := ms.versioner.UpdateList(listObj, returnedRV, next, remainingItemCount)
		return resp
	}

	// no continuation
	resp := ms.versioner.UpdateList(listObj, returnedRV, "", nil)
	return resp
}

// GuaranteedUpdate implements storage.Interface
func (ms *MemoryStore) GuaranteedUpdate(ctx context.Context, key string, out runtime.Object, ignoreNotFound bool, preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc, cachedExistingObject runtime.Object) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	klog.InfoS("GWJ update object", "key", key, "existing obj", cachedExistingObject)
	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return fmt.Errorf("unable to convert output object to pointer: %v", err)
	}

	if oldObjWi, found := ms.kvs[key]; !found {
		if ignoreNotFound {
			return runtime.SetZeroValue(out)
		}
		return storage.NewKeyNotFoundError(key, 0)
	} else {
		oldObj := oldObjWi.obj.DeepCopyObject()
		currState, err := getStateFromObject(ms.versioner, oldObjWi.obj)
		if err != nil {
			return err
		}

		if cachedExistingObject != nil {
			s, err := getStateFromObject(ms.versioner, cachedExistingObject)
			if err != nil {
				klog.ErrorS(err, "GWJ failed to update object", "key", key, "existing obj", cachedExistingObject)
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
		rv := atomic.AddUint64(&_MemoryRev, 1)
		ms.versioner.UpdateObject(ret, uint64(rv))

		uindex := uint64(len(ms.klist))
		newObjWi := &objWithIndex{
			key:   key,
			obj:   ret.DeepCopyObject(),
			index: uindex,
		}
		ms.kvs[key] = newObjWi
		ms.klist[oldObjWi.index] = nil
		ms.klist = append(ms.klist, newObjWi)
		if len(ms.klist) > len(ms.kvs)+NilSlotMemoryShrinkThrehold {
			ms.klist = ms.klist.shrink(uint64(len(ms.kvs)))
		}
		outVal.Set(reflect.ValueOf(ret).Elem())
		event := &objEvent{
			key:       key,
			obj:       ret.DeepCopyObject(),
			oldObj:    oldObj,
			rev:       rv,
			isDeleted: false,
			isCreated: false,
		}
		ms.sendEvent(event)
	}

	return nil
}

// Versioner implements storage.Interface
func (ms *MemoryStore) Versioner() storage.Versioner {
	return ms.versioner
}

// Watch implements storage.Interface
func (ms *MemoryStore) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	return ms._watch(ctx, key, opts, false)
}

// WatchWithOldObj implements FornaxStorage
func (ms *MemoryStore) WatchWithOldObj(ctx context.Context, key string, opts storage.ListOptions) (store.WatchWithOldObjInterface, error) {
	return ms._watch(ctx, key, opts, true)
}

// Watch implements FornaxStorage
func (ms *MemoryStore) Update(ctx context.Context, key string, ignoreNotFound bool, preconditions *storage.Preconditions, updatedObj runtime.Object, output runtime.Object) error {
	return ms.GuaranteedUpdate(ctx, key, output, ignoreNotFound, preconditions, ms._getTryUpdateFunc(updatedObj), nil)
}

func (ms *MemoryStore) _getTryUpdateFunc(updating runtime.Object) apistorage.UpdateFunc {
	return func(existing runtime.Object, res storage.ResponseMeta) (runtime.Object, *uint64, error) {
		existingVersion, err := ms.versioner.ObjectResourceVersion(existing)
		if err != nil {
			return nil, nil, err
		}
		updatingVersion, err := ms.versioner.ObjectResourceVersion(updating)
		if existingVersion != updatingVersion {
			return nil, nil, fmt.Errorf("object is already updated to a newer version, try to reget object and update again")
		}
		return updating, nil, nil
	}
}

func (ms *MemoryStore) _watch(ctx context.Context, key string, opts storage.ListOptions, withOldObj bool) (*memoryStoreWatcher, error) {
	klog.InfoS("GWJ watch object", "key", key, "ops", opts, "resource", ms.groupResourceString)
	rev, err := ms.versioner.ParseResourceVersion(opts.ResourceVersion)
	if err != nil {
		return nil, err
	}

	// start to watch new events
	watcher := NewMemoryStoreWatcher(ctx, key, opts.Recursive, opts.ProgressNotify, opts.Predicate)
	ms.watchers = append(ms.watchers, watcher)

	// find all obj event which are greater than passed rev and call watcher to run with these existing events
	objEvents, err := ms.getObjEventsAfterRev(key, opts)
	if err != nil {
		return nil, err
	}
	go watcher.run(rev, objEvents, withOldObj)
	return watcher, nil
}

func (ms *MemoryStore) getObjEventsAfterRev(key string, opts storage.ListOptions) ([]*objEvent, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	prefix := key
	if opts.Recursive && !strings.HasSuffix(key, "/") {
		prefix += "/"
	}

	rev, err := ms.versioner.ParseResourceVersion(opts.ResourceVersion)
	if err != nil {
		return nil, err
	}

	objEvents := []*objEvent{}
	uindex := ms.binarySearch(rev)
	for i := uindex; i < uint64(len(ms.klist)); i++ {
		v := ms.klist[i]
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
			if oState.rev == uint64(rev) {
				objEvents = append(objEvents, e)
			}
		case "":
			// append
			objEvents = append(objEvents, e)
		default:
			return nil, fmt.Errorf("Unknown ResourceVersionMatch value: %s", opts.ResourceVersionMatch)
		}
	}

	return objEvents, nil
}

func (ms *MemoryStore) binarySearch(rv uint64) uint64 {
	f := func(i int) bool {
		obj := ms.klist[(i)%len(ms.klist)]
		if obj == nil {
			return false
		}
		objRV, _ := getStateFromObject(ms.versioner, obj.obj)
		return objRV.rev >= rv
	}
	index := uint64(sort.Search(len(ms.klist), f))
	return index
}

// send objEvent to watchers and remove watcher who has failure to receive event
func (ms *MemoryStore) sendEvent(event *objEvent) {
	watchers := []*memoryStoreWatcher{}
	for _, v := range ms.watchers {
		if !v.stopped {
			v.incomingChan <- event
			watchers = append(watchers, v)
		}
	}
	ms.watchers = watchers
}

var _ apistorage.Interface = &MemoryStore{}
