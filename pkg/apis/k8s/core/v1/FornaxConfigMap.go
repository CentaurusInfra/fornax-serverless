package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FornaxConfigMap struct {
	corev1.ConfigMap
}

func (in *FornaxConfigMap) NamespaceScoped() bool {
	return true
}

func (in *FornaxConfigMap) New() runtime.Object {
	return &corev1.ConfigMap{}
}

func (in *FornaxConfigMap) NewList() runtime.Object {
	return &corev1.ConfigMapList{}
}

func (in *FornaxConfigMap) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "k8s.io",
		Version:  "v1",
		Resource: "configmaps",
	}
}

func (in *FornaxConfigMap) IsStorageVersion() bool {
	return true
}

func (in *FornaxConfigMap) GetObjectMeta() *metav1.ObjectMeta {
	return &(in.ObjectMeta)
}
