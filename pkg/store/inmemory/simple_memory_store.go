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
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"centaurusinfra.io/fornax-serverless/pkg/store"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	apistorage "k8s.io/apiserver/pkg/storage"
)

type MemoryStore struct {
	versioner        apistorage.Versioner
	revmu            sync.RWMutex
	stopChannel      chan interface{}
	kvs              *objStoreMap
	revSortedObjList *objList
	groupResource    schema.GroupResource
	grvKeyPrefix     string
	watchers         []*memoryStoreWatcher

	keyFunc      func(obj runtime.Object) (string, error)
	newFunc      func() runtime.Object
	newListFunc  func() runtime.Object
	getAttrsFunc apistorage.AttrFunc
	triggerFuncs apistorage.IndexerFuncs
	indexers     *cache.Indexers
}

// a workgroud to make sure memory revision will not move back, if machine clock does not rewind, need better option
var (
	_MemoryRev = uint64(2<<61) + uint64(time.Now().UnixNano())
)

// DefaultObjRevListInitSize must lager than DefaultObjRevListGrowThread, when revSortedObjList is filled up with empty slot less than DefaultObjRevListGrowThreashold, grow it DefaultObjRevListGrowThreashold
const (
	DefaultObjRevListInitSize       = 20000
	DefaultObjRevListGrowThreashold = 10000
	NilSlotShrinkLowThrehold        = 10000
	NilSlotShrinkHighThrehold       = 20000
	DefaultHouseKeepingInterval     = 60 * time.Second
)

// NewMemoryStore return a singleton storage.Interface for a groupResource
func NewMemoryStore(ctx context.Context, groupResource schema.GroupResource, grvKeyPrefix string, newFunc func() runtime.Object, newListFunc func() runtime.Object) *MemoryStore {
	key := groupResource.String()
	klog.InfoS("New or Get a in memory store for", "resource", key)
	si := &MemoryStore{
		versioner:   store.APIObjectVersioner{},
		revmu:       sync.RWMutex{},
		stopChannel: make(chan interface{}),
		newFunc:     newFunc,
		newListFunc: newListFunc,
		kvs:         &objStoreMap{mu: sync.RWMutex{}, kvs: map[string]objMapOrObj{}},
		revSortedObjList: &objList{
			objs:         make([]*objWithIndex, DefaultObjRevListInitSize),
			lastObjIndex: 0,
		},
		grvKeyPrefix:  grvKeyPrefix, // resource key prefix, every key should start with it
		groupResource: groupResource,
		watchers:      []*memoryStoreWatcher{},
	}
	ticker := time.NewTicker(DefaultHouseKeepingInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				si.houseKeeping()
			case <-si.stopChannel:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return si
}

// this is ugly, just want to let compatible with k8s api server store initialization
func (ms *MemoryStore) CompleteWithFunctions(
	keyFunc func(obj runtime.Object) (string, error),
	newFunc func() runtime.Object,
	newListFunc func() runtime.Object,
	getAttrsFunc apistorage.AttrFunc,
	triggerFuncs apistorage.IndexerFuncs,
	indexers *cache.Indexers,
) error {
	ms.keyFunc = keyFunc
	ms.newFunc = newFunc
	ms.newListFunc = newListFunc
	ms.getAttrsFunc = getAttrsFunc
	ms.triggerFuncs = triggerFuncs
	ms.indexers = indexers
	return nil
}

func (ms *MemoryStore) houseKeeping() {
	ms.revmu.Lock()
	defer ms.revmu.Unlock()
	klog.InfoS("Shrink revSortedObjList before", "size", ms.revSortedObjList.Len(), "last index", ms.revSortedObjList.lastObjIndex)
	st := time.Now().UnixMicro()
	c, _ := ms.kvs.count([]string{})
	if ms.revSortedObjList.lastObjIndex > uint64(c+NilSlotShrinkHighThrehold) {
		ms.revSortedObjList.shrink(uint64(c + NilSlotShrinkLowThrehold))
	}
	et := time.Now().UnixMicro()
	klog.InfoS("shrink revSortedObjList after", "size", ms.revSortedObjList.Len(), "last index", ms.revSortedObjList.lastObjIndex, "took-micro", et-st)
}

func (ms *MemoryStore) getKey(obj runtime.Object) (string, error) {
	if ms.keyFunc != nil {
		return ms.keyFunc(obj)
	} else {
		key := fmt.Sprintf("%s/%s", ms.grvKeyPrefix, util.Name(obj))
		return key, nil
	}
}

// Stop cleanup memory
func (ms *MemoryStore) Stop() error {
	ms.stopChannel <- "stop"
	return nil
}

// Count implements storage.Interface
func (ms *MemoryStore) Count(key string) (int64, error) {
	count, err := ms.kvs.count(strings.Split(key, "/"))
	return count, err
}

// Create implements storage.Interface
func (ms *MemoryStore) Create(ctx context.Context, key string, obj runtime.Object, out runtime.Object, ttl uint64) error {
	st := time.Now().UnixMicro()
	defer func() {
		et := time.Now().UnixMicro()
		klog.InfoS("Memory store create object", "key", key, "took-micro", et-st)
	}()
	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return err
	}

	if err := store.PrepareObjectForStorage(obj); err != nil {
		return fmt.Errorf("PrepareObjectForStorage failed: %v", err)
	}

	keys := strings.Split(key, "/")
	if o := ms.kvs.get(keys); o != nil {
		return apistorage.NewKeyExistsError(key, 0)
	} else {
		rev, index, err := ms.reserveRevAndSlot()
		if err != nil {
			return err
		}

		newObj := obj.DeepCopyObject()
		store.SetObjectResourceVersion(newObj, rev)
		objWi := &objWithIndex{
			key:     key,
			obj:     newObj.DeepCopyObject(),
			index:   index,
			deleted: false,
		}
		err = ms.kvs.put(keys, objWi, 0)
		if err != nil {
			return err
		}
		ms.revSortedObjList.objs[index] = objWi
		outVal.Set(reflect.ValueOf(newObj).Elem())

		event := &objEvent{
			key:       key,
			obj:       newObj.DeepCopyObject(),
			oldObj:    nil,
			rev:       rev,
			isDeleted: false,
			isCreated: true,
		}
		ms.sendEvent(event)
	}
	return nil
}

// Delete implements storage.Interface, deleted object is removed from kv map, but still keep in revisonedObjList,
// deleted object is removed from old poistion in list but append to end of list just like a updated object,
// so, it ensure watcher can get this deleted obj event if deleted object just happen after watcher's list call and before watch call
func (ms *MemoryStore) Delete(ctx context.Context, key string, out runtime.Object, preconditions *apistorage.Preconditions, validateDeletion apistorage.ValidateObjectFunc, cachedExistingObject runtime.Object) error {
	st := time.Now().UnixMicro()
	defer func() {
		et := time.Now().UnixMicro()
		klog.InfoS("Memory store delete object", "key", key, "took-micro", et-st)
	}()
	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return err
	}

	if cachedExistingObject != nil {
		_, err := store.GetObjectResourceVersion(cachedExistingObject)
		if err != nil {
			return apistorage.NewInternalError(err.Error())
		}
	}

	keys := strings.Split(key, "/")
	if existingObj := ms.kvs.get(keys); existingObj == nil {
		return apistorage.NewKeyNotFoundError(key, 0)
	} else {
		currObj := existingObj.obj.DeepCopyObject()
		_, err := store.GetObjectResourceVersion(currObj)
		if err != nil {
			return apistorage.NewInternalError(err.Error())
		}

		if preconditions != nil {
			if err := preconditions.Check(key, currObj); err != nil {
				return err
			}
		}

		if validateDeletion != nil {
			if err := validateDeletion(ctx, currObj); err != nil {
				return err
			}
		}

		rev, index, err := ms.reserveRevAndSlot()
		if err != nil {
			return err
		}

		deletedObj := currObj.DeepCopyObject()
		store.SetObjectResourceVersion(deletedObj, rev)
		deletedObjWi := &objWithIndex{
			key:     key,
			obj:     deletedObj,
			index:   index,
			deleted: true,
		}
		err = ms.kvs.del(keys)
		if err != nil {
			return err
		}
		ms.revSortedObjList.objs[existingObj.index] = nil
		ms.revSortedObjList.objs[index] = deletedObjWi
		outVal.Set(reflect.ValueOf(currObj).Elem())

		event := &objEvent{
			key:       key,
			obj:       nil,
			oldObj:    deletedObj.DeepCopyObject(),
			rev:       rev,
			isDeleted: true,
			isCreated: false,
		}
		ms.sendEvent(event)
	}
	return nil
}

// Get implements storage.Interface
func (ms *MemoryStore) Get(ctx context.Context, key string, opts apistorage.GetOptions, out runtime.Object) error {
	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return fmt.Errorf("unable to convert output object to pointer: %v", err)
	}

	keys := strings.Split(key, "/")
	if existingObj := ms.kvs.get(keys); existingObj == nil {
		if opts.IgnoreNotFound {
			return runtime.SetZeroValue(out)
		}
		return apistorage.NewKeyNotFoundError(key, 0)
	} else {
		currObj := existingObj.obj.DeepCopyObject()
		currObjRv, err := store.GetObjectResourceVersion(currObj)
		if err != nil {
			return apistorage.NewInternalError(err.Error())
		}

		if err := store.ValidateMinimumResourceVersion(opts.ResourceVersion, currObjRv); err != nil {
			return err
		}

		outVal.Set(reflect.ValueOf(existingObj.obj).Elem())
	}
	return nil
}

// GetList implements k8s storage.Interface,
// if provided Continue key and revision is not nil, use Continue key to find the object and use this object's index in revisonedObjList as starting position.
// if this object is deleted already, use Continue rv to search revisonedObjList to do a binary search to find starting position in revisonedObjList
// if no Continue key provided, use provided ResourceVersion to do a binary search to find find starting positon in revisonedObjList
// and iterate revisonedObjList from starting position to return a list of object, ignore obj which is marked as deleted.
func (ms *MemoryStore) GetList(ctx context.Context, key string, opts apistorage.ListOptions, listObj runtime.Object) error {
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
		parsedRV, err := store.ParseResourceVersion(resourceVersion)
		if err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("invalid resource version: %v", err))
		}
		fromRV = &parsedRV
	}

	// returnedRV is returned back to client to indicate the last revision of returned list, intialized it as 1
	returnedRV := uint64(1)
	// withRV is used to binary search in revisonedObjList to get starting index to return object list
	withRV := uint64(0)
	startingIndex := uint64(0)
	var continueKey string
	switch {
	case len(pred.Continue) > 0:
		if len(resourceVersion) > 0 && resourceVersion != "0" {
			return apierrors.NewBadRequest("specifying resource version is not allowed when using continue")
		}

		var continueRV int64
		continueKey, continueRV, err = store.DecodeContinue(pred.Continue, keyPrefix)
		if err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("invalid continue token: %v", err))
		}
		if continueRV > 0 {
			// withRV is set to continueRV if provided
			withRV = uint64(continueRV)
			returnedRV = withRV
		} else if continueRV == 0 {
			return apierrors.NewBadRequest("0 continue resource version is not allowed when using continue")
		}
		// use continueKey to find object from kv map,
		// if object exist, check if its rv is still same as continue rv,
		// then use this object index as startIndex to search revisonedObjList to return object list
		// if object's revision is not same continueRV or object does not exist anymore(deleted)
		// use continue rv to do binary search in revisonedObjList to find starting index
		continueKeys := strings.Split(continueKey, "/")
		if obj := ms.kvs.get(continueKeys); obj != nil {
			objRV, _ := store.GetObjectResourceVersion(obj.obj)
			if objRV == withRV {
				startingIndex = obj.index
			} else {
				startingIndex = ms.binarySearchInObjList(withRV)
			}
		}
	default:
		// no continue key, but with resource version, start to search from provided resource version
		if fromRV != nil && *fromRV > 0 {
			withRV = *fromRV
			returnedRV = *fromRV
			startingIndex = ms.binarySearchInObjList(*fromRV)
		}
	}

	// can not find valid searching index from list, return empty list
	if startingIndex >= uint64(ms.revSortedObjList.Len()) {
		return store.UpdateList(listObj, uint64(returnedRV), "", nil)
	}

	// range sorted revision list from searching index until meet requested limit or there are no more items
	limit := int64(math.MaxInt64)
	if pred.Limit > 0 {
		limit = pred.Limit
	}
	remainingItemCount := int64(0)
	hasMore := false
	lastKey := ""
	lastRev := withRV
	err = func() error {
		ms.revmu.RLock()
		defer ms.revmu.RUnlock()
		objBufferLen := atomic.LoadUint64(&ms.revSortedObjList.lastObjIndex)
		for i := startingIndex; i <= objBufferLen; i++ {
			v := ms.revSortedObjList.objs[i]
			// deleted object are also in list, ignore it for GetList call, but deleted object will be returned in watch call
			if v != nil && v.deleted == false {
				rv, _ := store.GetObjectResourceVersion(v.obj)
				lastKey = v.key
				lastRev = rv
				if !strings.HasPrefix(lastKey, keyPrefix) {
					continue
				}
				switch match {
				case metav1.ResourceVersionMatchNotOlderThan:
					if rv >= withRV {
						store.AppendListItem(listRetVal, v.obj, rv, pred)
					}
				case metav1.ResourceVersionMatchExact:
					if rv > withRV {
						store.AppendListItem(listRetVal, v.obj, rv, pred)
					}
				case "":
					if rv > withRV {
						store.AppendListItem(listRetVal, v.obj, rv, pred)
					}
				default:
					return fmt.Errorf("unknown ResourceVersionMatch value: %v", match)
				}
			}
			// got enough items, check if there are still items in list
			if int64(listRetVal.Len()) == limit {
				if i < objBufferLen-1 {
					remainingItemCount = int64(objBufferLen - 1 - i)
					hasMore = true
				}
				break
			}
		}
		return nil
	}()
	if err != nil {
		return err
	}

	if hasMore {
		continueToken, err := store.EncodeContinue(lastKey, keyPrefix, lastRev)
		if err != nil {
			return err
		}
		if pred.Empty() {
			return store.UpdateList(listObj, returnedRV, continueToken, &remainingItemCount)
		} else {
			return store.UpdateList(listObj, returnedRV, continueToken, nil)
		}
	}

	// no more items, use empty string as continue token
	return store.UpdateList(listObj, returnedRV, "", nil)
}

// GuaranteedUpdate implements k8s storage.Interface, updated object will get an new revision,
// its previous positon in revSortedObjList is set to nil, updated object is appended to end of revSortedObjList
func (ms *MemoryStore) GuaranteedUpdate(ctx context.Context, key string, out runtime.Object, ignoreNotFound bool, preconditions *apistorage.Preconditions, tryUpdate apistorage.UpdateFunc, cachedExistingObject runtime.Object) error {
	st := time.Now().UnixMicro()
	defer func() {
		et := time.Now().UnixMicro()
		klog.InfoS("Memory store update object", "key", key, "took-micro", et-st)
	}()
	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return fmt.Errorf("unable to convert output object to pointer: %v", err)
	}

	keys := strings.Split(key, "/")
	if curObjWi := ms.kvs.get(keys); curObjWi == nil {
		if ignoreNotFound {
			return runtime.SetZeroValue(out)
		}
		return apistorage.NewKeyNotFoundError(key, 0)
	} else {
		currObj := curObjWi.obj.DeepCopyObject()
		currRv, err := store.GetObjectResourceVersion(currObj)
		if err != nil {
			return apistorage.NewInternalError(err.Error())
		}

		if cachedExistingObject != nil {
			s, err := store.GetObjectResourceVersion(cachedExistingObject)
			if err != nil {
				return apistorage.NewInternalError(err.Error())
			}
			if s != currRv {
				klog.Warningf("provided cached existing object resource version is staled: cached RV %d, current RV %d", s, currRv)
			}
		}

		if preconditions != nil {
			if err := preconditions.Check(key, currObj); err != nil {
				return err
			}
		}

		// use try update to get a updated object of currObj, try update function should verify it's updating same revision of currObj
		ret, _, err := store.UpdateState(currObj, tryUpdate)
		if err != nil {
			return err
		}

		// bump updated object revsion
		rev, index, err := ms.reserveRevAndSlot()
		if err != nil {
			return err
		}
		if err := store.SetObjectResourceVersion(ret, rev); err != nil {
			return err
		}

		// update object in kv store, also pass currRv to check being updated object in kv store is still same revision
		newObjWi := &objWithIndex{
			key:     key,
			obj:     ret.DeepCopyObject(),
			index:   index,
			deleted: false,
		}
		err = ms.kvs.put(keys, newObjWi, currRv)
		if err != nil {
			return err
		}
		ms.revSortedObjList.objs[curObjWi.index] = nil
		ms.revSortedObjList.objs[newObjWi.index] = newObjWi
		outVal.Set(reflect.ValueOf(ret).Elem())

		event := &objEvent{
			key:       key,
			obj:       out.DeepCopyObject(),
			oldObj:    currObj,
			rev:       rev,
			isDeleted: false,
			isCreated: false,
		}
		ms.sendEvent(event)
	}
	return nil
}

// combine Create and Update in one call, Create an obj it it does not exist,
// or using mergeFunc to merge passed in obj and existing obj to get a updated obj,
// increase revision of updated obj, and put updated obj at end of of revSortedObjList
func (ms *MemoryStore) CreateOrUpdate(ctx context.Context, key string, obj runtime.Object, out runtime.Object, mergeFunc func(from runtime.Object, to runtime.Object) error) error {
	keys := strings.Split(key, "/")
	if curObjWi := ms.kvs.get(keys); curObjWi == nil {
		return ms.Create(ctx, key, obj, out, 0)
	} else {
		// merge exiting object into passed obj
		currObj := curObjWi.obj.DeepCopyObject()
		updatedObj := obj.DeepCopyObject()
		if mergeFunc != nil {
			err := mergeFunc(currObj, updatedObj)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("Do not have merge function to update existing object using provided obj")
		}

		return ms.GuaranteedUpdate(ctx, key, out, false, nil, store.GetTryUpdateFunc(updatedObj), currObj)
	}
}

// combine Get and Create in one call
// get a object if key exist, if not create a obj using passed objToCreate, and set a new revision for created obj, return it in out
func (ms *MemoryStore) GetOrCreate(ctx context.Context, key string, objToCreate runtime.Object, out runtime.Object) error {
	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return fmt.Errorf("unable to convert output object to pointer: %v", err)
	}

	keys := strings.Split(key, "/")
	if curObjWi := ms.kvs.get(keys); curObjWi == nil {
		return ms.Create(ctx, key, objToCreate, out, 0)
	} else {
		// get current object state
		currObj := curObjWi.obj.DeepCopyObject()
		outVal.Set(reflect.ValueOf(currObj).Elem())
	}

	return nil
}

// check if a object exist, if does not exit create a obj using passed objToCreate, and set a new revision for created obj, return it in out,
// if exist, use passed objToCreate to replace existing one, increase revision of object
func (ms *MemoryStore) CreateOrReplace(ctx context.Context, key string, objToCreate runtime.Object, out runtime.Object) error {
	outVal, err := conversion.EnforcePtr(out)
	if err != nil {
		return fmt.Errorf("unable to convert output object to pointer: %v", err)
	}

	keys := strings.Split(key, "/")
	if curObjWi := ms.kvs.get(keys); curObjWi == nil {
		return ms.Create(ctx, key, objToCreate, out, 0)
	} else {
		// get current object state
		currObj := curObjWi.obj.DeepCopyObject()
		currRv, err := store.GetObjectResourceVersion(currObj)
		if err != nil {
			return apistorage.NewInternalError(err.Error())
		}
		newObj := objToCreate.DeepCopyObject()
		rev, index, err := ms.reserveRevAndSlot()
		if err != nil {
			return err
		}
		store.SetObjectResourceVersion(newObj, rev)
		newObjWi := &objWithIndex{
			key:     key,
			obj:     newObj.DeepCopyObject(),
			index:   index,
			deleted: false,
		}

		err = ms.kvs.put(keys, newObjWi, currRv)
		if err != nil {
			return err
		}
		ms.revSortedObjList.objs[curObjWi.index] = nil
		ms.revSortedObjList.objs[index] = newObjWi
		outVal.Set(reflect.ValueOf(newObj).Elem())
		event := &objEvent{
			key:       key,
			obj:       newObj.DeepCopyObject(),
			oldObj:    currObj,
			rev:       rev,
			isDeleted: false,
			isCreated: false,
		}
		ms.sendEvent(event)
	}

	return nil
}

// Versioner implements k8s storage.Interface
func (ms *MemoryStore) Versioner() apistorage.Versioner {
	return ms.versioner
}

// Watch implements k8s storage.Interface
func (ms *MemoryStore) Watch(ctx context.Context, key string, opts apistorage.ListOptions) (watch.Interface, error) {
	return ms.watch(ctx, key, opts, false)
}

// WatchWithOldObj implements FornaxStorage
func (ms *MemoryStore) WatchWithOldObj(ctx context.Context, key string, opts apistorage.ListOptions) (store.WatchWithOldObjInterface, error) {
	return ms.watch(ctx, key, opts, true)
}

// EnsureUpdateAndDelete implements FornaxStorage, it update object and delete it if object has delete timestamp and empty finalizer, delete it
func (ms *MemoryStore) EnsureUpdateAndDelete(ctx context.Context, key string, ignoreNotFound bool, preconditions *apistorage.Preconditions, updatedObj runtime.Object, output runtime.Object) error {
	err := ms.GuaranteedUpdate(ctx, key, output, ignoreNotFound, preconditions, store.GetTryUpdateFunc(updatedObj), nil)
	if err != nil {
		return err
	}

	if store.ShouldDeleteSpec(output) {
		err = ms.Delete(ctx, key, output, preconditions, func(ctx context.Context, obj runtime.Object) error { return nil }, output)
	}

	return nil
}

func (ms *MemoryStore) watch(ctx context.Context, key string, opts apistorage.ListOptions, withOldObj bool) (*memoryStoreWatcher, error) {
	rev, err := store.ParseResourceVersion(opts.ResourceVersion)
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

func (ms *MemoryStore) getObjEventsAfterRev(key string, rev uint64, opts apistorage.ListOptions) ([]*objEvent, error) {
	prefix := key
	if opts.Recursive && !strings.HasSuffix(key, "/") {
		prefix += "/"
	}

	rev, err := store.ParseResourceVersion(opts.ResourceVersion)
	if err != nil {
		return nil, err
	}

	objEvents := []*objEvent{}
	uindex := ms.binarySearchInObjList(rev)
	for i := uindex; i < uint64(ms.revSortedObjList.Len()); i++ {
		v := ms.revSortedObjList.objs[i]
		if v == nil || !strings.HasPrefix(v.key, prefix) {
			continue
		}
		oRev, _ := store.GetObjectResourceVersion(v.obj)

		var e *objEvent
		if v.deleted {
			e = &objEvent{
				key:       v.key,
				obj:       nil,
				oldObj:    v.obj,
				rev:       oRev,
				isDeleted: true,
				isCreated: false,
			}
		} else {
			e = &objEvent{
				key:       v.key,
				obj:       v.obj,
				oldObj:    nil,
				rev:       oRev,
				isDeleted: false,
				isCreated: true,
			}
		}

		switch opts.ResourceVersionMatch {
		case metav1.ResourceVersionMatchNotOlderThan:
			if oRev >= uint64(rev) {
				objEvents = append(objEvents, e)
			}
		case metav1.ResourceVersionMatchExact:
			if oRev > uint64(rev) {
				objEvents = append(objEvents, e)
			}
		case "":
			// append
			if oRev > uint64(rev) {
				objEvents = append(objEvents, e)
			}
		default:
			return nil, fmt.Errorf("Unknown ResourceVersionMatch value: %s", opts.ResourceVersionMatch)
		}
	}

	return objEvents, nil
}

func (ms *MemoryStore) binarySearchInObjList(rv uint64) uint64 {
	f := func(i int) bool {
		obj := ms.revSortedObjList.objs[(i)%ms.revSortedObjList.Len()]
		if obj == nil {
			return false
		}
		objRV, _ := store.GetObjectResourceVersion(obj.obj)
		return objRV >= rv
	}
	index := uint64(sort.Search(ms.revSortedObjList.Len(), f))
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

// occupy a revision number and a position in sorted revisioned object list
// sorted klist has empty slots spreaded when items are deleted and updated,
// if len of klist is more than a threshold, we want to shrink array to avoid memory waste
func (ms *MemoryStore) reserveRevAndSlot() (uint64, uint64, error) {
	ms.revmu.Lock()
	defer ms.revmu.Unlock()
	rev := atomic.AddUint64(&_MemoryRev, 1)
	uindex := atomic.AddUint64(&ms.revSortedObjList.lastObjIndex, 1)
	if uint64(ms.revSortedObjList.Len()) < uindex+DefaultObjRevListGrowThreashold {
		ms.revSortedObjList.grow(DefaultObjRevListGrowThreashold)
	}
	return rev, uindex, nil
}

func (ms *MemoryStore) getSingleObjectAsList(ctx context.Context, key string, opts apistorage.ListOptions, listObj runtime.Object) error {
	resourceVersion := opts.ResourceVersion
	match := opts.ResourceVersionMatch
	pred := opts.Predicate

	var fromRV *uint64
	if len(resourceVersion) > 0 {
		parsedRV, err := store.ParseResourceVersion(resourceVersion)
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
		return store.UpdateList(listObj, atomic.LoadUint64(&_MemoryRev), "", nil)
	} else {
		rv, err := store.GetObjectResourceVersion(obj.obj)
		if err != nil {
			return err
		}
		if fromRV != nil {
			switch match {
			case metav1.ResourceVersionMatchNotOlderThan:
				if rv >= *fromRV {
					store.AppendListItem(listRetVal, obj.obj, rv, pred)
				}
			case metav1.ResourceVersionMatchExact:
				if rv == *fromRV {
					store.AppendListItem(listRetVal, obj.obj, rv, pred)
				}
			case "":
				if rv > *fromRV {
					// append
					store.AppendListItem(listRetVal, obj.obj, rv, pred)
				}
			default:
				return fmt.Errorf("unknown ResourceVersionMatch value: %v", match)
			}
		} else {
			store.AppendListItem(listRetVal, obj.obj, rv, pred)
		}
		return store.UpdateList(listObj, rv, "", nil)
	}
}

var _ apistorage.Interface = &MemoryStore{}
