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

// Most of code is a COPY from k8s.io/apiserver/pkg/storage/etcd3/api_object_versioner.go
package store

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	apistorage "k8s.io/apiserver/pkg/storage"
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

// AppendListItem decodes and appends the object (if it passes filter) to v, which must be a slice.
func AppendListItem(v reflect.Value, obj runtime.Object, rev uint64, pred apistorage.SelectionPredicate) error {
	// being unable to set the version does not prevent the object from being extracted
	if err := SetObjectResourceVersion(obj, rev); err != nil {
		return err
	}
	if matched, err := pred.Matches(obj); err == nil && matched {
		v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
	}
	return nil
}

func UpdateState(existintObj runtime.Object, userUpdate apistorage.UpdateFunc) (runtime.Object, uint64, error) {
	obj := existintObj.DeepCopyObject() // deep copy to avoid obj changed by other routine, state should be a snapshot
	meta := apistorage.ResponseMeta{}

	rv, err := GetObjectResourceVersion(obj)
	if err != nil {
		return nil, 0, fmt.Errorf("couldn't get resource version: %v", err)
	}
	meta.ResourceVersion = uint64(rv)

	ret, ttlPtr, err := userUpdate(obj, meta)
	if err != nil {
		return nil, 0, err
	}

	if err := PrepareObjectForStorage(ret); err != nil {
		return nil, 0, fmt.Errorf("PrepareObjectForStorage failed: %v", err)
	}
	var ttl uint64
	if ttlPtr != nil {
		ttl = *ttlPtr
	}
	return ret, ttl, nil
}

// TODO: return a typed error that instructs clients that they must relist
func DecodeContinue(continueValue, keyPrefix string) (fromKey string, rv int64, err error) {
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
func EncodeContinue(key, keyPrefix string, resourceVersion uint64) (string, error) {
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

func HasDeletionTimestamp(obj runtime.Object) bool {
	objMeta, err := meta.Accessor(obj)
	if err != nil {
		return false
	}
	if objMeta.GetDeletionTimestamp() != nil {
		return true
	}
	return false
}

func ShouldDeleteSpec(obj runtime.Object) bool {
	objMeta, err := meta.Accessor(obj)
	if err != nil {
		return false
	}
	if len(objMeta.GetFinalizers()) > 0 {
		return false
	}
	if objMeta.GetDeletionTimestamp() == nil {
		return false
	}
	return objMeta.GetDeletionGracePeriodSeconds() == nil || *objMeta.GetDeletionGracePeriodSeconds() == 0
}

func GetTryUpdateFunc(updating runtime.Object) apistorage.UpdateFunc {
	return func(existing runtime.Object, res apistorage.ResponseMeta) (runtime.Object, *uint64, error) {
		existingVersion, err := GetObjectResourceVersion(existing)
		if err != nil {
			return nil, nil, err
		}
		updatingVersion, err := GetObjectResourceVersion(updating)
		if existingVersion != updatingVersion {
			return nil, nil, fmt.Errorf("object is already updated to a newer version, get it and update again")
		}
		return updating.DeepCopyObject(), nil, nil
	}
}
