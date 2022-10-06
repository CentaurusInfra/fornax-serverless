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

package inmemory

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	apistorage "k8s.io/apiserver/pkg/storage"
	"k8s.io/klog/v2"
)

// continueToken is a simple structured object for encoding the state of a continue token.
// TODO: if we change the version of the encoded from, we can't start encoding the new version
// until all other servers are upgraded (i.e. we need to support rolling schema)
// This is a public API struct and cannot change.
type continueToken struct {
	APIVersion      string `json:"v"`
	ResourceVersion int64  `json:"rv"`
	StartKey        string `json:"start"`
}

func getStateFromObject(versioner apistorage.Versioner, obj runtime.Object) (*objState, error) {
	state := &objState{
		obj:  obj,
		meta: &apistorage.ResponseMeta{},
	}

	rv, err := versioner.ObjectResourceVersion(obj)
	if err != nil {
		return nil, fmt.Errorf("couldn't get resource version: %v", err)
	}
	state.rev = rv
	state.meta.ResourceVersion = uint64(state.rev)

	// Compute the serialized form - for that we need to temporarily clean
	// its resource version field (those are not stored in etcd).
	if err := versioner.PrepareObjectForStorage(obj); err != nil {
		return nil, fmt.Errorf("PrepareObjectForStorage failed: %v", err)
	}
	if err := versioner.UpdateObject(state.obj, uint64(rv)); err != nil {
		klog.Errorf("failed to update object version: %v", err)
	}
	return state, nil
}

// validateMinimumResourceVersion returns a 'too large resource' version error when the provided minimumResourceVersion is
// greater than the most recent actualRevision available from storage.
func (ms *memoryStore) validateMinimumResourceVersion(versioner apistorage.Versioner, minimumResourceVersion string, actualRevision uint64) error {
	if minimumResourceVersion == "" {
		return nil
	}
	minimumRV, err := versioner.ParseResourceVersion(minimumResourceVersion)
	if err != nil {
		return apierrors.NewBadRequest(fmt.Sprintf("invalid resource version: %v", err))
	}
	// Enforce the storage.Interface guarantee that the resource version of the returned data
	// "will be at least 'resourceVersion'".
	if minimumRV > actualRevision {
		return apistorage.NewTooLargeResourceVersionError(minimumRV, actualRevision, 0)
	}
	return nil
}

// growSlice takes a slice value and grows its capacity up
// to the maximum of the passed sizes or maxCapacity, whichever
// is smaller. Above maxCapacity decisions about allocation are left
// to the Go runtime on append. This allows a caller to make an
// educated guess about the potential size of the total list while
// still avoiding overly aggressive initial allocation. If sizes
// is empty maxCapacity will be used as the size to grow.
func growSlice(v reflect.Value, maxCapacity int, sizes ...int) {
	cap := v.Cap()
	max := cap
	for _, size := range sizes {
		if size > max {
			max = size
		}
	}
	if len(sizes) == 0 || max > maxCapacity {
		max = maxCapacity
	}
	if max <= cap {
		return
	}
	if v.Len() > 0 {
		extra := reflect.MakeSlice(v.Type(), 0, max)
		reflect.Copy(extra, v)
		v.Set(extra)
	} else {
		extra := reflect.MakeSlice(v.Type(), 0, max)
		v.Set(extra)
	}
}

// appendListItem decodes and appends the object (if it passes filter) to v, which must be a slice.
func appendListItem(v reflect.Value, obj runtime.Object, rev uint64, pred apistorage.SelectionPredicate, versioner apistorage.Versioner) error {
	// being unable to set the version does not prevent the object from being extracted
	if err := versioner.UpdateObject(obj, rev); err != nil {
		klog.Errorf("failed to update object version: %v", err)
	}
	if matched, err := pred.Matches(obj); err == nil && matched {
		v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
	}
	return nil
}

func updateState(versioner apistorage.Versioner, st *objState, userUpdate apistorage.UpdateFunc) (runtime.Object, uint64, error) {
	ret, ttlPtr, err := userUpdate(st.obj, *st.meta)
	if err != nil {
		return nil, 0, err
	}

	if err := versioner.PrepareObjectForStorage(ret); err != nil {
		return nil, 0, fmt.Errorf("PrepareObjectForStorage failed: %v", err)
	}
	var ttl uint64
	if ttlPtr != nil {
		ttl = *ttlPtr
	}
	return ret, ttl, nil
} // parseFrom transforms an encoded predicate from into a versioned struct.

// TODO: return a typed error that instructs clients that they must relist
func decodeContinue(continueValue, keyPrefix string) (fromKey string, rv int64, err error) {
	data, err := base64.RawURLEncoding.DecodeString(continueValue)
	if err != nil {
		return "", 0, fmt.Errorf("continue key is not valid: %v", err)
	}
	var c continueToken
	if err := json.Unmarshal(data, &c); err != nil {
		return "", 0, fmt.Errorf("continue key is not valid: %v", err)
	}
	switch c.APIVersion {
	case "meta.k8s.io/v1":
		if c.ResourceVersion == 0 {
			return "", 0, fmt.Errorf("continue key is not valid: incorrect encoded start resourceVersion (version meta.k8s.io/v1)")
		}
		if len(c.StartKey) == 0 {
			return "", 0, fmt.Errorf("continue key is not valid: encoded start key empty (version meta.k8s.io/v1)")
		}
		// defend against path traversal attacks by clients - path.Clean will ensure that startKey cannot
		// be at a higher level of the hierarchy, and so when we append the key prefix we will end up with
		// continue start key that is fully qualified and cannot range over anything less specific than
		// keyPrefix.
		key := c.StartKey
		if !strings.HasPrefix(key, "/") {
			key = "/" + key
		}
		cleaned := path.Clean(key)
		if cleaned != key {
			return "", 0, fmt.Errorf("continue key is not valid: %s", c.StartKey)
		}
		return keyPrefix + cleaned[1:], c.ResourceVersion, nil
	default:
		return "", 0, fmt.Errorf("continue key is not valid: server does not recognize this encoded version %q", c.APIVersion)
	}
}

// encodeContinue returns a string representing the encoded continuation of the current query.
func encodeContinue(key, keyPrefix string, resourceVersion uint64) (string, error) {
	nextKey := strings.TrimPrefix(key, keyPrefix)
	if nextKey == key {
		return "", fmt.Errorf("unable to encode next field: the key and key prefix do not match")
	}
	out, err := json.Marshal(&continueToken{APIVersion: "meta.k8s.io/v1", ResourceVersion: int64(resourceVersion), StartKey: nextKey})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(out), nil
}
