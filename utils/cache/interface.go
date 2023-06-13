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
	"github.com/soda-cdm/kahu/utils/k8sresource"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Interface interface {
	// Add adds the given object to store
	Add(obj k8sresource.Resource) error

	// Update updates the given object in store
	Update(obj k8sresource.Resource) error

	// Delete deletes the given object from store
	Delete(obj k8sresource.Resource) error

	// List returns a list of all the currently non-empty objects
	List() []k8sresource.Resource

	// ListKeys returns a list of all the keys currently associated with objects
	ListKeys() []string

	// Get returns the object
	Get(obj interface{}) (item k8sresource.Resource, exists bool, err error)

	// GetByKey returns the object
	GetByKey(key string) (item k8sresource.Resource, exists bool, err error)

	// AddChildren adds parent child relationship between objects
	AddChildren(obj k8sresource.Resource, children ...k8sresource.Resource) error

	// GetKey returns related object's key
	GetKey(obj k8sresource.Resource) (string, error)

	GetByGVK(gvk schema.GroupVersionKind) ([]k8sresource.Resource, error)

	DeepCopy() Interface
}
