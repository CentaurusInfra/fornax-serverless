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

package session

import (
	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	v1 "k8s.io/api/core/v1"
)

type Session struct {
	Identifier    string                       `json:"identifier,omitempty"`
	PodIdentifier string                       `json:"podIdentifier,omitempty"`
	Pod           v1.Pod                       `json:"pod,omitempty"`
	Session       *fornaxv1.ApplicationSession `json:"session,omitempty"`
	SessionState  string                       `json:"sessionState,omitempty"`
}
