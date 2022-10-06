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

package inmemory

import (
	"context"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	apistorage "k8s.io/apiserver/pkg/storage"
)

type memoryStoreWatcher struct {
	ctx          context.Context
	stopped      bool
	recursive    bool
	stopChannel  chan bool
	incomingChan chan *objEvent
	outgoingChan chan watch.Event
	keyPrefix    string
	rev          int64
	predicate    apistorage.SelectionPredicate
}

func NewMemoryStoreWatcher(ctx context.Context, key string, rev int64, recursive, progressNotify bool, predicate apistorage.SelectionPredicate) *memoryStoreWatcher {
	if recursive && !strings.HasSuffix(key, "/") {
		key += "/"
	}
	watcher := &memoryStoreWatcher{
		ctx:          ctx,
		stopped:      false,
		keyPrefix:    key,
		rev:          rev,
		recursive:    recursive,
		predicate:    predicate,
		stopChannel:  make(chan bool, 1),
		incomingChan: make(chan *objEvent, 100),
		outgoingChan: make(chan watch.Event, 100),
	}
	if predicate.Empty() {
		// The filter doesn't filter out any object.
		watcher.predicate = apistorage.Everything
	}
	go watcher.run()
	return watcher
}

func (wc *memoryStoreWatcher) filter(obj runtime.Object) bool {
	if wc.predicate.Empty() {
		return true
	}
	matched, err := wc.predicate.Matches(obj)
	return err == nil && matched
}

func (wc *memoryStoreWatcher) acceptAll() bool {
	return wc.predicate.Empty()
}

func (wc *memoryStoreWatcher) run() {
	defer func() {
		wc.stopped = true
		close(wc.incomingChan)
		close(wc.outgoingChan)
	}()
	for {
		select {
		case <-wc.ctx.Done():
			return
		case <-wc.stopChannel:
			return
		case event := <-wc.incomingChan:
			wcEvent := wc.transform(event)
			if wcEvent != nil {
				wc.outgoingChan <- *wcEvent
			}
		}
	}
}

func (wc *memoryStoreWatcher) Receive(event *objEvent) error {
	wc.incomingChan <- event
	return nil
}

// ResultChan implements watch.Interface
func (wc *memoryStoreWatcher) ResultChan() <-chan watch.Event {
	return wc.outgoingChan
}

// Stop implements watch.Interface
func (wc *memoryStoreWatcher) Stop() {
	wc.stopChannel <- true
}

func (wc *memoryStoreWatcher) transform(e *objEvent) (res *watch.Event) {
	if wc.recursive {
		if !strings.HasPrefix(e.key, wc.keyPrefix) {
			return nil
		}
	} else {
		// not recursive, check exact match
		if e.key != wc.keyPrefix {
			return nil
		}
	}
	curObj, oldObj := e.obj, e.prevObj

	switch {
	case e.isDeleted:
		if !wc.filter(oldObj) {
			return nil
		}
		res = &watch.Event{
			Type:   watch.Deleted,
			Object: oldObj,
		}
	case e.isCreated:
		if !wc.filter(curObj) {
			return nil
		}
		res = &watch.Event{
			Type:   watch.Added,
			Object: curObj,
		}
	default:
		if wc.acceptAll() {
			res = &watch.Event{
				Type:   watch.Modified,
				Object: curObj,
			}
			return res
		}
		curObjPasses := wc.filter(curObj)
		oldObjPasses := wc.filter(oldObj)
		switch {
		case curObjPasses && oldObjPasses:
			res = &watch.Event{
				Type:   watch.Modified,
				Object: curObj,
			}
		case curObjPasses && !oldObjPasses:
			res = &watch.Event{
				Type:   watch.Modified,
				Object: curObj,
			}
		case curObjPasses && !oldObjPasses:
			res = &watch.Event{
				Type:   watch.Added,
				Object: curObj,
			}
		case !curObjPasses && oldObjPasses:
			res = &watch.Event{
				Type:   watch.Deleted,
				Object: oldObj,
			}
		}
	}
	return res
}

var _ watch.Interface = &memoryStoreWatcher{}
