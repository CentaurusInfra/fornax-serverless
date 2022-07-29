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
	"os"
	"reflect"
	"testing"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/store/sqlite"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewATestSession(id string) *fornaxtypes.Session {
	testSession := fornaxtypes.Session{
		Identifier:    id,
		PodIdentifier: "",
		Pod:           &v1.Pod{},
		Session:       &fornaxv1.ApplicationSession{},
		SessionState:  "",
	}
	return &testSession
}

func NewATestPod(id string) *fornaxtypes.FornaxPod {
	testPod := fornaxtypes.FornaxPod{
		Identifier:     id,
		ApplicationId:  "applicationId",
		FornaxPodState: "PodStateCreated",
		Pod: &v1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "k8s.io/core/v1",
			},
			ObjectMeta: metav1.ObjectMeta{},
			Spec:       v1.PodSpec{},
			Status:     v1.PodStatus{},
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
	testPod := NewATestPod("testPod1")
	store.PutPod(testPod)

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

func TestPodStore_PutPod(t *testing.T) {
	store, _ := NewPodSqliteStore(&sqlite.SQLiteStoreOptions{
		ConnUrl: "./test.db",
	})
	defer os.Remove("./test.db")
	testPod := NewATestPod("testPod1")
	testPod2 := NewATestPod("testPod1")
	testPod2.FornaxPodState = "PodStateTerminated"
	tests := []struct {
		name    string
		args    *fornaxtypes.FornaxPod
		wantErr bool
	}{
		{
			name:    "goodput",
			args:    testPod,
			wantErr: false,
		},
		{
			name:    "duplicateReplace",
			args:    testPod2,
			wantErr: false,
		},
		{
			name:    "nilobj",
			args:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.PutPod(tt.args); (err != nil) != tt.wantErr {
				t.Errorf("PodStore.PutPod() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
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
	testSession := NewATestSession("session1")
	store.PutSession(testSession)

	tests := []struct {
		name       string
		identifier string
		want       *fornaxtypes.Session
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

func TestSessionStore_PutSession(t *testing.T) {
	store, _ := NewSessionSqliteStore(&sqlite.SQLiteStoreOptions{
		ConnUrl: "./test.db",
	})
	defer os.Remove("./test.db")

	testSession := NewATestSession("session1")
	testSession2 := NewATestSession("session1")
	testSession2.SessionState = "SessionStateClosed"
	tests := []struct {
		name    string
		session *fornaxtypes.Session
		wantErr bool
	}{
		{
			name:    "put",
			session: testSession,
			wantErr: false,
		},
		{
			name:    "duplicateError",
			session: testSession,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.PutSession(tt.session); (err != nil) != tt.wantErr {
				t.Errorf("SessionStore.PutSession() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
