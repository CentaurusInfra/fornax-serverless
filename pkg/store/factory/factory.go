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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/generic"
	apistorage "k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"k8s.io/apiserver/pkg/storage/storagebackend/factory"
	"k8s.io/client-go/tools/cache"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxstore "centaurusinfra.io/fornax-serverless/pkg/store"
	"centaurusinfra.io/fornax-serverless/pkg/store/backendstorage/inmemory"
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

func NewFornaxStorage(groupResource schema.GroupResource, pagingEnabled bool) *inmemory.MemoryStore {
	return inmemory.NewMemoryStore(groupResource, pagingEnabled)
}

// Creates a cacher based given storageConfig.
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

	storage := inmemory.NewMemoryStore(storageConfig.GroupResource, storageConfig.Paging)

	destroyFunc := func() {
		d()
		storage.Stop()
	}

	return storage, destroyFunc, nil
}

func GetApplicationSessionCache(store fornaxstore.FornaxStorage, sessionLabel string) (*fornaxv1.ApplicationSession, error) {
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

func GetApplicationCache(store fornaxstore.FornaxStorage, applicationLabel string) (*fornaxv1.Application, error) {
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
