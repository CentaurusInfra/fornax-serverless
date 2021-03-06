/*
Copyright 2022 The fornax-serverless Authors.

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
// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	v1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ApplicationSessionLister helps list ApplicationSessions.
// All objects returned here must be treated as read-only.
type ApplicationSessionLister interface {
	// List lists all ApplicationSessions in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1.ApplicationSession, err error)
	// ApplicationSessions returns an object that can list and get ApplicationSessions.
	ApplicationSessions(namespace string) ApplicationSessionNamespaceLister
	ApplicationSessionListerExpansion
}

// applicationSessionLister implements the ApplicationSessionLister interface.
type applicationSessionLister struct {
	indexer cache.Indexer
}

// NewApplicationSessionLister returns a new ApplicationSessionLister.
func NewApplicationSessionLister(indexer cache.Indexer) ApplicationSessionLister {
	return &applicationSessionLister{indexer: indexer}
}

// List lists all ApplicationSessions in the indexer.
func (s *applicationSessionLister) List(selector labels.Selector) (ret []*v1.ApplicationSession, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.ApplicationSession))
	})
	return ret, err
}

// ApplicationSessions returns an object that can list and get ApplicationSessions.
func (s *applicationSessionLister) ApplicationSessions(namespace string) ApplicationSessionNamespaceLister {
	return applicationSessionNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ApplicationSessionNamespaceLister helps list and get ApplicationSessions.
// All objects returned here must be treated as read-only.
type ApplicationSessionNamespaceLister interface {
	// List lists all ApplicationSessions in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1.ApplicationSession, err error)
	// Get retrieves the ApplicationSession from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1.ApplicationSession, error)
	ApplicationSessionNamespaceListerExpansion
}

// applicationSessionNamespaceLister implements the ApplicationSessionNamespaceLister
// interface.
type applicationSessionNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all ApplicationSessions in the indexer for a given namespace.
func (s applicationSessionNamespaceLister) List(selector labels.Selector) (ret []*v1.ApplicationSession, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.ApplicationSession))
	})
	return ret, err
}

// Get retrieves the ApplicationSession from the indexer for a given namespace and name.
func (s applicationSessionNamespaceLister) Get(name string) (*v1.ApplicationSession, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("applicationsession"), name)
	}
	return obj.(*v1.ApplicationSession), nil
}
