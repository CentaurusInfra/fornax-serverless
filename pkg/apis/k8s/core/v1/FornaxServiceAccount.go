package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FornaxServiceAccount struct {
	corev1.ServiceAccount
}

func (in *FornaxServiceAccount) NamespaceScoped() bool {
	return true
}

func (in *FornaxServiceAccount) New() runtime.Object {
	return &corev1.ServiceAccount{}
}

func (in *FornaxServiceAccount) NewList() runtime.Object {
	return &corev1.ServiceAccountList{}
}

func (in *FornaxServiceAccount) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "k8s.io",
		Version:  "v1",
		Resource: "serviceaccounts",
	}
}

func (in *FornaxServiceAccount) IsStorageVersion() bool {
	return true
}

func (in *FornaxServiceAccount) GetObjectMeta() *metav1.ObjectMeta {
	return &(in.ObjectMeta)
}
