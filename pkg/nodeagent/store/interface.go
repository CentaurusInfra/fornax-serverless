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
	"errors"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
)

var (
	StoreObjectNotFound = errors.New("no such object")
)

type Store interface {
	ListObject() ([]interface{}, error)
	DelObject(indentifier string) error
	GetObject(indentifier string) (interface{}, error)
	PutObject(indentifier string, obj interface{}) error
}

// use json to store node agent store object for now, consider using protobuf if meet performance issue
func JsonToPod(str string) (*types.FornaxPod, error) {
	res := types.FornaxPod{}
	if err := json.Unmarshal([]byte(str), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func JsonFromPod(obj *types.FornaxPod) (string, error) {
	var bytes []byte
	var err error
	if bytes, err = json.Marshal(obj); err != nil {
		return "", err
	}
	return string(bytes), nil
}

// use json to store node agent store object for now, consider using protobuf if meet performance issue
func JsonToNode(str string) (*types.FornaxNodeWithRevision, error) {
	res := types.FornaxNodeWithRevision{}
	if err := json.Unmarshal([]byte(str), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func JsonFromNode(obj *types.FornaxNodeWithRevision) (string, error) {
	var bytes []byte
	var err error
	if bytes, err = json.Marshal(obj); err != nil {
		return "", err
	}
	return string(bytes), nil
}

func JsonToSession(str string) (*types.FornaxSession, error) {
	res := types.FornaxSession{}
	if err := json.Unmarshal([]byte(str), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func JsonFromSession(obj *types.FornaxSession) (string, error) {
	var bytes []byte
	var err error
	if bytes, err = json.Marshal(obj); err != nil {
		return "", err
	}
	return string(bytes), nil
}
