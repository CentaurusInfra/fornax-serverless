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

package factory

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/generic"
	apistorage "k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"k8s.io/apiserver/pkg/storage/storagebackend/factory"
	"k8s.io/client-go/tools/cache"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxstore "centaurusinfra.io/fornax-serverless/pkg/store"
	"centaurusinfra.io/fornax-serverless/pkg/store/composite"
	"centaurusinfra.io/fornax-serverless/pkg/store/inmemory"
	"centaurusinfra.io/fornax-serverless/pkg/store/storage"
	"centaurusinfra.io/fornax-serverless/pkg/util"
)

var (
	_FornaxInMemoryStoresMutex  = &sync.RWMutex{}
	_InMemoryResourceStores     = map[string]*inmemory.MemoryStore{}
	_FornaxCompositeStoresMutex = &sync.RWMutex{}
	_CompositedResourceStores   = map[string]*composite.CompositeStore{}
)

type FornaxRestOptionsFactory struct {
	OptionsGetter generic.RESTOptionsGetter
}

func (f *FornaxRestOptionsFactory) GetRESTOptions(resource schema.GroupResource) (generic.RESTOptions, error) {
	options, err := f.OptionsGetter.GetRESTOptions(resource)
	if resource == fornaxv1.ApplicationGrv.GroupResource() {
		options.Decorator = CompositedFornaxApplicationStorageFunc
	} else if resource == fornaxv1.ApplicationSessionGrv.GroupResource() {
		options.Decorator = FornaxApplicationSessionStorageFunc
	} else {
		return options, fmt.Errorf("unknown resource %v", resource)
	}
	if err != nil {
		return options, err
	}
	return options, nil
}

var (
	RegisteredBackEndStorage = map[string]storage.Store{}
)

// use json to store node agent store object for now, consider using protobuf if meet performance issue
func JsonToApplication(text []byte) (interface{}, error) {
	res := &fornaxv1.Application{}
	if err := json.Unmarshal([]byte(text), res); err != nil {
		return nil, err
	}
	return res, nil
}

func JsonFromApplication(obj interface{}) ([]byte, error) {
	if _, ok := obj.(*fornaxv1.Application); !ok {
		return nil, storage.InvalidObjectType
	}

	var bytes []byte
	var err error
	if bytes, err = json.Marshal(obj); err != nil {
		return nil, err
	}
	return bytes, nil
}

func NewFornaxApplicationStatusStorage(ctx context.Context) *inmemory.MemoryStore {
	return newFornaxStorage(ctx, fornaxv1.ApplicationGrv.GroupResource(), fornaxv1.ApplicationGrvKey, nil, nil)
}

func NewFornaxApplicationSessionStorage(ctx context.Context) *inmemory.MemoryStore {
	return newFornaxStorage(ctx, fornaxv1.ApplicationSessionGrv.GroupResource(), fornaxv1.ApplicationSessionGrvKey, nil, nil)
}

func newFornaxStorage(ctx context.Context, groupResource schema.GroupResource, grvKey string, newFunc func() runtime.Object, newListFunc func() runtime.Object) *inmemory.MemoryStore {
	_FornaxInMemoryStoresMutex.Lock()
	defer _FornaxInMemoryStoresMutex.Unlock()
	key := groupResource.String()
	if si, found := _InMemoryResourceStores[key]; found {
		return si
	} else {
		si = inmemory.NewMemoryStore(ctx, groupResource, grvKey, newFunc, newListFunc)
		_InMemoryResourceStores[key] = si
		return si
	}
}

// this function is provided to k8s api server to get resource storage.Interface
func CompositedFornaxApplicationStorageFunc(
	storageConfig *storagebackend.ConfigForResource,
	resourcePrefix string,
	keyFunc func(obj runtime.Object) (string, error),
	newFunc func() runtime.Object,
	newListFunc func() runtime.Object,
	getAttrsFunc apistorage.AttrFunc,
	triggerFuncs apistorage.IndexerFuncs,
	indexers *cache.Indexers) (apistorage.Interface, factory.DestroyFunc, error) {

	key := storageConfig.GroupResource.String()
	_FornaxCompositeStoresMutex.Lock()
	defer _FornaxCompositeStoresMutex.Unlock()
	if s, f := _CompositedResourceStores[key]; f {
		return s, s.DestroyFunc, nil
	}

	specStore, persistStoreDestroyFunc, err := factory.Create(*storageConfig, newFunc)
	if err != nil {
		return specStore, persistStoreDestroyFunc, err
	}
	statusStore := NewFornaxApplicationStatusStorage(context.Background())
	statusStore.CompleteWithFunctions(keyFunc, newFunc, newListFunc, getAttrsFunc, triggerFuncs, indexers)
	destroyFunc := func() {
		persistStoreDestroyFunc()
		statusStore.Stop()
	}
	cStore := composite.NewCompositeStore(storageConfig.GroupResource, specStore, statusStore, applicationStatusAndRevisionMerge, newFunc, newListFunc, keyFunc, destroyFunc)
	cStore.Run(context.Background())
	_CompositedResourceStores[key] = cStore
	return cStore, destroyFunc, nil
}

func applicationKeyFunc(o runtime.Object) (string, error) {
	if obj, ok := o.(*fornaxv1.Application); ok {
		return fmt.Sprintf("%s/%s", fornaxv1.ApplicationGrvKey, util.Name(obj)), nil
	} else {
		return "", fmt.Errorf("not a valid fornax Application")
	}
}

func applicationStatusAndRevisionMerge(from runtime.Object, to runtime.Object) error {
	fromApp, ok := from.(*fornaxv1.Application)
	if !ok {
		return fmt.Errorf("from object is not a valid fornax Application runtime object")
	}
	toApp, ok := to.(*fornaxv1.Application)
	if !ok {
		return fmt.Errorf("from object is not a valid fornax Application runtime object")
	}

	status := fromApp.Status.DeepCopy()
	toApp.Status = *status
	if status.TotalInstances == 0 {
		util.RemoveFinalizer(&toApp.ObjectMeta, fornaxv1.FinalizerApplicationPod)
	} else {
		util.AddFinalizer(&toApp.ObjectMeta, fornaxv1.FinalizerApplicationPod)
	}
	toApp.ResourceVersion = fromApp.ResourceVersion

	return nil
}

// this function is provided to k8s api server to get resource storage.Interface
func FornaxApplicationSessionStorageFunc(
	storageConfig *storagebackend.ConfigForResource,
	resourcePrefix string,
	keyFunc func(obj runtime.Object) (string, error),
	newFunc func() runtime.Object,
	newListFunc func() runtime.Object,
	getAttrsFunc apistorage.AttrFunc,
	triggerFuncs apistorage.IndexerFuncs,
	indexers *cache.Indexers) (apistorage.Interface, factory.DestroyFunc, error) {

	var storage *inmemory.MemoryStore
	_FornaxInMemoryStoresMutex.Lock()
	key := storageConfig.GroupResource.String()
	defer _FornaxInMemoryStoresMutex.Unlock()
	if b, f := _InMemoryResourceStores[key]; !f {
		return nil, nil, fmt.Errorf("Can not find a regisgered store for %s", key)
	} else {
		storage = b
	}

	storage.CompleteWithFunctions(keyFunc, newFunc, newListFunc, getAttrsFunc, triggerFuncs, indexers)
	destroyFunc := func() {
		storage.Stop()
	}

	return storage, destroyFunc, nil
}

func GetApplicationSessionCache(store fornaxstore.ApiStorageInterface, sessionLabel string) (*fornaxv1.ApplicationSession, error) {
	out := &fornaxv1.ApplicationSession{}
	key := fmt.Sprintf("%s/%s", fornaxv1.ApplicationSessionGrvKey, sessionLabel)
	err := store.Get(context.Background(), key, apistorage.GetOptions{IgnoreNotFound: false}, out)
	if err != nil {
		if fornaxstore.IsObjectNotFoundErr(err) {
			return nil, nil
		}
	}
	return out, nil
}

func GetApplicationCache(store fornaxstore.ApiStorageInterface, applicationLabel string) (*fornaxv1.Application, error) {
	out := &fornaxv1.Application{}
	key := fmt.Sprintf("%s/%s", fornaxv1.ApplicationGrvKey, applicationLabel)
	err := store.Get(context.Background(), key, apistorage.GetOptions{IgnoreNotFound: false}, out)
	if err != nil {
		if fornaxstore.IsObjectNotFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

func CreateApplicationSession(ctx context.Context, store fornaxstore.ApiStorageInterface, session *fornaxv1.ApplicationSession) (*fornaxv1.ApplicationSession, error) {
	out := &fornaxv1.ApplicationSession{}
	key := fmt.Sprintf("%s/%s", fornaxv1.ApplicationSessionGrvKey, util.Name(session))
	err := store.Create(ctx, key, session, out, uint64(0))
	if err != nil {
		return nil, err
	}
	return out, nil
}

func CreateApplication(ctx context.Context, store fornaxstore.ApiStorageInterface, application *fornaxv1.Application) (*fornaxv1.Application, error) {
	out := &fornaxv1.Application{}
	key := fmt.Sprintf("%s/%s", fornaxv1.ApplicationGrvKey, util.Name(application))
	err := store.Create(ctx, key, application, out, uint64(0))
	if err != nil {
		return nil, err
	}
	return out, nil
}

func UpdateApplicationSession(ctx context.Context, store fornaxstore.ApiStorageInterface, session *fornaxv1.ApplicationSession) (*fornaxv1.ApplicationSession, error) {
	out := &fornaxv1.ApplicationSession{}
	key := fmt.Sprintf("%s/%s", fornaxv1.ApplicationSessionGrvKey, util.Name(session))
	err := store.EnsureUpdateAndDelete(ctx, key, true, nil, session, out)
	if err != nil {
		return nil, err
	}

	return out, nil
}
