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

package store

import (
	"encoding/json"

	v1 "k8s.io/api/core/v1"
)

type Store interface {
	DelObject(indentifier string) error
	GetObject(indentifier string) (interface{}, error)
	PutObject(indentifier string, obj interface{}) error
}

type Pod struct {
	Identifier string       `json:"identifier,omitempty"`
	Pod        v1.Pod       `json:"pod,omitempty"`
	PodState   string       `json:"podState,omitempty"`
	ConfigMap  v1.ConfigMap `json:"configMap,omitempty"`
}

type Session struct {
	Identifier   string `json:"identifier,omitempty"`
	SessionState string `json:"sessionState,omitempty"`

	Pod v1.Pod `json:"pod,omitempty"`
}

type Container struct {
	Identifier string       `json:"identifier,omitempty"`
	Container  v1.Container `json:"container,omitempty"`
}

// use json to store node agent store object for now, consider using protobuf if meet performance issue
func JsonToPod(str string) (*Pod, error) {
	res := Pod{}
	if err := json.Unmarshal([]byte(str), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func JsonFromPod(obj *Pod) (string, error) {
	var bytes []byte
	var err error
	if bytes, err = json.Marshal(obj); err != nil {
		return "", err
	}
	return string(bytes), nil
}

func JsonToContainer(str string) (*Container, error) {
	res := Container{}
	if err := json.Unmarshal([]byte(str), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func JsonFromContainer(obj *Container) (string, error) {
	var bytes []byte
	var err error
	if bytes, err = json.Marshal(obj); err != nil {
		return "", err
	}
	return string(bytes), nil
}

func JsonToSession(str string) (*Session, error) {
	res := Session{}
	if err := json.Unmarshal([]byte(str), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func JsonFromSession(obj *Session) (string, error) {
	var bytes []byte
	var err error
	if bytes, err = json.Marshal(obj); err != nil {
		return "", err
	}
	return string(bytes), nil
}
