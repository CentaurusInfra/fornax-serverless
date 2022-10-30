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
	"fmt"
	"os"
	"reflect"
	"testing"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/store/storage/sqlite"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewATestSession(id string, revision uint64) *fornaxtypes.FornaxSession {
	testSession := fornaxtypes.FornaxSession{
		Identifier:    id,
		PodIdentifier: id,
		Session: &fornaxv1.ApplicationSession{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ApplicationSession",
				APIVersion: "centaurusinfra.io/fornax-serverless/core/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:            id,
				GenerateName:    id,
				Namespace:       "test",
				ResourceVersion: fmt.Sprintf("%d", revision),
				Generation:      0,
			},
			Spec:   fornaxv1.ApplicationSessionSpec{},
			Status: fornaxv1.ApplicationSessionStatus{},
		},
	}
	return &testSession
}

func NewATestPod(id string, revision int64) *fornaxtypes.FornaxPod {
	testPod := fornaxtypes.FornaxPod{
		Identifier:     id,
		FornaxPodState: "PodStateCreated",
		Pod: &v1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "k8s.io/core/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:            id,
				Namespace:       "test",
				GenerateName:    id,
				ResourceVersion: fmt.Sprintf("%d", revision),
				Generation:      0,
			},
			Spec:   v1.PodSpec{},
			Status: v1.PodStatus{},
		},
		ConfigMap:  &v1.ConfigMap{},
		RuntimePod: &runtime.Pod{},
	}
	return &testPod
}

func TestNewPodSqliteStore(t *testing.T) {
	type args struct {
		options *sqlite.SQLiteStoreOptions
	}
	tests := []struct {
		name    string
		args    args
		want    *PodStore
		wantErr bool
	}{
		{
			name: "malformed connection url",
			args: args{
				options: &sqlite.SQLiteStoreOptions{
					ConnUrl: "/fff/test.db",
				},
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewPodSqliteStore(tt.args.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPodSqliteStore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewPodSqliteStore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPodStore_GetPod(t *testing.T) {
	store, _ := NewPodSqliteStore(&sqlite.SQLiteStoreOptions{
		ConnUrl: "./test.db",
	})
	defer os.Remove("./test.db")
	testPod := NewATestPod("testPod1", 1)
	store.PutPod(testPod, 1)

	tests := []struct {
		name       string
		identifier string
		want       *fornaxtypes.FornaxPod
		wantErr    bool
	}{
		{
			name:       "donotexist-test",
			identifier: "donotexist",
			want:       nil,
			wantErr:    true,
		},
		{
			name:       "putgettest",
			identifier: "testPod1",
			want:       testPod,
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetPod(tt.identifier)
			if (err != nil) != tt.wantErr {
				t.Errorf("PodStore.GetPod() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PodStore.GetPod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPodStore_PutPod_Updated(t *testing.T) {
	store, _ := NewPodSqliteStore(&sqlite.SQLiteStoreOptions{
		ConnUrl: "./test.db",
	})
	defer os.Remove("./test.db")
	testPod := NewATestPod("testPod1", 1)
	testPod.FornaxPodState = "PodStateRunning"
	testPod2 := NewATestPod("testPod1", 2)
	testPod2.FornaxPodState = "PodStateTerminated"
	tests := []struct {
		name     string
		args     *fornaxtypes.FornaxPod
		revision int64
		wantErr  bool
	}{
		{
			name:     "goodput",
			args:     testPod,
			revision: 1,
			wantErr:  false,
		},
		{
			name:     "duplicateReplace",
			args:     testPod2,
			revision: 2,
			wantErr:  false,
		},
		{
			name:     "nilobj",
			args:     nil,
			revision: 2,
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.PutPod(tt.args, tt.revision); (err != nil) != tt.wantErr {
				t.Errorf("PodStore.PutPod() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	if s, err := store.GetPod("testPod1"); err != nil || s == nil || s.Pod.ResourceVersion != testPod2.Pod.ResourceVersion || s.FornaxPodState != testPod2.FornaxPodState {
		t.Error("pod is not updated although revision is bumped")
	}
}

func TestPodStore_PutPod_NotUpdated(t *testing.T) {
	store, _ := NewPodSqliteStore(&sqlite.SQLiteStoreOptions{
		ConnUrl: "./test.db",
	})
	defer os.Remove("./test.db")
	testPod := NewATestPod("testPod1", 1)
	testPod.FornaxPodState = "PodStateRunning"
	testPod2 := NewATestPod("testPod1", 2)
	testPod2.FornaxPodState = "PodStateTerminated"
	tests := []struct {
		name     string
		args     *fornaxtypes.FornaxPod
		revision int64
		wantErr  bool
	}{
		{
			name:     "goodput",
			args:     testPod,
			revision: 1,
			wantErr:  false,
		},
		{
			name:     "duplicateReplace",
			args:     testPod2,
			revision: 1,
			wantErr:  false,
		},
		{
			name:    "nilobj",
			args:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.PutPod(tt.args, tt.revision); (err != nil) != tt.wantErr {
				t.Errorf("PodStore.PutPod() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	if s, err := store.GetPod("testPod1"); err != nil || s == nil || s.Pod.ResourceVersion != testPod.Pod.ResourceVersion || s.FornaxPodState != testPod.FornaxPodState {
		t.Error("pod is updated although revision is not bumped")
	}
}

func TestNewSessionSqliteStore(t *testing.T) {
	type args struct {
		options *sqlite.SQLiteStoreOptions
	}
	tests := []struct {
		name    string
		args    args
		want    *SessionStore
		wantErr bool
	}{
		{
			name: "malformed connection url",
			args: args{
				options: &sqlite.SQLiteStoreOptions{
					ConnUrl: "/fff/test.db",
				},
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSessionSqliteStore(tt.args.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSessionSqliteStore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewSessionSqliteStore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionStore_GetSession(t *testing.T) {
	store, _ := NewSessionSqliteStore(&sqlite.SQLiteStoreOptions{
		ConnUrl: "./test.db",
	})
	defer os.Remove("./test.db")
	testSession := NewATestSession("session1", 1)
	store.PutSession(testSession, 1)

	tests := []struct {
		name       string
		identifier string
		want       *fornaxtypes.FornaxSession
		wantErr    bool
	}{
		{
			name:       "donotexist",
			identifier: "donotexist",
			want:       nil,
			wantErr:    true,
		},
		{
			name:       "putget",
			identifier: "session1",
			want:       testSession,
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetSession(tt.identifier)
			if (err != nil) != tt.wantErr {
				t.Errorf("SessionStore.GetSession() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SessionStore.GetSession() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionStore_PutSession_Updated(t *testing.T) {
	store, _ := NewSessionSqliteStore(&sqlite.SQLiteStoreOptions{
		ConnUrl: "./test.db",
	})
	defer os.Remove("./test.db")

	testSession := NewATestSession("session1", 1)
	testSession.Session.Status.SessionStatus = fornaxv1.SessionStatusAvailable
	testSession2 := NewATestSession("session1", 2)
	testSession2.Session.Status.SessionStatus = fornaxv1.SessionStatusClosed
	tests := []struct {
		name     string
		session  *fornaxtypes.FornaxSession
		revision int64
		wantErr  bool
	}{
		{
			name:     "put",
			session:  testSession,
			revision: 1,
			wantErr:  false,
		},
		{
			name:     "upd",
			session:  testSession2,
			revision: 2,
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.PutSession(tt.session, tt.revision); (err != nil) != tt.wantErr {
				t.Errorf("SessionStore.PutSession() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	if s, err := store.GetSession("session1"); err != nil || s == nil || s.Session.ResourceVersion != testSession2.Session.ResourceVersion || s.Session.Status.SessionStatus != testSession2.Session.Status.SessionStatus {
		t.Error("session is not updated, although revision changed")
	}
}

func TestSessionStore_PutSession_NotUpdated(t *testing.T) {
	store, _ := NewSessionSqliteStore(&sqlite.SQLiteStoreOptions{
		ConnUrl: "./test.db",
	})
	defer os.Remove("./test.db")

	testSession := NewATestSession("session1", 1)
	testSession.Session.Status.SessionStatus = fornaxv1.SessionStatusAvailable
	testSession2 := NewATestSession("session1", 1)
	testSession2.Session.Status.SessionStatus = fornaxv1.SessionStatusClosed
	tests := []struct {
		name     string
		session  *fornaxtypes.FornaxSession
		revision int64
		wantErr  bool
	}{
		{
			name:     "put",
			session:  testSession,
			revision: 1,
			wantErr:  false,
		},
		{
			name:     "upd",
			session:  testSession2,
			revision: 1,
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.PutSession(tt.session, tt.revision); (err != nil) != tt.wantErr {
				t.Errorf("SessionStore.PutSession() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	if s, err := store.GetSession("session1"); err != nil || s == nil || s.Session.ResourceVersion != testSession.Session.ResourceVersion || s.Session.Status.SessionStatus != testSession.Session.Status.SessionStatus {
		t.Error("session is updated although revision is not changed")
	}
}
