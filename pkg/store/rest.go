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

// New returns a new etcd backed request handler for the resource.

package store

import (
	"context"

	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource"
	brest "sigs.k8s.io/apiserver-runtime/pkg/builder/rest"
)

// New returns a new etcd backed request handler for the resource.
func FornaxReadonlyResourceHandler(obj resource.Object) brest.ResourceHandlerProvider {
	return func(scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter) (rest.Storage, error) {
		gvr := obj.GetGroupVersionResource()
		s := &brest.DefaultStrategy{
			Object:         obj,
			ObjectTyper:    scheme,
			TableConvertor: rest.NewDefaultTableConvertor(gvr.GroupResource()),
		}
		return newReadonlyStore(scheme, obj.New, obj.NewList, gvr, s, optsGetter, nil)
	}
}

// newReadonlyStore returns a RESTStorage object that will work against API services.
func newReadonlyStore(
	scheme *runtime.Scheme,
	single, list func() runtime.Object,
	gvr schema.GroupVersionResource,
	s brest.Strategy, optsGetter generic.RESTOptionsGetter, fn brest.StoreFn) (rest.Storage, error) {
	store := &genericregistry.Store{
		NewFunc:                  single,
		NewListFunc:              list,
		PredicateFunc:            s.Match,
		DefaultQualifiedResource: gvr.GroupResource(),
		TableConvertor:           s,
		CreateStrategy:           s,
		UpdateStrategy:           s,
		DeleteStrategy:           s,
		StorageVersioner:         gvr.GroupVersion(),
	}

	options := &generic.StoreOptions{RESTOptions: optsGetter, AttrFunc: brest.GetAttrs}
	if fn != nil {
		fn(scheme, store, options)
	}
	if err := store.CompleteWithOptions(options); err != nil {
		return nil, err
	}
	fstore := &fornaxReadOnlyStore{
		backendStore: store,
	}
	return fstore, nil
}

var _ readOnlyStore = &fornaxReadOnlyStore{}

type readOnlyStore interface {
	rest.Storage
	rest.Lister
	rest.Getter
	rest.Watcher
	rest.TableConvertor
	rest.Scoper
}

type fornaxReadOnlyStore struct {
	backendStore *genericregistry.Store
}

// NamespaceScoped implements ReadOnlyStore
func (r *fornaxReadOnlyStore) NamespaceScoped() bool {
	return r.backendStore.NamespaceScoped()
}

// ConvertToTable implements ReadOnlyStore
func (r *fornaxReadOnlyStore) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return r.backendStore.ConvertToTable(ctx, object, tableOptions)
}

// List implements ReadOnlyStore
func (r *fornaxReadOnlyStore) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	return r.backendStore.List(ctx, options)
}

// NewList implements ReadOnlyStore
func (r *fornaxReadOnlyStore) NewList() runtime.Object {
	return r.backendStore.NewList()
}

// Get implements ReadOnlyStore
func (r *fornaxReadOnlyStore) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return r.backendStore.Get(ctx, name, options)
}

// Watch implements ReadOnlyStore
func (r *fornaxReadOnlyStore) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	return r.backendStore.Watch(ctx, options)
}

// New implements rest.Storage
func (r *fornaxReadOnlyStore) New() runtime.Object {
	return r.backendStore.New()
}
