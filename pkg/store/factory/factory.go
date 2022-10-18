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
	"centaurusinfra.io/fornax-serverless/pkg/store/inmemory"
	"centaurusinfra.io/fornax-serverless/pkg/store/storage"
	"centaurusinfra.io/fornax-serverless/pkg/store/storage/etcd"
	"centaurusinfra.io/fornax-serverless/pkg/util"
)

var (
	_InMemoryStoresMutex = &sync.RWMutex{}
	_InMemoryStores      = map[string]*inmemory.MemoryStore{}
)

type FornaxRestOptionsFactory struct {
	OptionsGetter generic.RESTOptionsGetter
}

func (f *FornaxRestOptionsFactory) GetRESTOptions(resource schema.GroupResource) (generic.RESTOptions, error) {
	options, err := f.OptionsGetter.GetRESTOptions(resource)
	options.Decorator = FornaxStorageFunc
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

func NewApplicationEtcdStore(ctx context.Context, etcdEndPoints []string) (storage.StoreWithRevision, error) {
	backend, err := etcd.NewEtcdStore(ctx, etcdEndPoints, fornaxv1.ApplicationGrvKey, JsonToApplication, JsonFromApplication)
	if err != nil {
		return nil, err
	}
	return backend, nil
}

func NewFornaxApplicationStorage(backend storage.StoreWithRevision) *inmemory.MemoryStore {
	a := &fornaxv1.Application{}
	return NewFornaxStorage(fornaxv1.ApplicationGrv.GroupResource(), fornaxv1.ApplicationGrvKey, a.New, a.NewList, backend)
}

func NewFornaxApplicationSessionStorage() *inmemory.MemoryStore {
	return NewFornaxStorage(fornaxv1.ApplicationSessionGrv.GroupResource(), fornaxv1.ApplicationSessionGrvKey, nil, nil, nil)
}

func NewFornaxStorage(groupResource schema.GroupResource, grvKey string, newFunc func() runtime.Object, newListFunc func() runtime.Object, backendStore storage.StoreWithRevision) *inmemory.MemoryStore {
	_InMemoryStoresMutex.Lock()
	defer _InMemoryStoresMutex.Unlock()
	key := groupResource.String()
	if si, found := _InMemoryStores[key]; found {
		return si
	} else {
		si = inmemory.NewMemoryStore(groupResource, grvKey, newFunc, newListFunc, backendStore)
		_InMemoryStores[key] = si
		return si
	}
}

// this function is provided to k8s api server to get resource storage.Interface
func FornaxStorageFunc(
	storageConfig *storagebackend.ConfigForResource,
	resourcePrefix string,
	keyFunc func(obj runtime.Object) (string, error),
	newFunc func() runtime.Object,
	newListFunc func() runtime.Object,
	getAttrsFunc apistorage.AttrFunc,
	triggerFuncs apistorage.IndexerFuncs,
	indexers *cache.Indexers) (apistorage.Interface, factory.DestroyFunc, error) {

	s, d, err := factory.Create(*storageConfig, newFunc)
	if err != nil {
		return s, d, err
	}

	var storage *inmemory.MemoryStore
	_InMemoryStoresMutex.Lock()
	key := storageConfig.GroupResource.String()
	defer _InMemoryStoresMutex.Unlock()
	if b, f := _InMemoryStores[key]; !f {
		return nil, nil, fmt.Errorf("Can not find a regisgered store for %s", key)
	} else {
		storage = b
	}

	storage.CompleteWithFunctions(keyFunc, newFunc, newListFunc, getAttrsFunc, triggerFuncs, indexers)
	destroyFunc := func() {
		d()
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
