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

package cache

import (
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sync"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"

	"github.com/soda-cdm/kahu/utils/k8sresource"
)

const (
	resourceGVKIndex = "resource.gvk.index.kahu.io"
)

type store struct {
	sync.RWMutex
	cache   cache.ThreadSafeStore
	keyFunc cache.KeyFunc
	logger  log.FieldLogger
}

var errNotResourceObject = fmt.Errorf("object does not implement the Resource interface")

func NewCache() Interface {
	return &store{
		cache: cache.NewThreadSafeStore(cacheResourceIndexers(), cache.Indices{}),
		//relations: make(map[string]sets.String),
		keyFunc: uidKeyFunc,
		logger:  log.WithField("module", "resource-cache"),
	}
}

func uidKeyFunc(obj interface{}) (string, error) {
	if resource, ok := obj.(k8sresource.Resource); ok {
		return string(resource.GetUID()), nil
	}

	return "", errNotResourceObject
}

func cacheResourceIndexers() cache.Indexers {
	return cache.Indexers{
		resourceGVKIndex: func(obj interface{}) ([]string, error) {
			keys := make([]string, 0)
			var resource k8sresource.Resource
			switch t := obj.(type) {
			case k8sresource.Resource:
				resource = t
			default:
				return keys, nil
			}

			keys = append(keys, resource.GetObjectKind().GroupVersionKind().String())
			return keys, nil
		},
	}
}

// Add adds the given object to store
func (s *store) Add(obj k8sresource.Resource) error {
	s.Lock()
	defer s.Unlock()

	key, err := s.keyFunc(obj)
	if err != nil {
		return err
	}
	//_, ok := s.relations[key]
	//if ok {
	//	return fmt.Errorf("object already exist")
	//}
	//s.relations[key] = sets.NewString()
	s.cache.Add(key, obj)
	return nil
}

// Update updates the given object in store
func (s *store) Update(obj k8sresource.Resource) error {
	s.Lock()
	defer s.Unlock()

	key, err := s.keyFunc(obj)
	if err != nil {
		return err
	}
	s.cache.Update(key, obj)
	return nil
}

// Delete deletes the given object from store
func (s *store) Delete(obj k8sresource.Resource) error {
	s.Lock()
	defer s.Unlock()

	key, err := s.keyFunc(obj)
	if err != nil {
		return err
	}
	//delete(s.relations, key)
	s.cache.Delete(key)
	return nil
}

// List returns a list of all the currently non-empty objects
func (s *store) List() []k8sresource.Resource {
	s.RLock()
	defer s.RUnlock()

	var resources []k8sresource.Resource
	for _, obj := range s.cache.List() {
		switch t := obj.(type) {
		case k8sresource.Resource:
			resources = append(resources, t)
		}
	}

	return resources
}

// ListKeys returns a list of all the keys currently associated with objects
func (s *store) ListKeys() []string {
	s.RLock()
	defer s.RUnlock()
	return s.cache.ListKeys()
}

// Get returns the object
func (s *store) Get(obj interface{}) (item k8sresource.Resource, exists bool, err error) {
	s.RLock()
	defer s.RUnlock()

	key, err := s.keyFunc(obj)
	if err != nil {
		return k8sresource.Resource{}, false, err
	}

	obj, exists = s.cache.Get(key)
	switch t := obj.(type) {
	case k8sresource.Resource:
		return t, exists, nil
	default:
		return k8sresource.Resource{}, false, errNotResourceObject
	}
}

// GetByKey returns the object
func (s *store) GetByKey(key string) (k8sresource.Resource, bool, error) {
	s.RLock()
	defer s.RUnlock()
	obj, exists := s.cache.Get(key)
	switch t := obj.(type) {
	case k8sresource.Resource:
		return t, exists, nil
	default:
		return k8sresource.Resource{}, false, errNotResourceObject
	}
}

// AddChildren adds parent child relationship between objects
func (s *store) AddChildren(_ k8sresource.Resource, children ...k8sresource.Resource) error {
	s.Lock()
	defer s.Unlock()

	for _, child := range children {
		childKey, err := s.keyFunc(child)
		if err != nil {
			return err
		}
		_, ok := s.cache.Get(childKey)
		if !ok {
			s.cache.Add(childKey, child)
		}
	}
	return nil
}

// GetByGVK returns the objects
func (s *store) GetByGVK(gvk schema.GroupVersionKind) ([]k8sresource.Resource, error) {
	s.RLock()
	defer s.RUnlock()
	objs, err := s.cache.ByIndex(resourceGVKIndex, gvk.String())
	if err != nil {
		return nil, err
	}

	resources := make([]k8sresource.Resource, 0)
	for _, obj := range objs {
		switch t := obj.(type) {
		case k8sresource.Resource:
			resources = append(resources, t)
		default:
			return nil, errNotResourceObject
		}
	}
	return resources, nil
}

func (s *store) GetKey(obj k8sresource.Resource) (string, error) {
	return s.keyFunc(obj)
}

func (s *store) DeepCopy() Interface {
	newCache := NewCache()
	for _, resource := range s.List() {
		err := newCache.Add(k8sresource.Resource{
			Unstructured: unstructured.Unstructured{
				Object: resource.DeepCopy().Object,
			}})
		if err != nil {
			s.logger.Warningf("Failed to add resource[%s/%s] with deep copy", resource.GetNamespace(),
				resource.GetName())
		}
	}
	return newCache
}
