package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FornaxNode struct {
	corev1.Node
}

func (in *FornaxNode) NamespaceScoped() bool {
	return false
}

func (in *FornaxNode) New() runtime.Object {
	return &corev1.Node{}
}

func (in *FornaxNode) NewList() runtime.Object {
	return &corev1.NodeList{}
}

func (in *FornaxNode) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "k8s.io",
		Version:  "v1",
		Resource: "nodes",
	}
}

func (in *FornaxNode) IsStorageVersion() bool {
	return true
}

func (in *FornaxNode) GetObjectMeta() *metav1.ObjectMeta {
	return &(in.ObjectMeta)
}
