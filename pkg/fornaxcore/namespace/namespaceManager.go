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

package namespace

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"sync"
)

type Namespace = corev1.Namespace

type NamespaceManager struct {
	namespaces map[string]Namespace
	sync.RWMutex
}

func New() *NamespaceManager {
	return &NamespaceManager{
		namespaces: make(map[string]Namespace),
	}
}

func (nm *NamespaceManager) GetNamespace(name string) *Namespace {
	nm.RLock()
	defer nm.RUnlock()
	ns, ok := nm.namespaces[name]
	if !ok {
		return nil
	}
	return &ns
}

func (nm *NamespaceManager) CreateNamespace(ns *Namespace) error {
	if len(ns.Name) == 0 {
		return fmt.Errorf("name should not be empty")
	}

	nm.Lock()
	defer nm.Unlock()

	_, ok := nm.namespaces[ns.Name]
	if ok {
		return fmt.Errorf("named resource already exists")
	}

	nm.namespaces[ns.Name] = *ns
	return nil
}

// todo: ns-manager to maintain ns data by list/watch ns from etcd
