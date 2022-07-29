package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FornaxPod struct {
	corev1.Pod
}

func (in *FornaxPod) NamespaceScoped() bool {
	return true
}

func (in *FornaxPod) New() runtime.Object {
	return &corev1.Pod{}
}

func (in *FornaxPod) NewList() runtime.Object {
	return &corev1.PodList{}
}

func (in *FornaxPod) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "k8s.io",
		Version:  "v1",
		Resource: "pods",
	}
}

func (in *FornaxPod) IsStorageVersion() bool {
	return true
}

func (in *FornaxPod) GetObjectMeta() *metav1.ObjectMeta {
	return &(in.ObjectMeta)
}
