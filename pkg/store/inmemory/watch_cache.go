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
	"strings"
	"sync"

	"k8s.io/apiserver/pkg/storage"
)

type watchCache struct {
	size    uint64
	events  []*objEvent
	cacheMu sync.RWMutex
}

func (wc *watchCache) getObjEventsAfterRev(key string, rev uint64, opts storage.ListOptions) (objEvents []*objEvent, err error) {
	wc.cacheMu.RLock()
	defer wc.cacheMu.RUnlock()

	for _, event := range wc.events {
		if strings.HasPrefix(event.key, key) && event.rev > rev {
			objEvents = append(objEvents, event)
		}
	}

	return objEvents, nil
}

func (wc *watchCache) addObjEvents(event *objEvent) {
	wc.cacheMu.Lock()
	defer wc.cacheMu.Unlock()
	wc.events = append(wc.events, event)
}

func (wc *watchCache) shrink() {
	if uint64(len(wc.events)) > wc.size {
		events := wc.events[0:wc.size]
		wc.events = events
		wc.cacheMu.Lock()
		defer wc.cacheMu.Unlock()
	}
}
