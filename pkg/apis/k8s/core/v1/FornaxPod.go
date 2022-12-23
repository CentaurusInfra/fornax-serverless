package v1

import (
	"fmt"

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
	return FornaxPodGrv
}

func (in *FornaxPod) IsStorageVersion() bool {
	return true
}

func (in *FornaxPod) GetObjectMeta() *metav1.ObjectMeta {
	return &(in.ObjectMeta)
}

var FornaxPodGrv = schema.GroupVersionResource{
	Group:    "",
	Version:  "v1",
	Resource: "pods",
}

var FornaxPodKind = K8sSchemeGroupVersion.WithKind("Pod")
var FornaxPodGrvKey = fmt.Sprintf("/%s", FornaxPodGrv.Resource)
