package namespacemanager

import corev1 "k8s.io/api/core/v1"

type Namespace = corev1.Namespace

type NamespaceManager struct {
	tenants map[string]Namespace
}

func New() *NamespaceManager {
	return &NamespaceManager{
		tenants: make(map[string]Namespace),
	}
}
