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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/storage"
)

type objEvent struct {
	key       string
	obj       runtime.Object
	oldObj    runtime.Object
	rev       uint64
	isDeleted bool
	isCreated bool
}

type objWithIndex struct {
	key     string
	obj     runtime.Object
	index   uint64
	deleted bool
}

type objList struct {
	objs         []*objWithIndex
	lastObjIndex uint64
}

// shrink this list to specified length, by removing nil obj or obj is marked as deleted
func (list *objList) Len() int {
	return (len(list.objs))
}

// shrink this list to specified length, by removing nil obj or obj is marked as deleted
func (list *objList) shrink(length uint64) {
	diff := uint64(list.Len()) - length
	if diff <= 0 {
		return
	}
	newList := make([]*objWithIndex, length)
	i := uint64(0)
	lastIndex := uint64(0)
	for _, v := range list.objs {
		if (v == nil || v.deleted) && diff > 0 {
			// skip one nil or deleted object, reduce 1 from diff
			diff -= 1
			continue
		} else {
			if i < length {
				newList[i] = v
			} else {
				// append if array is not long enough
				newList = append(newList, v)
			}
			if v != nil {
				v.index = i
				lastIndex = i
			}
			i += 1
		}
	}
	list.objs = newList
	list.lastObjIndex = lastIndex
}

func (list *objList) grow(length uint64) {
	newList := make([]*objWithIndex, uint64(list.Len())+length)
	copy(newList, list.objs)
	list.objs = newList
}

type objMapOrObj struct {
	obj    *objWithIndex
	objMap *objStoreMap
}

type objStoreMap struct {
	mu  sync.RWMutex
	kvs map[string]objMapOrObj
}

func (mm *objStoreMap) count(keys []string) (int64, error) {
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

func (mm *objStoreMap) _countItemsInMap() int64 {
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

func (mm *objStoreMap) _get(key string) *objMapOrObj {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	if o, f := mm.kvs[key]; f {
		return &o
	}
	return nil
}

func (mm *objStoreMap) get(keys []string) *objWithIndex {
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

func (mm *objStoreMap) _putObj(key string, obj *objWithIndex, expectedRV uint64) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if o, f := mm.kvs[key]; f {
		if o.obj != nil {
			objRv, err := store.GetObjectResourceVersion(o.obj.obj)
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

func (mm *objStoreMap) _putObjMap(key string, objMap *objStoreMap) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.kvs[key] = objMapOrObj{
		obj:    nil,
		objMap: objMap,
	}
}

func (mm *objStoreMap) put(keys []string, obj *objWithIndex, expectedRV uint64) error {
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
				newmm := &objStoreMap{mu: sync.RWMutex{}, kvs: map[string]objMapOrObj{}}
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

func (mm *objStoreMap) _del(key string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	delete(mm.kvs, key)
}

func (mm *objStoreMap) del(keys []string) error {
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
