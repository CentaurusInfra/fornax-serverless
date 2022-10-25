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

package composite

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"time"

	"k8s.io/klog/v2"

	"centaurusinfra.io/fornax-serverless/pkg/store"
	"centaurusinfra.io/fornax-serverless/pkg/store/inmemory"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	apistorage "k8s.io/apiserver/pkg/storage"
)

// CompositeStore save a Resource into two storage.Interface, one is persist store using etcd, it save Resource Spec,
// another is memory store, it's used to save Resource Status which can be calcluated onfly using received data from node agent in memory.
// eg. how many running instances of a application,

// when client call api on a resoruce, spec will write into persist store, it does not lose when fornax api server restarted,
// at same time, memory store will be updated to keep a latest copy of spec, meantime, when resource controller/managers received resource implementation status from nodeaget, e.g. Pod or Session, it calculate resource satus and write resource status into memory store,
// as status can be recalculated, thise information will be eventually consistent when api server restarted

// when api client call Delete/Update/Create/Get/List rest api on resources, it read/modify persist store firstly for spec change, then call memory store to get or create resource status, and merge spec and status into returned runtime object
// for mutation rest api, it als call memory store storage api to replicate spec change.

// persist store is using etcd, so resource revision in persist store is using etcd revision, also same resource in memory has its own revision number which are managed by memory store using a gloabl atomic memory revision number whenever spec changed or status changed.
// these two revisions are totolly different, as spec change is much less and status change are majority, so, we use revision number in memory store in returned object for each api call, so it represent memory status change,

// then when api client call watch api to watch resources, we need to watch memory store not persist store, if client changed spec, spec are replicated into memory store also, so, client can still get spec change, not only status change.

type CompositeStore struct {
	mu                sync.Mutex
	groupResourceKey  string
	specPersistStore  storage.Interface
	statusMemoryStore *inmemory.MemoryStore
	mergeFunc         func(from runtime.Object, to runtime.Object) error
	keyFunc           func(obj runtime.Object) (string, error)
	newFunc           func() runtime.Object
	newListFunc       func() runtime.Object
	DestroyFunc       func()
}

// Count implements storage.Interface
func (c *CompositeStore) Count(key string) (int64, error) {
	return c.specPersistStore.Count(key)
}

// Create implements storage.Interface
func (c *CompositeStore) Create(ctx context.Context, key string, obj runtime.Object, out runtime.Object, ttl uint64) error {
	specOut := out.DeepCopyObject()
	statusOut := out.DeepCopyObject()
	// create persist copy firstly,
	err := c.specPersistStore.Create(ctx, key, obj, specOut, ttl)
	if err != nil {
		return err
	}

	// status copy must be recreated if a spec is created
	newSpecObj := specOut.DeepCopyObject()
	err = c.statusMemoryStore.CreateOrReplace(ctx, key, newSpecObj, statusOut)
	if err == nil {
		// set output with status
		outVal, _ := conversion.EnforcePtr(out)
		outVal.Set(reflect.ValueOf(statusOut).Elem())
		return nil
	} else {
		return apistorage.NewInternalError("unnown creation error")
	}
}

// Delete implements storage.Interface
func (c *CompositeStore) Delete(ctx context.Context, key string, out runtime.Object, preconditions *storage.Preconditions, validateDeletion storage.ValidateObjectFunc, cachedExistingObject runtime.Object) error {
	outVal, _ := conversion.EnforcePtr(out)
	specOut := out.DeepCopyObject()
	statusOut := out.DeepCopyObject()
	// memory status deletion successful, delete persist part
	err := c.specPersistStore.Delete(ctx, key, specOut, preconditions, validateDeletion, cachedExistingObject)
	if err != nil {
		return err
	}

	err = c.statusMemoryStore.Delete(ctx, key, statusOut, nil, nil, nil)
	if err != nil && !apistorage.IsNotFound(err) {
		return err
	}
	outVal.Set(reflect.ValueOf(statusOut).Elem())
	return err
}

// Get implements storage.Interface
// get spec from specPersistStore, and get status from statusMemoryStore, and merge them, if no spec, return error,
// if no status, create one,
func (c *CompositeStore) Get(ctx context.Context, key string, opts storage.GetOptions, out runtime.Object) error {
	outVal, _ := conversion.EnforcePtr(out)
	specOut := out.DeepCopyObject()
	statusOut := out.DeepCopyObject()

	err := c.specPersistStore.Get(ctx, key, opts, specOut)
	if err != nil {
		return err
	}

	if store.HasDeletionTimestamp(specOut) {
		err = c.statusMemoryStore.Get(ctx, key, storage.GetOptions{IgnoreNotFound: true}, statusOut)
		if err != nil {
			return err
		}
	} else {
		err = c.statusMemoryStore.GetOrCreate(ctx, key, specOut, statusOut)
		if err != nil {
			return err
		}
	}
	c.mergeFunc(statusOut, specOut)
	outVal.Set(reflect.ValueOf(specOut).Elem())
	return err
}

// GetList implements storage.Interface
func (c *CompositeStore) GetList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	err := c.specPersistStore.GetList(ctx, key, opts, listObj)
	if err != nil {
		return err
	}
	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return err
	}
	listOutVal, err := conversion.EnforcePtr(listPtr)
	for i := 0; i < listOutVal.Len(); i++ {
		objVal := listOutVal.Index(i)
		if specObj, ok := objVal.Addr().Interface().(runtime.Object); ok {
			oKey, err := c.keyFunc(specObj)
			if err != nil {
				continue
			}
			statusOut := c.newFunc()

			if store.HasDeletionTimestamp(specObj) {
				err = c.statusMemoryStore.Get(ctx, oKey, storage.GetOptions{IgnoreNotFound: true}, statusOut)
				if err != nil {
					continue
				}
			} else {
				err = c.statusMemoryStore.GetOrCreate(ctx, oKey, specObj, statusOut)
				if err != nil {
					continue
				}
			}
			c.mergeFunc(statusOut, specObj)
		}
	}

	return nil
}

// GuaranteedUpdate implements storage.Interface
func (c *CompositeStore) GuaranteedUpdate(ctx context.Context, key string, out runtime.Object, ignoreNotFound bool, preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc, cachedExistingObject runtime.Object) error {
	specOut := out.DeepCopyObject()

	// ugly because we have to update etcd object's finalizer to make sure does not delete object with empty finalizer in persit store's GuaranteedUpdate method
	c.ensureFinalizerUpdated(ctx, key)
	err := c.specPersistStore.GuaranteedUpdate(ctx, key, specOut, ignoreNotFound, preconditions, tryUpdate, cachedExistingObject)
	if err != nil {
		klog.ErrorS(err, "Failed to update object spec", "key", key)
		return err
	}

	statusOut := out.DeepCopyObject()
	// composite store GuaranteedUpdate does not update status directly via api, it only get existing one or create
	// status is updated using statusMemoryStore by internal components, we just get status from memrory and merge status into out
	err = c.statusMemoryStore.CreateOrUpdate(ctx, key, specOut, statusOut, c.mergeFunc)
	if err != nil {
		klog.ErrorS(err, "Failed to update memory status", "key", key)
		return err
	}
	outVal, _ := conversion.EnforcePtr(out)
	outVal.Set(reflect.ValueOf(statusOut).Elem())

	return nil
}

// update spec store's finalizers, so it won't be deleted until status store watch routine reset finalizers
func (c *CompositeStore) ensureFinalizerUpdated(ctx context.Context, key string) error {
	statusOut := c.newFunc()
	err := c.statusMemoryStore.Get(ctx, key, apistorage.GetOptions{IgnoreNotFound: true}, statusOut)
	if err != nil {
		return err
	}
	objMeta, err := meta.Accessor(statusOut)
	if err != nil {
		return err
	}

	specOut := c.newFunc()
	err = c.specPersistStore.Get(ctx, key, apistorage.GetOptions{IgnoreNotFound: true}, specOut)
	if err != nil {
		return err
	}
	specMeta, err := meta.Accessor(specOut)
	if err != nil {
		return err
	}

	// finalizers are computed according memory status
	if len(objMeta.GetFinalizers()) != len(specMeta.GetFinalizers()) {
		specMeta.SetFinalizers(objMeta.GetFinalizers())
	}

	return c.specPersistStore.GuaranteedUpdate(ctx, key, specOut, true, nil, store.GetTryUpdateFunc(specOut), nil)
}

// Versioner implements storage.Interface
func (c *CompositeStore) Versioner() storage.Versioner {
	return c.specPersistStore.Versioner()
}

// Watch implements storage.Interface
func (c *CompositeStore) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	mwci, err := c.statusMemoryStore.Watch(ctx, key, opts)
	if err != nil {
		mwci.Stop()
		return nil, err
	}
	return mwci, err
}

// NewMemoryStore return a singleton storage.Interface for a groupResource
func NewCompositeStore(
	groupResource schema.GroupResource,
	persistStore storage.Interface,
	memoryStore *inmemory.MemoryStore,
	mergeFunc func(from runtime.Object, to runtime.Object) error,
	newFunc func() runtime.Object,
	newListFunc func() runtime.Object,
	keyFunc func(obj runtime.Object) (string, error),
	destroyFunc func(),
) *CompositeStore {
	klog.InfoS("New or Get a composite store for", "resource", groupResource)
	si := &CompositeStore{
		specPersistStore:  persistStore,
		statusMemoryStore: memoryStore,
		mergeFunc:         mergeFunc,
		keyFunc:           keyFunc,
		newFunc:           newFunc,
		newListFunc:       newListFunc,
		DestroyFunc:       destroyFunc,
	}
	return si
}

func (c *CompositeStore) Run(ctx context.Context) {
	// watch etcd and load all existing specs, it also watch spec change happened outside this storage
	go func() {
		rev := uint64(0)
		for {
			err := func() error {
				mwci, err := c.specPersistStore.Watch(ctx, c.groupResourceKey, apistorage.ListOptions{
					ResourceVersion:      strconv.Itoa(int(rev)),
					ResourceVersionMatch: "",
					Predicate:            apistorage.Everything,
					Recursive:            true,
					ProgressNotify:       true,
				})
				if err != nil {
					return err
				}
				mwch := mwci.ResultChan()
				defer mwci.Stop()

				for {
					select {
					case mwche := <-mwch:
						if mwche.Type == watch.Bookmark {
							obj := mwche.Object
							rev, err = store.GetObjectResourceVersion(obj)
							return errors.New("rewatch")
						} else if mwche.Type == watch.Error {
							return errors.New("rewatch")
						} else {
							obj := mwche.Object.DeepCopyObject()
							orev, err := store.GetObjectResourceVersion(obj)
							if err != nil {
								continue
							} else {
								rev = orev
							}
							key, err := c.keyFunc(obj)
							if err != nil {
								continue
							}
							statusOut := c.newFunc()
							if mwche.Type == watch.Deleted {
								c.statusMemoryStore.Delete(ctx, key, statusOut, nil, nil, nil)
							} else if mwche.Type == watch.Added || mwche.Type == watch.Modified {
								c.statusMemoryStore.CreateOrUpdate(ctx, key, obj, statusOut, c.mergeFunc)
							} else {
								//no op
							}
						}
					}
				}
			}()
			if err != nil {
				time.Sleep(1 * time.Second)
				continue
			}
		}
	}()

	// when a resource is being deleted, it's updated with deletion timestamp if it has finalizers, not physically deleted
	// resource finalizers are calculated according status change in memory, watch memory store event to check if resource finalizers are empty and has deletion timestamp
	// and deleted its spec obj
	go func() {
		rev := uint64(0)
		for {
			err := func() error {
				mwci, err := c.statusMemoryStore.Watch(ctx, c.groupResourceKey, apistorage.ListOptions{
					ResourceVersion:      strconv.Itoa(int(rev)),
					ResourceVersionMatch: "",
					Predicate:            apistorage.Everything,
					Recursive:            true,
					ProgressNotify:       true,
				})
				if err != nil {
					return err
				}
				mwch := mwci.ResultChan()
				defer mwci.Stop()

				for {
					select {
					case mwche := <-mwch:
						if mwche.Type == watch.Bookmark {
							return errors.New("rewatch")
						} else if mwche.Type == watch.Error {
							return errors.New("rewatch")
						} else {
							statusObj := mwche.Object.DeepCopyObject()
							orev, err := store.GetObjectResourceVersion(statusObj)
							if err != nil {
								continue
							} else {
								rev = orev
							}
							key, err := c.keyFunc(statusObj)
							if err != nil {
								continue
							}
							specOut := c.newFunc()
							if mwche.Type == watch.Deleted {
								if store.ShouldDeleteSpec(statusObj) {
									// obj has deletion timestamp and finalizers is empty,
									err = c.specPersistStore.Get(ctx, key, apistorage.GetOptions{IgnoreNotFound: false}, specOut)
									if err == nil && store.HasDeletionTimestamp(specOut) {
										c.specPersistStore.Delete(ctx, key, specOut, nil, apistorage.ValidateAllObjectFunc, nil)
									}
								}
							}
						}
					}
				}
			}()
			if err != nil {
				time.Sleep(1 * time.Second)
				continue
			}
		}
	}()
}

var _ apistorage.Interface = &CompositeStore{}
