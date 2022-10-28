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

package storage

import (
	"errors"
)

var (
	ObjectNotFound    = errors.New("no such object")
	InvalidObjectType = errors.New("object type is not expected")
)

type RevisionObject struct {
	Obj      interface{}
	Revision int64
}

type Store interface {
	ListObject() ([]interface{}, error)
	DelObject(key string) error
	GetObject(key string) (interface{}, error)
	PutObject(key string, obj interface{}, revision int64) error
}

type TextFromObjectFunc func(interface{}) ([]byte, error)
type TextToObjectFunc func([]byte) (interface{}, error)

type RevisionEvent struct {
	Key              string
	Value            []byte
	PrevValue        []byte
	Rev              int64
	Err              error
	IsDeleted        bool
	IsCreated        bool
	IsError          bool
	IsProgressNotify bool
}
