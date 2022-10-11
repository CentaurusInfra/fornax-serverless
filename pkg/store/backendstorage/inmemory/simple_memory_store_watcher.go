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

	"centaurusinfra.io/fornax-serverless/pkg/store"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	apistorage "k8s.io/apiserver/pkg/storage"
)

type memoryStoreWatcher struct {
	ctx                    context.Context
	stopped                bool
	recursive              bool
	stopChannel            chan bool
	incomingChan           chan *objEvent
	outgoingChan           chan watch.Event
	outgoingChanWithOldObj chan store.WatchEventWithOldObj
	keyPrefix              string
	predicate              apistorage.SelectionPredicate
}

func NewMemoryStoreWatcher(ctx context.Context, key string, recursive, progressNotify bool, predicate apistorage.SelectionPredicate) *memoryStoreWatcher {
	if recursive && !strings.HasSuffix(key, "/") {
		key += "/"
	}
	watcher := &memoryStoreWatcher{
		ctx:                    ctx,
		stopped:                false,
		keyPrefix:              key,
		recursive:              recursive,
		predicate:              predicate,
		stopChannel:            make(chan bool, 1),
		incomingChan:           make(chan *objEvent, 1000),
		outgoingChan:           make(chan watch.Event, 1000),
		outgoingChanWithOldObj: make(chan store.WatchEventWithOldObj, 1000),
	}
	// if predicate.Empty() {
	//  // The filter doesn't filter out any object.
	//  watcher.predicate = apistorage.Everything
	// }
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

// send existing events, then start consume events in channel, events should be larger than env
// pasted objEvents should be already sorted according to event's rev
func (wc *memoryStoreWatcher) run(rev uint64, existingObjEvents []*objEvent, eventWithOldObj bool) {
	defer func() {
		wc.stopped = true
		close(wc.incomingChan)
		close(wc.outgoingChan)
		close(wc.outgoingChanWithOldObj)
	}()

	startingRev := rev
	for _, event := range existingObjEvents {
		wcEvent := wc.transform(event)
		if wcEvent != nil {
			if eventWithOldObj {
				if e := wc.transformToEventWithOldObj(wcEvent, event.oldObj); e != nil {
					wc.outgoingChanWithOldObj <- *e
				}
			} else {
				wc.outgoingChan <- *wcEvent
			}
		}
		// existingObjEvents is supposed to sorted with rev, but do this check anyway
		if event.rev > startingRev {
			startingRev = event.rev
		}
	}

	for {
		select {
		case <-wc.ctx.Done():
			return
		case <-wc.stopChannel:
			return
		case event := <-wc.incomingChan:
			if event.rev > startingRev {
				startingRev = event.rev
				wcEvent := wc.transform(event)
				if wcEvent != nil {
					if eventWithOldObj {
						if e := wc.transformToEventWithOldObj(wcEvent, event.oldObj); e != nil {
							wc.outgoingChanWithOldObj <- *e
						}
					} else {
						wc.outgoingChan <- *wcEvent
					}
				}
			}
		}
	}
}

// ResultChan implements watch.Interface
func (wc *memoryStoreWatcher) ResultChan() <-chan watch.Event {
	return wc.outgoingChan
}

// ResultChanWithPrevobj implements WatchWithOldObjInterface
func (wc *memoryStoreWatcher) ResultChanWithPrevobj() <-chan store.WatchEventWithOldObj {
	return wc.outgoingChanWithOldObj
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
	curObj, oldObj := e.obj, e.oldObj

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

func (wc *memoryStoreWatcher) transformToEventWithOldObj(e *watch.Event, oldObj runtime.Object) (res *store.WatchEventWithOldObj) {
	switch e.Type {
	case watch.Added:
		return &store.WatchEventWithOldObj{
			Type:      e.Type,
			Object:    e.Object,
			OldObject: nil,
		}
	case watch.Deleted:
		return &store.WatchEventWithOldObj{
			Type:      e.Type,
			Object:    e.Object,
			OldObject: nil,
		}
	case watch.Modified:
		return &store.WatchEventWithOldObj{
			Type:      e.Type,
			Object:    e.Object,
			OldObject: oldObj,
		}
	default:
		return nil
	}
}

var _ watch.Interface = &memoryStoreWatcher{}
var _ store.WatchWithOldObjInterface = &memoryStoreWatcher{}
