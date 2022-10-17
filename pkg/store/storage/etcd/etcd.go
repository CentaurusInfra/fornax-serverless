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

package etcd3

import (
	"context"
	"fmt"
	"path"

	"centaurusinfra.io/fornax-serverless/pkg/store/storage"
	etcd "go.etcd.io/etcd/client/v3"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/storage/value"
	"k8s.io/klog/v2"
)

type etcdStore struct {
	ctx                 context.Context
	client              *etcd.Client
	codec               runtime.Codec
	transformer         value.Transformer
	pathPrefix          string
	groupResource       schema.GroupResource
	groupResourceString string
}

// DelObject implements store.Store
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
		etcd.Compare(etcd.ModRevision(key), "=", rev),
	).Then(
		etcd.OpDelete(key),
	).Else(
		etcd.OpGet(key),
	).Commit()
	if err != nil {
		return err
	}

	if !txnResp.Succeeded {
		return fmt.Errorf("Deletion of %s failed because of a revision conflict, please get a new revision and retry", key)
	}
	return nil
}

// GetObject implements store.Store
func (s *etcdStore) GetObject(key string) (interface{}, error) {
	getResp, err := s.client.KV.Get(s.ctx, key)
	if err != nil {
		return nil, err
	}
	if len(getResp.Kvs) == 0 {
		return nil, nil
	}

	kv := getResp.Kvs[0]
	data := []byte{}
	res := decode(s.codec, data, out, kv.ModRevision)
	klog.InfoS("GWJ get a obj", "key", key, "output", out)
	return res
}

func (*etcdStore) ListObject() ([]interface{}, error) {
	panic("unimplemented")
}

// PutObject implements store.Store
func (s *etcdStore) PutObject(key string, obj interface{}, rev int64) error {
	klog.InfoS("GWJ create a obj", "key", key, "input", obj)
	data, err := runtime.Encode(s.codec, obj)
	if err != nil {
		return err
	}

	txnResp, err := s.client.KV.Txn(s.ctx).If(
		notFound(key),
	).Then(
		etcd.OpPut(key, string(data)),
	).Else(
		etcd.OpGet(key),
	).Commit()
	if err != nil {
		return err
	}

	if !txnResp.Succeeded {
		txnResp, err = s.client.KV.Txn(s.ctx).If(
			etcd.Compare(etcd.ModRevision(key), "=", rev),
		).Then(
			etcd.OpPut(key, string(data)),
		).Else(
			etcd.OpGet(key),
		).Commit()
		if err != nil {
			return err
		}
		if !txnResp.Succeeded {
			return fmt.Errorf("Update of %s failed because of a revision conflict, please get a new revision and retry", key)
		}
	}

	if out != nil {
		putResp := txnResp.Responses[0].GetResponsePut()
		res := decode(s.codec, data, out, putResp.Header.Revision)
		klog.InfoS("GWJ create a obj", "key", key, "output", out)
		return res
	}
}

func NewEtcdStore(c *etcd.Client, prefix string, groupResource schema.GroupResource) *etcdStore {
	result := &etcdStore{
		client:              c,
		pathPrefix:          path.Join("/", prefix),
		groupResource:       groupResource,
		groupResourceString: groupResource.String(),
	}
	return result
}

var _ storage.Store = &etcdStore{}

// // GuaranteedUpdate implements storage.Interface.GuaranteedUpdate.
// func (s *etcdStore) GuaranteedUpdate(
//  ctx context.Context, key string, out runtime.Object, ignoreNotFound bool,
//  preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc, cachedExistingObject runtime.Object) error {
//  trace := utiltrace.New("GuaranteedUpdate etcd3", utiltrace.Field{"type", getTypeName(out)})
//  defer trace.LogIfLong(500 * time.Millisecond)
//
//  klog.InfoS("GWJ update obj", "key", key, "obj", cachedExistingObject)
//  v, err := conversion.EnforcePtr(out)
//  if err != nil {
//    return fmt.Errorf("unable to convert output object to pointer: %v", err)
//  }
//  key = path.Join(s.pathPrefix, key)
//
//  getCurrentState := func() (*objState, error) {
//    startTime := time.Now()
//    getResp, err := s.client.KV.Get(ctx, key)
//    metrics.RecordEtcdRequestLatency("get", getTypeName(out), startTime)
//    if err != nil {
//      return nil, err
//    }
//    return s.getState(ctx, getResp, key, v, ignoreNotFound)
//  }
//
//  var origState *objState
//  var origStateIsCurrent bool
//  if cachedExistingObject != nil {
//    origState, err = s.getStateFromObject(cachedExistingObject)
//  } else {
//    origState, err = getCurrentState()
//    origStateIsCurrent = true
//  }
//  if err != nil {
//    return err
//  }
//  trace.Step("initial value restored")
//
//  transformContext := authenticatedDataString(key)
//  for {
//    if err := preconditions.Check(key, origState.obj); err != nil {
//      // If our data is already up to date, return the error
//      if origStateIsCurrent {
//        return err
//      }
//
//      // It's possible we were working with stale data
//      // Actually fetch
//      origState, err = getCurrentState()
//      if err != nil {
//        return err
//      }
//      origStateIsCurrent = true
//      // Retry
//      continue
//    }
//
//    ret, ttl, err := s.updateState(origState, tryUpdate)
//    if err != nil {
//      // If our data is already up to date, return the error
//      if origStateIsCurrent {
//        return err
//      }
//
//      // It's possible we were working with stale data
//      // Remember the revision of the potentially stale data and the resulting update error
//      cachedRev := origState.rev
//      cachedUpdateErr := err
//
//      // Actually fetch
//      origState, err = getCurrentState()
//      if err != nil {
//        return err
//      }
//      origStateIsCurrent = true
//
//      // it turns out our cached data was not stale, return the error
//      if cachedRev == origState.rev {
//        return cachedUpdateErr
//      }
//
//      // Retry
//      continue
//    }
//    klog.InfoS("GWJ update obj return", "key", key, "ret", ret)
//
//    data, err := runtime.Encode(s.codec, ret)
//    if err != nil {
//      return err
//    }
//
//    newData, err := s.transformer.TransformToStorage(ctx, data, transformContext)
//    if err != nil {
//      return storage.NewInternalError(err.Error())
//    }
//
//    startTime := time.Now()
//    txnResp, err := s.client.KV.Txn(ctx).If(
//      clientv3.Compare(clientv3.ModRevision(key), "=", origState.rev),
//    ).Then(
//      clientv3.OpPut(key, string(newData), opts...),
//    ).Else(
//      clientv3.OpGet(key),
//    ).Commit()
//    metrics.RecordEtcdRequestLatency("update", getTypeName(out), startTime)
//    if err != nil {
//      return err
//    }
//    trace.Step("Transaction committed")
//    if !txnResp.Succeeded {
//      getResp := (*clientv3.GetResponse)(txnResp.Responses[0].GetResponseRange())
//      klog.V(4).Infof("GuaranteedUpdate of %s failed because of a conflict, going to retry", key)
//      origState, err = s.getState(ctx, getResp, key, v, ignoreNotFound)
//      if err != nil {
//        return err
//      }
//      trace.Step("Retry value restored")
//      origStateIsCurrent = true
//      continue
//    }
//    putResp := txnResp.Responses[0].GetResponsePut()
//
//    return decode(s.codec, data, out, putResp.Header.Revision)
//  }
// }
//
// func getNewItemFunc(listObj runtime.Object, v reflect.Value) func() runtime.Object {
//  // For unstructured lists with a target group/version, preserve the group/version in the instantiated list items
//  if unstructuredList, isUnstructured := listObj.(*unstructured.UnstructuredList); isUnstructured {
//    if apiVersion := unstructuredList.GetAPIVersion(); len(apiVersion) > 0 {
//      return func() runtime.Object {
//        return &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": apiVersion}}
//      }
//    }
//  }
//
//  // Otherwise just instantiate an empty item
//  elem := v.Type().Elem()
//  return func() runtime.Object {
//    return reflect.New(elem).Interface().(runtime.Object)
//  }
// }
//
// func (s *etcdStore) Count(key string) (int64, error) {
//  key = path.Join(s.pathPrefix, key)
//
//  // We need to make sure the key ended with "/" so that we only get children "directories".
//  // e.g. if we have key "/a", "/a/b", "/ab", getting keys with prefix "/a" will return all three,
//  // while with prefix "/a/" will return only "/a/b" which is the correct answer.
//  if !strings.HasSuffix(key, "/") {
//    key += "/"
//  }
//
//  startTime := time.Now()
//  getResp, err := s.client.KV.Get(context.Background(), key, clientv3.WithRange(clientv3.GetPrefixRangeEnd(key)), clientv3.WithCountOnly())
//  metrics.RecordEtcdRequestLatency("listWithCount", key, startTime)
//  if err != nil {
//    return 0, err
//  }
//  klog.InfoS("GWJ count list of obj", "key", key, "count", getResp.Count)
//  return getResp.Count, nil
// }
//
// // continueToken is a simple structured object for encoding the state of a continue token.
// // TODO: if we change the version of the encoded from, we can't start encoding the new version
// // until all other servers are upgraded (i.e. we need to support rolling schema)
// // This is a public API struct and cannot change.
// type continueToken struct {
//  APIVersion      string `json:"v"`
//  ResourceVersion int64  `json:"rv"`
//  StartKey        string `json:"start"`
// }
//
// // parseFrom transforms an encoded predicate from into a versioned struct.
// // TODO: return a typed error that instructs clients that they must relist
// func decodeContinue(continueValue, keyPrefix string) (fromKey string, rv int64, err error) {
//  data, err := base64.RawURLEncoding.DecodeString(continueValue)
//  if err != nil {
//    return "", 0, fmt.Errorf("continue key is not valid: %v", err)
//  }
//  var c continueToken
//  if err := json.Unmarshal(data, &c); err != nil {
//    return "", 0, fmt.Errorf("continue key is not valid: %v", err)
//  }
//  switch c.APIVersion {
//  case "meta.k8s.io/v1":
//    if c.ResourceVersion == 0 {
//      return "", 0, fmt.Errorf("continue key is not valid: incorrect encoded start resourceVersion (version meta.k8s.io/v1)")
//    }
//    if len(c.StartKey) == 0 {
//      return "", 0, fmt.Errorf("continue key is not valid: encoded start key empty (version meta.k8s.io/v1)")
//    }
//    // defend against path traversal attacks by clients - path.Clean will ensure that startKey cannot
//    // be at a higher level of the hierarchy, and so when we append the key prefix we will end up with
//    // continue start key that is fully qualified and cannot range over anything less specific than
//    // keyPrefix.
//    key := c.StartKey
//    if !strings.HasPrefix(key, "/") {
//      key = "/" + key
//    }
//    cleaned := path.Clean(key)
//    if cleaned != key {
//      return "", 0, fmt.Errorf("continue key is not valid: %s", c.StartKey)
//    }
//    return keyPrefix + cleaned[1:], c.ResourceVersion, nil
//  default:
//    return "", 0, fmt.Errorf("continue key is not valid: server does not recognize this encoded version %q", c.APIVersion)
//  }
// }
//
// // encodeContinue returns a string representing the encoded continuation of the current query.
// func encodeContinue(key, keyPrefix string, resourceVersion int64) (string, error) {
//  nextKey := strings.TrimPrefix(key, keyPrefix)
//  if nextKey == key {
//    return "", fmt.Errorf("unable to encode next field: the key and key prefix do not match")
//  }
//  out, err := json.Marshal(&continueToken{APIVersion: "meta.k8s.io/v1", ResourceVersion: resourceVersion, StartKey: nextKey})
//  if err != nil {
//    return "", err
//  }
//  return base64.RawURLEncoding.EncodeToString(out), nil
// }
//
// // GetList implements storage.Interface.
// func (s *etcdStore) GetList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
//  recursive := opts.Recursive
//  resourceVersion := opts.ResourceVersion
//  match := opts.ResourceVersionMatch
//  pred := opts.Predicate
//  trace := utiltrace.New(fmt.Sprintf("List(recursive=%v) etcd3", recursive),
//    utiltrace.Field{"key", key},
//    utiltrace.Field{"resourceVersion", resourceVersion},
//    utiltrace.Field{"resourceVersionMatch", match},
//    utiltrace.Field{"limit", pred.Limit},
//    utiltrace.Field{"continue", pred.Continue})
//  defer trace.LogIfLong(500 * time.Millisecond)
//  klog.InfoS("GWJ get list of obj", "key", key, "opts", opts)
//  listPtr, err := meta.GetItemsPtr(listObj)
//  if err != nil {
//    return err
//  }
//  v, err := conversion.EnforcePtr(listPtr)
//  if err != nil || v.Kind() != reflect.Slice {
//    return fmt.Errorf("need ptr to slice: %v", err)
//  }
//  key = path.Join(s.pathPrefix, key)
//
//  // For recursive lists, we need to make sure the key ended with "/" so that we only
//  // get children "directories". e.g. if we have key "/a", "/a/b", "/ab", getting keys
//  // with prefix "/a" will return all three, while with prefix "/a/" will return only
//  // "/a/b" which is the correct answer.
//  if recursive && !strings.HasSuffix(key, "/") {
//    key += "/"
//  }
//  keyPrefix := key
//
//  // set the appropriate clientv3 options to filter the returned data set
//  var limitOption *clientv3.OpOption
//  var limit int64 = pred.Limit
//  options := make([]clientv3.OpOption, 0, 4)
//  if pred.Limit > 0 {
//    options = append(options, clientv3.WithLimit(limit))
//    limitOption = &options[len(options)-1]
//  }
//
//  newItemFunc := getNewItemFunc(listObj, v)
//
//  var fromRV *uint64
//
//  var returnedRV, continueRV, withRev int64
//  var continueKey string
//  switch {
//  case recursive && len(pred.Continue) > 0:
//    continueKey, continueRV, err = decodeContinue(pred.Continue, keyPrefix)
//    if err != nil {
//      return apierrors.NewBadRequest(fmt.Sprintf("invalid continue token: %v", err))
//    }
//
//    if len(resourceVersion) > 0 && resourceVersion != "0" {
//      return apierrors.NewBadRequest("specifying resource version is not allowed when using continue")
//    }
//
//    rangeEnd := clientv3.GetPrefixRangeEnd(keyPrefix)
//    options = append(options, clientv3.WithRange(rangeEnd))
//    key = continueKey
//
//    // If continueRV > 0, the LIST request needs a specific resource version.
//    // continueRV==0 is invalid.
//    // If continueRV < 0, the request is for the latest resource version.
//    if continueRV > 0 {
//      withRev = continueRV
//      returnedRV = continueRV
//    }
//  case recursive && pred.Limit > 0:
//    if fromRV != nil {
//      switch match {
//      case metav1.ResourceVersionMatchNotOlderThan:
//        // The not older than constraint is checked after we get a response from etcd,
//        // and returnedRV is then set to the revision we get from the etcd response.
//      case metav1.ResourceVersionMatchExact:
//        returnedRV = int64(*fromRV)
//        withRev = returnedRV
//      case "": // legacy case
//        if *fromRV > 0 {
//          returnedRV = int64(*fromRV)
//          withRev = returnedRV
//        }
//      default:
//        return fmt.Errorf("unknown ResourceVersionMatch value: %v", match)
//      }
//    }
//
//    rangeEnd := clientv3.GetPrefixRangeEnd(keyPrefix)
//    options = append(options, clientv3.WithRange(rangeEnd))
//  default:
//    if fromRV != nil {
//      switch match {
//      case metav1.ResourceVersionMatchNotOlderThan:
//        // The not older than constraint is checked after we get a response from etcd,
//        // and returnedRV is then set to the revision we get from the etcd response.
//      case metav1.ResourceVersionMatchExact:
//        returnedRV = int64(*fromRV)
//        withRev = returnedRV
//      case "": // legacy case
//      default:
//        return fmt.Errorf("unknown ResourceVersionMatch value: %v", match)
//      }
//    }
//
//    if recursive {
//      options = append(options, clientv3.WithPrefix())
//    }
//  }
//  if withRev != 0 {
//    options = append(options, clientv3.WithRev(withRev))
//  }
//
//  klog.InfoS("GWJ get list of obj with", "key", key, "rv", withRev)
//  // loop until we have filled the requested limit from etcd or there are no more results
//  var lastKey []byte
//  var hasMore bool
//  var getResp *clientv3.GetResponse
//  var numFetched int
//  var numEvald int
//  // Because these metrics are for understanding the costs of handling LIST requests,
//  // get them recorded even in error cases.
//  defer func() {
//    numReturn := v.Len()
//    metrics.RecordStorageListMetrics(s.groupResourceString, numFetched, numEvald, numReturn)
//  }()
//  for {
//    startTime := time.Now()
//    getResp, err = s.client.KV.Get(ctx, key, options...)
//    if recursive {
//      metrics.RecordEtcdRequestLatency("list", getTypeName(listPtr), startTime)
//    } else {
//      metrics.RecordEtcdRequestLatency("get", getTypeName(listPtr), startTime)
//    }
//    if err != nil {
//      return interpretListError(err, len(pred.Continue) > 0, continueKey, keyPrefix)
//    }
//    numFetched += len(getResp.Kvs)
//    hasMore = getResp.More
//
//    if len(getResp.Kvs) == 0 && getResp.More {
//      return fmt.Errorf("no results were found, but etcd indicated there were more values remaining")
//    }
//
//    // avoid small allocations for the result slice, since this can be called in many
//    // different contexts and we don't know how significantly the result will be filtered
//    if pred.Empty() {
//      growSlice(v, len(getResp.Kvs))
//    } else {
//      growSlice(v, 2048, len(getResp.Kvs))
//    }
//
//    // take items from the response until the bucket is full, filtering as we go
//    for i, kv := range getResp.Kvs {
//      if limitOption != nil && int64(v.Len()) >= pred.Limit {
//        hasMore = true
//        break
//      }
//      lastKey = kv.Key
//
//      data, _, err := s.transformer.TransformFromStorage(ctx, kv.Value, authenticatedDataString(kv.Key))
//      if err != nil {
//        return storage.NewInternalErrorf("unable to transform key %q: %v", kv.Key, err)
//      }
//
//      if err := appendListItem(v, data, uint64(kv.ModRevision), pred, s.codec, newItemFunc); err != nil {
//        return err
//      }
//      numEvald++
//
//      // free kv early. Long lists can take O(seconds) to decode.
//      getResp.Kvs[i] = nil
//    }
//
//    // indicate to the client which resource version was returned
//    if returnedRV == 0 {
//      returnedRV = getResp.Header.Revision
//    }
//
//    // no more results remain or we didn't request paging
//    if !hasMore || limitOption == nil {
//      break
//    }
//    // we're paging but we have filled our bucket
//    if int64(v.Len()) >= pred.Limit {
//      break
//    }
//
//    if limit < maxLimit {
//      // We got incomplete result due to field/label selector dropping the object.
//      // Double page size to reduce total number of calls to etcd.
//      limit *= 2
//      if limit > maxLimit {
//        limit = maxLimit
//      }
//      *limitOption = clientv3.WithLimit(limit)
//    }
//    key = string(lastKey) + "\x00"
//    if withRev == 0 {
//      withRev = returnedRV
//      options = append(options, clientv3.WithRev(withRev))
//    }
//  }
//
//  klog.InfoS("GWJ get list of obj return", "key", key, "rv", returnedRV, "lastKey", lastKey, "hasMore", hasMore, "len", v.Len())
//  // instruct the client to begin querying from immediately after the last key we returned
//  // we never return a key that the client wouldn't be allowed to see
//  if hasMore {
//    // we want to start immediately after the last key
//    next, err := encodeContinue(string(lastKey)+"\x00", keyPrefix, returnedRV)
//    if err != nil {
//      return err
//    }
//    var remainingItemCount *int64
//    // getResp.Count counts in objects that do not match the pred.
//    // Instead of returning inaccurate count for non-empty selectors, we return nil.
//    // Only set remainingItemCount if the predicate is empty.
//    if utilfeature.DefaultFeatureGate.Enabled(features.RemainingItemCount) {
//      if pred.Empty() {
//        c := int64(getResp.Count - pred.Limit)
//        remainingItemCount = &c
//      }
//    }
//    return s.versioner.UpdateList(listObj, uint64(returnedRV), next, remainingItemCount)
//  }
//
//  // no continuation
//  return s.versioner.UpdateList(listObj, uint64(returnedRV), "", nil)
// }
//
// // growSlice takes a slice value and grows its capacity up
// // to the maximum of the passed sizes or maxCapacity, whichever
// // is smaller. Above maxCapacity decisions about allocation are left
// // to the Go runtime on append. This allows a caller to make an
// // educated guess about the potential size of the total list while
// // still avoiding overly aggressive initial allocation. If sizes
// // is empty maxCapacity will be used as the size to grow.
// func growSlice(v reflect.Value, maxCapacity int, sizes ...int) {
//  cap := v.Cap()
//  max := cap
//  for _, size := range sizes {
//    if size > max {
//      max = size
//    }
//  }
//  if len(sizes) == 0 || max > maxCapacity {
//    max = maxCapacity
//  }
//  if max <= cap {
//    return
//  }
//  if v.Len() > 0 {
//    extra := reflect.MakeSlice(v.Type(), 0, max)
//    reflect.Copy(extra, v)
//    v.Set(extra)
//  } else {
//    extra := reflect.MakeSlice(v.Type(), 0, max)
//    v.Set(extra)
//  }
// }
//
// func (s *etcdStore) getState(ctx context.Context, getResp *clientv3.GetResponse, key string, v reflect.Value, ignoreNotFound bool) (*objState, error) {
//  state := &objState{
//    meta: &storage.ResponseMeta{},
//  }
//
//  if u, ok := v.Addr().Interface().(runtime.Unstructured); ok {
//    state.obj = u.NewEmptyInstance()
//  } else {
//    state.obj = reflect.New(v.Type()).Interface().(runtime.Object)
//  }
//
//  if len(getResp.Kvs) == 0 {
//    if !ignoreNotFound {
//      return nil, storage.NewKeyNotFoundError(key, 0)
//    }
//    if err := runtime.SetZeroValue(state.obj); err != nil {
//      return nil, err
//    }
//  } else {
//    data, stale, err := s.transformer.TransformFromStorage(ctx, getResp.Kvs[0].Value, authenticatedDataString(key))
//    if err != nil {
//      return nil, storage.NewInternalError(err.Error())
//    }
//    state.rev = getResp.Kvs[0].ModRevision
//    state.meta.ResourceVersion = uint64(state.rev)
//    state.data = data
//    state.stale = stale
//    if err := decode(s.codec, state.data, state.obj, state.rev); err != nil {
//      return nil, err
//    }
//  }
//  return state, nil
// }
//
// // decode decodes value of bytes into object. It will also set the object resource version to rev.
// // On success, objPtr would be set to the object.
// func decode(codec runtime.Codec, value []byte, objPtr runtime.Object, rev int64) error {
//  if _, err := conversion.EnforcePtr(objPtr); err != nil {
//    return fmt.Errorf("unable to convert output object to pointer: %v", err)
//  }
//  _, _, err := codec.Decode(value, nil, objPtr)
//  if err != nil {
//    return err
//  }
//  return nil
// }
//
// // appendListItem decodes and appends the object (if it passes filter) to v, which must be a slice.
// func appendListItem(v reflect.Value, data []byte, rev uint64, pred storage.SelectionPredicate, codec runtime.Codec, versioner storage.Versioner, newItemFunc func() runtime.Object) error {
//  obj, _, err := codec.Decode(data, nil, newItemFunc())
//  if err != nil {
//    return err
//  }
//  // being unable to set the version does not prevent the object from being extracted
//  if err := versioner.UpdateObject(obj, rev); err != nil {
//    klog.Errorf("failed to update object version: %v", err)
//  }
//  if matched, err := pred.Matches(obj); err == nil && matched {
//    v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
//  }
//  return nil
// }
//

func notFound(key string) etcd.Cmp {
	return etcd.Compare(etcd.ModRevision(key), "=", 0)
}

//
// // getTypeName returns type name of an object for reporting purposes.
// func getTypeName(obj interface{}) string {
//  return reflect.TypeOf(obj).String()
// }
