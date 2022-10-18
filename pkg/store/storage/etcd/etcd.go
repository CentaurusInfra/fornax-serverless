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

package etcd

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"google.golang.org/grpc"

	"centaurusinfra.io/fornax-serverless/pkg/store/storage"
	etcdv3 "go.etcd.io/etcd/client/v3"

	"k8s.io/klog/v2"
)

type etcdStore struct {
	ctx            context.Context
	client         *etcdv3.Client
	pathPrefix     string
	toObjectFunc   storage.TextToObjectFunc
	fromObjectFunc storage.TextFromObjectFunc
}

// Watch implements storage.StoreWithRevision
func (s *etcdStore) Watch(key string, rev int64, update chan *storage.RevisionEvent) error {
	options := make([]etcdv3.OpOption, 0, 4)
	options = append(options, etcdv3.WithRev(rev))
	watchChan := s.client.Watch(s.ctx, key, options...)
	go func() {
		for wres := range watchChan {
			if wres.Err() != nil {
				update <- errorEvent(wres.Header.Revision)
				return
			}
			if wres.IsProgressNotify() {
				update <- progressNotifyEvent(wres.Header.Revision)
				continue
			}

			for _, e := range wres.Events {
				parsedEvent, err := parseEvent(e)
				if err != nil {
					update <- errorEvent(wres.Header.Revision)
					return
				}
				update <- parsedEvent
			}
		}
	}()
	return nil
}

// DelObject implements store.StoreWithRevision
func (s *etcdStore) DelObject(key string, rev int64) error {
	klog.InfoS("GWJ delete a obj", "key", key)

	getResp, err := s.client.KV.Get(s.ctx, key)
	if err != nil {
		return err
	}
	if len(getResp.Kvs) == 0 {
		return storage.ObjectNotFound
	}

	txnResp, err := s.client.KV.Txn(s.ctx).If(
		etcdv3.Compare(etcdv3.ModRevision(key), "=", rev),
	).Then(
		etcdv3.OpDelete(key),
	).Else(
		etcdv3.OpGet(key),
	).Commit()
	if err != nil {
		return err
	}

	if !txnResp.Succeeded {
		return fmt.Errorf("Deletion of %s failed because of a revision conflict, please get a new revision and retry", key)
	}
	return nil
}

// GetObject implements storage.StoreWithRevision
func (s *etcdStore) GetObject(key string) (*storage.RevisionObject, error) {
	key = s.addPathPrefix(key)
	getResp, err := s.client.KV.Get(s.ctx, key)
	if err != nil {
		return nil, err
	}
	if len(getResp.Kvs) == 0 {
		return nil, storage.ObjectNotFound
	}

	kv := getResp.Kvs[0]
	obj, err := s.toObjectFunc(kv.Value)
	if err != nil {
		return nil, err
	}
	robj := &storage.RevisionObject{
		Obj:      obj,
		Revision: kv.ModRevision,
	}
	klog.InfoS("GWJ get a obj", "key", key, "output", robj)
	return robj, nil
}

// ListObject implements storage.StoreWithRevision
func (s *etcdStore) ListObject(rev int64) ([]*storage.RevisionObject, error) {
	objects := []*storage.RevisionObject{}
	options := make([]etcdv3.OpOption, 0, 4)
	rangeEnd := etcdv3.GetPrefixRangeEnd(s.pathPrefix)
	options = append(options, etcdv3.WithRange(rangeEnd))
	options = append(options, etcdv3.WithLimit(1000))
	options = append(options, etcdv3.WithPrefix())
	if rev != 0 {
		options = append(options, etcdv3.WithRev(rev))
	}
	for {
		getResp, err := s.client.KV.Get(s.ctx, s.pathPrefix, options...)
		if err != nil {
			return nil, err
		}
		for _, v := range getResp.Kvs {
			obj, err := s.toObjectFunc(v.Value)
			if err != nil {
				return nil, err
			}
			if obj != nil {
				robj := &storage.RevisionObject{
					Obj:      objects,
					Revision: rev,
				}
				objects = append(objects, robj)
			}
		}

		hasMore := getResp.More
		if len(getResp.Kvs) == 0 && hasMore {
			return nil, fmt.Errorf("return nothing but etcd says there are still more")
		}
		if !hasMore {
			break
		} else {
			options = append(options, etcdv3.WithRev((getResp.Header.Revision)))
		}
	}

	return objects, nil
}

// PutObject implements storage.StoreWithRevision
func (s *etcdStore) PutObject(key string, obj interface{}, rev int64) (int64, error) {
	key = s.addPathPrefix(key)
	klog.InfoS("GWJ create a obj", "key", key, "input", obj)
	data, err := s.fromObjectFunc(obj)
	if err != nil {
		return 0, err
	}

	txnResp, err := s.client.KV.Txn(s.ctx).If(
		etcdv3.Compare(etcdv3.ModRevision(key), "=", rev),
	).Then(
		etcdv3.OpPut(key, string(data)),
	).Else(
		etcdv3.OpGet(key),
	).Commit()
	if err != nil {
		return 0, err
	}

	if !txnResp.Succeeded {
		return 0, fmt.Errorf("Update of %s failed because of a revision conflict, please get a new revision and retry", key)
	}

	return txnResp.Header.Revision, nil
}

func (s *etcdStore) addPathPrefix(key string) string {
	if !strings.HasPrefix(key, s.pathPrefix) {
		return s.pathPrefix + key
	}
	return key
}

// return a simple non secured etcd client, add tls config later
func newETCD3Client(endpoints []string) (*etcdv3.Client, error) {
	dialOptions := []grpc.DialOption{
		grpc.WithBlock(), // block until the underlying connection is up
	}

	cfg := etcdv3.Config{
		DialTimeout:          5 * time.Second,
		DialKeepAliveTime:    5 * time.Minute,
		DialKeepAliveTimeout: 5 * time.Second,
		DialOptions:          dialOptions,
		Endpoints:            endpoints,
		TLS:                  nil,
	}

	return etcdv3.New(cfg)
}

func NewEtcdStore(ctx context.Context, etcdEndpoints []string, prefix string, toObjectFunc storage.TextToObjectFunc, fromObjectFunc storage.TextFromObjectFunc) (*etcdStore, error) {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	client, err := newETCD3Client(etcdEndpoints)
	if err != nil {
		return nil, err
	}
	result := &etcdStore{
		ctx:            ctx,
		client:         client,
		pathPrefix:     path.Join("/", prefix),
		toObjectFunc:   toObjectFunc,
		fromObjectFunc: fromObjectFunc,
	}
	return result, nil
}

var _ storage.StoreWithRevision = &etcdStore{}
