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
	"fmt"
	"sync"

	"centaurusinfra.io/fornax-serverless/pkg/store"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/storage"
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
	obj  runtime.Object
	meta *storage.ResponseMeta
	rev  uint64
}

type objWithIndex struct {
	key        string
	obj        runtime.Object
	index      uint64
	storageRev int64
}

type objList []*objWithIndex

func (list objList) shrink(length int64) objList {
	klog.InfoS("GWJ shrink memory storage list", "len", len(list), "to", length)
	newList := make([]*objWithIndex, length)
	i := uint64(0)
	for _, v := range list {
		if v != nil {
			newList[i] = v
			v.index = i
			i += 1
		}
	}
	klog.InfoS("GWJ done shrink memory storage list", "len", len(newList))
	return newList
}

type objMapOrObj struct {
	obj    *objWithIndex
	objMap *memoryStoreMap
}

type memoryStoreMap struct {
	mu  sync.RWMutex
	kvs map[string]objMapOrObj
	num int64
}

func (mm *memoryStoreMap) count(keys []string) (int64, error) {
	if len(keys) == 0 {
		return mm._countItemsInMap(), nil
	}

	thismm := mm
	count := int64(0)
	for i := 0; i < len(keys); i++ {
		key := keys[i]
		if o := thismm._get(key); o == nil {
			continue
		} else {
			if i != len(keys)-1 {
				if o.obj != nil {
					return 0, fmt.Errorf("%dth middle node of keys %v is not a map", i, keys)
				} else {
					thismm = o.objMap
				}
			} else {
				// reach to end of keys, count items from here
				if o.obj != nil {
					return 1, nil
				} else {
					return o.objMap._countItemsInMap(), nil
				}
			}
		}
	}
	return count, nil
}

func (mm *memoryStoreMap) _countItemsInMap() int64 {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	count := int64(0)
	for _, v := range mm.kvs {
		if v.obj != nil {
			count += 1
		} else {
			count += v.objMap._countItemsInMap()
		}
	}
	return count
}

func (mm *memoryStoreMap) _get(key string) *objMapOrObj {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	if o, f := mm.kvs[key]; f {
		return &o
	}
	return nil
}

func (mm *memoryStoreMap) get(keys []string) *objWithIndex {
	if len(keys) == 0 {
		return nil
	}

	thismm := mm
	for i := 0; i < len(keys); i++ {
		if o := thismm._get(keys[i]); o == nil {
			return nil
		} else {
			if o.obj != nil {
				return o.obj
			} else if o.objMap != nil {
				thismm = o.objMap
			} else {
				// not supposed to happen,
				return nil
			}
		}
	}
	return nil
}

func (mm *memoryStoreMap) _putObj(key string, obj *objWithIndex, expectedRV uint64) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if o, f := mm.kvs[key]; f {
		if o.obj != nil {
			objRv, err := store.ObjectResourceVersion(o.obj.obj)
			if err != nil {
				return err
			}
			if objRv > expectedRV {
				return storage.NewTooLargeResourceVersionError(expectedRV, objRv, 0)
			}
		} else {
			// not supposed to happen, for syntax correctness
			return storage.NewInternalErrorf("tree node with final key %s is not nil, but has nil value", key)
		}
	}

	mm.kvs[key] = objMapOrObj{
		obj:    obj,
		objMap: nil,
	}
	return nil
}

func (mm *memoryStoreMap) _putObjMap(key string, objMap *memoryStoreMap) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.kvs[key] = objMapOrObj{
		obj:    nil,
		objMap: objMap,
	}
}

func (mm *memoryStoreMap) put(keys []string, obj *objWithIndex, expectedRV uint64) error {
	if len(keys) == 0 {
		return fmt.Errorf("Empty keys are provided %v", keys)
	}

	thismm := mm
	for i := 0; i < len(keys); i++ {
		if o := thismm._get(keys[i]); o == nil {
			if i == len(keys)-1 {
				thismm._putObj(keys[i], obj, expectedRV)
			} else {
				// add a map and move to next level
				newmm := &memoryStoreMap{mu: sync.RWMutex{}, kvs: map[string]objMapOrObj{}}
				thismm._putObjMap(keys[i], newmm)
				thismm = newmm
			}
		} else {
			if i == len(keys)-1 {
				if o.obj != nil {
					err := thismm._putObj(keys[i], obj, expectedRV)
					if err != nil {
						return err
					}
				} else {
					return fmt.Errorf("leaf node of keys %v is a map, it's supposed to be nil or obj", keys)
				}
			} else {
				if o.objMap == nil {
					return fmt.Errorf("%dth middle node of keys %v is not a map", i, keys)
				} else {
					// continue to search next level
					thismm = o.objMap
				}
			}
		}
	}
	return nil
}

func (mm *memoryStoreMap) _del(key string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	delete(mm.kvs, key)
}

func (mm *memoryStoreMap) del(keys []string) error {
	if len(keys) == 0 {
		return fmt.Errorf("Empty keys are provided %v", keys)
	}

	thismm := mm
	for i := 0; i < len(keys); i++ {
		if o := thismm._get(keys[i]); o == nil {
			return nil
		} else {
			if i == len(keys)-1 {
				if o.obj != nil {
					thismm._del(keys[i])
				} else {
					return fmt.Errorf("leaf node of keys %v is a map, it's supposed to be nil or obj", keys)
				}
			} else {
				if o.objMap == nil {
					return fmt.Errorf("%dth middle node of keys %v is not a map", i, keys)
				} else {
					// continue to search next level
					thismm = o.objMap
				}
			}
		}
	}
	return nil
}
