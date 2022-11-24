/*
Copyright 2022 The SODA Authors.

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

package utils

import (
	"fmt"
	"reflect"
	"strconv"
	"sync"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
)

type Store interface {

	// Add adds the given object to the accumulator associated with the given object's key
	Add(obj interface{}, value interface{}) error

	// Update updates the given object in the accumulator associated with the given object's key
	Update(obj interface{}, value interface{}) error

	// Delete deletes the given object from the accumulator associated with the given object's key
	Delete(obj interface{}) error

	// Get returns the accumulator associated with the given object's key
	Get(obj interface{}) (item interface{}, exists bool, err error)

	// List returns a list of all the currently non-empty accumulators
	List() []interface{}

	// ListKeys returns a list of all the keys currently associated with non-empty accumulators
	ListKeys() []string
}

// KeyFunc knows how to make a key from an object. Implementations should be deterministic.
type KeyFunc func(obj interface{}) (string, error)

// StoreError will be returned any time a Store gives an error; it includes the object
// at fault.
type StoreError struct {
	Obj interface{}
	Err error
}

// Error gives a human-readable description of the error.
func (k StoreError) Error() string {
	return fmt.Sprintf("couldn't store key for object %+v: %v", k.Obj, k.Err)
}

type threadSafeStore struct {
	items map[string]interface{}

	lock sync.RWMutex

	keyFunc KeyFunc
}

func NewStore(keyFunc KeyFunc) Store {
	return &threadSafeStore{
		items:   make(map[string]interface{}),
		keyFunc: keyFunc,
	}
}

// Add adds the given object to the accumulator associated with the given object's key
func (store *threadSafeStore) Add(obj interface{}, value interface{}) error {
	store.lock.Lock()
	defer store.lock.Unlock()
	key, err := store.keyFunc(obj)
	if err != nil {
		return StoreError{
			Obj: obj,
			Err: err,
		}
	}

	if _, ok := store.items[key]; ok {
		return StoreError{
			Obj: obj,
			Err: fmt.Errorf("key (%s) already exist", key),
		}
	}

	store.items[key] = value
	return nil
}

// Update updates the given object in the accumulator associated with the given object's key
func (store *threadSafeStore) Update(obj interface{}, value interface{}) error {
	store.lock.Lock()
	defer store.lock.Unlock()
	key, err := store.keyFunc(obj)
	if err != nil {
		return StoreError{
			Obj: obj,
			Err: err,
		}
	}

	if _, ok := store.items[key]; !ok {
		return StoreError{
			Obj: obj,
			Err: fmt.Errorf("key (%s) donot exist", key),
		}
	}

	store.items[key] = value
	return nil
}

// Delete deletes the given object from the accumulator associated with the given object's key
func (store *threadSafeStore) Delete(obj interface{}) error {
	store.lock.Lock()
	defer store.lock.Unlock()
	key, err := store.keyFunc(obj)
	if err != nil {
		return StoreError{
			Obj: obj,
			Err: err,
		}
	}

	delete(store.items, key)
	return nil
}

// Get returns the accumulator associated with the given object's key
func (store *threadSafeStore) Get(obj interface{}) (item interface{}, exists bool, err error) {
	store.lock.RLock()
	defer store.lock.RUnlock()
	key, err := store.keyFunc(obj)
	if err != nil {
		return nil, false, StoreError{
			Obj: obj,
			Err: err,
		}
	}

	value, ok := store.items[key]

	return value, ok, nil
}

// List returns a list of all the currently non-empty accumulators
func (store *threadSafeStore) List() []interface{} {
	values := make([]interface{}, 0)
	store.lock.RLock()
	defer store.lock.RUnlock()
	for _, val := range store.items {
		values = append(values, val)
	}
	return values
}

// ListKeys returns a list of all the keys currently associated with non-empty accumulators
func (store *threadSafeStore) ListKeys() []string {
	keys := make([]string, 0)
	store.lock.RLock()
	defer store.lock.RUnlock()
	for key := range store.items {
		keys = append(keys, key)
	}
	return keys
}

// StoreRevisionUpdate updates given cache with a new object version from Informer
// callback (i.e. with events from etcd) or with an object modified by the
// controller itself. Returns "true", if the cache was updated, false if the
// object is an old version and should be ignored.
func StoreRevisionUpdate(store Store, obj interface{}, className string) (bool, error) {
	objName, err := DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return false, fmt.Errorf("couldn't get key for object %+v: %w", obj, err)
	}

	oldRevObj, found, err := store.Get(obj)
	if err != nil {
		return false, fmt.Errorf("error finding %s %q in controller cache: %w", className, objName, err)
	}

	objAccessor, err := meta.Accessor(obj)
	if err != nil {
		return false, err
	}
	currentRevision := objAccessor.GetResourceVersion()

	if !found {
		// This is a new object
		log.Debugf("StoreRevisionUpdate: adding %s %q, version %s", className, objName, currentRevision)
		if err = store.Add(obj, currentRevision); err != nil {
			return false, fmt.Errorf("error adding %s %q to controller cache: %w", className, objName, err)
		}
		return true, nil
	}

	oldRevision, err := revisionAccessor(oldRevObj)
	if err != nil {
		return false, err
	}

	objResourceVersion, err := strconv.ParseInt(currentRevision, 10, 64)
	if err != nil {
		return false, fmt.Errorf("error parsing ResourceVersion %q of %s %q: %s", currentRevision, className, objName, err)
	}
	oldObjResourceVersion, err := strconv.ParseInt(oldRevision, 10, 64)
	if err != nil {
		return false, fmt.Errorf("error parsing old ResourceVersion %q of %s %q: %s", oldRevision, className, objName, err)
	}

	// Throw away only older version, let the same version pass - we do want to
	// get periodic sync events.
	if oldObjResourceVersion > objResourceVersion {
		log.Debugf("StoreRevisionUpdate: ignoring %s %q version %q", className, objName, objResourceVersion)
		return false, nil
	}

	log.Debugf("StoreRevisionUpdate updating %s %q with version %q", className, objName, objResourceVersion)
	if err = store.Update(obj, currentRevision); err != nil {
		return false, fmt.Errorf("error updating %s %q in controller cache: %w", className, objName, err)
	}
	return true, nil
}

// StoreClean delete given cache with the object version from Informer
// callback (i.e. with events from etcd) or with an object modified by the
// controller itself. Returns error if face any issue during delete
func StoreClean(store Store, obj interface{}, className string) error {
	objName, err := DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return fmt.Errorf("couldn't get key for object %+v: %w", obj, err)
	}

	_, found, err := store.Get(obj)
	if err != nil {
		return fmt.Errorf("error finding %s %q in cache: %w", className, objName, err)
	}

	if !found {
		return nil
	}

	if err = store.Delete(obj); err != nil {
		return err
	}
	return nil
}

func revisionAccessor(rev interface{}) (string, error) {
	if revision, ok := rev.(string); ok {
		return revision, nil
	}

	return "", fmt.Errorf("invalid revision type (%s)", reflect.TypeOf(rev))
}
