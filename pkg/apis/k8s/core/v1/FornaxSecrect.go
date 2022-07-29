package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FornaxSecret struct {
	corev1.Secret
}

func (in *FornaxSecret) NamespaceScoped() bool {
	return true
}

func (in *FornaxSecret) New() runtime.Object {
	return &corev1.Secret{}
}

func (in *FornaxSecret) NewList() runtime.Object {
	return &corev1.SecretList{}
}

func (in *FornaxSecret) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "k8s.io",
		Version:  "v1",
		Resource: "secrets",
	}
}

func (in *FornaxSecret) IsStorageVersion() bool {
	return true
}

func (in *FornaxSecret) GetObjectMeta() *metav1.ObjectMeta {
	return &(in.ObjectMeta)
}
