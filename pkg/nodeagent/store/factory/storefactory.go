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

package factory

import (
	"fmt"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/session"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/store"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/store/sqlite"
)

type PodStore struct {
	store.Store
}

type SessionStore struct {
	store.Store
}

type ContainerStore struct {
	store.Store
}

func NewPodSqliteStore(options *sqlite.SQLiteStoreOptions) (*PodStore, error) {
	if store, err := sqlite.NewSqliteStore("Pod", options,
		func(text string) (interface{}, error) { return store.JsonToPod(text) },
		func(obj interface{}) (string, error) { return store.JsonFromPod(obj.(*pod.Pod)) }); err != nil {
		return nil, err
	} else {
		return &PodStore{store}, nil
	}
}

func (s *PodStore) GetPod(identifier string) (*pod.Pod, error) {
	obj, err := s.GetObject(identifier)
	if err != nil {
		return nil, err
	}
	if v, ok := obj.(*pod.Pod); !ok {
		return nil, fmt.Errorf("%v not a Pod object", obj)
	} else {
		return v, nil
	}
}

func (s *PodStore) PutPod(pod *pod.Pod) error {
	if pod == nil {
		return fmt.Errorf("nil pod is passed")
	}
	err := s.PutObject(pod.Identifier, pod)
	if err != nil {
		return err
	}
	return nil
}

func NewSessionSqliteStore(options *sqlite.SQLiteStoreOptions) (*SessionStore, error) {
	if store, err := sqlite.NewSqliteStore("Session", options,
		func(text string) (interface{}, error) { return store.JsonToSession(text) },
		func(obj interface{}) (string, error) { return store.JsonFromSession(obj.(*session.Session)) }); err != nil {
		return nil, err
	} else {
		return &SessionStore{store}, nil
	}
}

func (s *SessionStore) GetSession(identifier string) (*session.Session, error) {
	obj, err := s.GetObject(identifier)
	if err != nil {
		return nil, err
	}
	if v, ok := obj.(*session.Session); !ok {
		return nil, fmt.Errorf("%v not a Session object", obj)
	} else {
		return v, nil
	}
}

func (s *SessionStore) PutSession(session *session.Session) error {
	if session == nil {
		return fmt.Errorf("nil session is passed")
	}
	err := s.PutObject(session.Identifier, session)
	if err != nil {
		return err
	}
	return nil
}

func NewContainerSqliteStore(options *sqlite.SQLiteStoreOptions) (*ContainerStore, error) {
	if store, err := sqlite.NewSqliteStore("Container", options,
		func(text string) (interface{}, error) { return store.JsonToContainer(text) },
		func(obj interface{}) (string, error) { return store.JsonFromContainer(obj.(*pod.Container)) }); err != nil {
		return nil, err
	} else {
		return &ContainerStore{store}, nil
	}
}

func (s *ContainerStore) GetContainer(identifier string) (*pod.Container, error) {
	obj, err := s.GetObject(identifier)
	if err != nil {
		return nil, err
	}
	if v, ok := obj.(*pod.Container); !ok {
		return nil, fmt.Errorf("%v not a store.Container object", obj)
	} else {
		return v, nil
	}
}

func (s *ContainerStore) PutContainer(container *pod.Container) error {
	if container == nil {
		return fmt.Errorf("nil container is passed")
	}
	err := s.PutObject(container.Identifier, container)
	if err != nil {
		return err
	}
	return nil
}
