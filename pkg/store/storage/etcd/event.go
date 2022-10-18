/*
Copyright 2016 The Kubernetes Authors.

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

package etcd

import (
	"fmt"

	"centaurusinfra.io/fornax-serverless/pkg/store/storage"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// parseKV converts a KeyValue retrieved from an initial sync() listing to a synthetic isCreated event.
func parseKV(kv *mvccpb.KeyValue) *storage.RevisionEvent {
	return &storage.RevisionEvent{
		Key:       string(kv.Key),
		Value:     kv.Value,
		PrevValue: nil,
		Rev:       kv.ModRevision,
		IsDeleted: false,
		IsCreated: true,
	}
}

func parseEvent(e *clientv3.Event) (*storage.RevisionEvent, error) {
	if !e.IsCreate() && e.PrevKv == nil {
		// If the previous value is nil, error. One example of how this is possible is if the previous value has been compacted already.
		return nil, fmt.Errorf("etcd event received with PrevKv=nil (key=%q, modRevision=%d, type=%s)", string(e.Kv.Key), e.Kv.ModRevision, e.Type.String())

	}
	ret := &storage.RevisionEvent{
		Key:       string(e.Kv.Key),
		Value:     e.Kv.Value,
		Rev:       e.Kv.ModRevision,
		IsDeleted: e.Type == clientv3.EventTypeDelete,
		IsCreated: e.IsCreate(),
	}
	if e.PrevKv != nil {
		ret.PrevValue = e.PrevKv.Value
	}
	return ret, nil
}

func progressNotifyEvent(rev int64) *storage.RevisionEvent {
	return &storage.RevisionEvent{
		Rev:              rev,
		IsProgressNotify: true,
	}
}

func errorEvent(rev int64) *storage.RevisionEvent {
	return &storage.RevisionEvent{
		IsError: true,
	}
}
