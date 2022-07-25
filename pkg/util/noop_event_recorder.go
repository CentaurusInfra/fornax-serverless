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

package util

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

func NewNoopEventRecorder() record.EventRecorder {
	return &NoopEventRecorder{}
}

var _ record.EventRecorder = &NoopEventRecorder{}

type NoopEventRecorder struct {
}

// AnnotatedEventf implements record.EventRecorder
func (*NoopEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype string, reason string, messageFmt string, args ...interface{}) {
	return
}

// Event implements record.EventRecorder
func (*NoopEventRecorder) Event(object runtime.Object, eventtype string, reason string, message string) {
	return
}

// Eventf implements record.EventRecorder
func (*NoopEventRecorder) Eventf(object runtime.Object, eventtype string, reason string, messageFmt string, args ...interface{}) {
	return
}
