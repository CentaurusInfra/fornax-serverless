package namespacemanager

import corev1 "k8s.io/api/core/v1"

type Namespace = corev1.Namespace

type NamespaceManager struct {
	namespaces map[string]Namespace
}

func New() *NamespaceManager {
	return &NamespaceManager{
		namespaces: make(map[string]Namespace),
	}
}

// todo: ns-manager to maintain ns data by list/watch ns from etcd
