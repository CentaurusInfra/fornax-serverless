package namespacemanager

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
