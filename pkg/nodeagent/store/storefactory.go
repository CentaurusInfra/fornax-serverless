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
	"fmt"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/store/storage"
	"centaurusinfra.io/fornax-serverless/pkg/store/storage/sqlite"
	"k8s.io/klog/v2"
)

type NodeStore struct {
	storage.Store
}

type PodStore struct {
	storage.Store
}

type SessionStore struct {
	storage.Store
}

func NewNodeSqliteStore(options *sqlite.SQLiteStoreOptions) (*NodeStore, error) {
	if store, err := sqlite.NewSqliteStore("Node", options,
		func(text []byte) (interface{}, error) { return JsonToNode(text) },
		func(obj interface{}) ([]byte, error) { return JsonFromNode(obj.(*types.FornaxNodeWithRevision)) }); err != nil {
		return nil, err
	} else {
		return &NodeStore{store}, nil
	}
}

func (s *NodeStore) GetNode(identifier string) (*types.FornaxNodeWithRevision, error) {
	obj, err := s.GetObject(identifier)
	if err != nil {
		return nil, err
	}
	if v, ok := obj.(*types.FornaxNodeWithRevision); !ok {
		return nil, fmt.Errorf("%v not a Node object", obj)
	} else {
		return v, nil
	}
}

func (s *NodeStore) PutNode(node *types.FornaxNodeWithRevision, revision int64) error {
	if node == nil {
		return fmt.Errorf("nil node is passed")
	}
	err := s.PutObject(node.Identifier, node, revision)
	if err != nil {
		return err
	}
	return nil
}

func NewPodSqliteStore(options *sqlite.SQLiteStoreOptions) (*PodStore, error) {
	if store, err := sqlite.NewSqliteStore("Pod", options,
		func(text []byte) (interface{}, error) { return JsonToPod(text) },
		func(obj interface{}) ([]byte, error) { return JsonFromPod(obj.(*types.FornaxPod)) }); err != nil {
		return nil, err
	} else {
		return &PodStore{store}, nil
	}
}

func (s *PodStore) GetPod(identifier string) (*types.FornaxPod, error) {
	obj, err := s.GetObject(identifier)
	if err != nil {
		return nil, err
	}
	if v, ok := obj.(*types.FornaxPod); !ok {
		return nil, fmt.Errorf("%v not a Pod object", obj)
	} else {
		return v, nil
	}
}

func (s *PodStore) PutPod(pod *types.FornaxPod, revision int64) error {
	if pod == nil {
		return fmt.Errorf("nil pod is passed")
	}
	err := s.PutObject(pod.Identifier, pod, revision)
	if err != nil {
		klog.ErrorS(err, "Failed to save FornaxPod", "name", pod.Identifier)
		return err
	}
	return nil
}

func NewSessionSqliteStore(options *sqlite.SQLiteStoreOptions) (*SessionStore, error) {
	if store, err := sqlite.NewSqliteStore("Session", options,
		func(text []byte) (interface{}, error) { return JsonToSession(text) },
		func(obj interface{}) ([]byte, error) { return JsonFromSession(obj.(*types.FornaxSession)) }); err != nil {
		return nil, err
	} else {
		return &SessionStore{store}, nil
	}
}

func (s *SessionStore) GetSession(identifier string) (*types.FornaxSession, error) {
	obj, err := s.GetObject(identifier)
	if err != nil {
		return nil, err
	}
	if v, ok := obj.(*types.FornaxSession); !ok {
		return nil, fmt.Errorf("%v not a Session object", obj)
	} else {
		return v, nil
	}
}

func (s *SessionStore) PutSession(session *types.FornaxSession, revision int64) error {
	if session == nil {
		return fmt.Errorf("nil session is passed")
	}
	err := s.PutObject(session.Identifier, session, revision)
	if err != nil {
		return err
	}
	return nil
}

// use json to store node agent store object for now, consider using protobuf if meet performance issue
func JsonToPod(data []byte) (*types.FornaxPod, error) {
	res := types.FornaxPod{}
	if err := json.Unmarshal([]byte(data), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func JsonFromPod(obj *types.FornaxPod) ([]byte, error) {
	var bytes []byte
	var err error
	if bytes, err = json.Marshal(obj); err != nil {
		return nil, err
	}
	return bytes, nil
}

// use json to store node agent store object for now, consider using protobuf if meet performance issue
func JsonToNode(text []byte) (*types.FornaxNodeWithRevision, error) {
	res := types.FornaxNodeWithRevision{}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func JsonFromNode(obj *types.FornaxNodeWithRevision) ([]byte, error) {
	var bytes []byte
	var err error
	if bytes, err = json.Marshal(obj); err != nil {
		return nil, err
	}
	return bytes, nil
}

func JsonToSession(text []byte) (*types.FornaxSession, error) {
	res := types.FornaxSession{}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func JsonFromSession(obj *types.FornaxSession) ([]byte, error) {
	var bytes []byte
	var err error
	if bytes, err = json.Marshal(obj); err != nil {
		return nil, err
	}
	return bytes, nil
}
