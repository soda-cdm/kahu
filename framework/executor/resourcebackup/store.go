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

package resourcebackup

import (
	"fmt"
	"sync"
)

type StoreCollection struct {
	sync.RWMutex
	store map[string]Service
}

var collectionInit sync.Once
var collection *StoreCollection

func init() {
	collectionInit.Do(func() {
		collection = &StoreCollection{
			store: make(map[string]Service),
		}
	})
}

func NewStore() *StoreCollection {
	return collection
}

func (collection *StoreCollection) Get(location string) (Service, bool) {
	collection.RLock()
	defer collection.RUnlock()
	service, ok := collection.store[location]
	if !ok || service == nil {
		return service, false
	}
	return service, ok
}

func (collection *StoreCollection) Add(location string, svc Service) error {
	collection.Lock()
	defer collection.Unlock()
	service, ok := collection.store[location]
	if ok && service != nil {
		return fmt.Errorf("service alrady available for %s", location)
	}
	collection.store[location] = svc
	return nil
}

func (collection *StoreCollection) Remove(location string) error {
	collection.Lock()
	defer collection.Unlock()
	_, ok := collection.store[location]
	if !ok {
		return nil
	}

	delete(collection.store, location)
	return nil
}

func (collection *StoreCollection) List() []Service {
	collection.Lock()
	defer collection.Unlock()
	services := make([]Service, 0)
	for _, value := range collection.store {
		services = append(services, value)
	}

	return services
}
