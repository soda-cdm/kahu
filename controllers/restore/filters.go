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

package restore

import (
	"fmt"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
)

type filterHandler interface {
	handle(restore *kahuapi.Restore) error
}

func constructFilterHandler(cache cache.Indexer) filterHandler {
	return newNamespaceFilter(cache,
		newResourceFilter(cache,
			newLabelSelectorFilter(cache)))
}

type namespaceFilter struct {
	cache cache.Indexer
	next  filterHandler
}

func newNamespaceFilter(cache cache.Indexer, handler filterHandler) filterHandler {
	return &namespaceFilter{
		cache: cache,
		next:  handler,
	}
}

func (handler *namespaceFilter) handle(restore *kahuapi.Restore) error {
	// perform include/exclude namespace on cache

	excludeNamespaces := sets.NewString()

	// process include namespace
	includeNamespaces := sets.NewString(restore.Spec.IncludeNamespaces...)
	if len(includeNamespaces) != 0 {
		availableNamespaces := handler.cache.ListIndexFuncValues(backupObjectNamespaceIndex)
		for _, namespace := range availableNamespaces {
			// if available namespace are not included exclude them
			if !includeNamespaces.Has(namespace) {
				excludeNamespaces.Insert()
			}
		}
	}

	excludeNamespaces.Insert(restore.Spec.ExcludeNamespaces...)

	// remove objects from cache present in excluded namespaces
	var (
		excludeResourceList = make([]interface{}, 0)
	)
	for _, namespace := range excludeNamespaces.List() {
		resourceList, err := handler.cache.ByIndex(backupObjectNamespaceIndex, namespace)
		if err != nil {
			return fmt.Errorf("failed to retrieve resources from cache for "+
				"namespace resource exclusion. %s", err)
		}
		excludeResourceList = append(excludeResourceList, resourceList...)
	}

	for _, resource := range excludeResourceList {
		err := handler.cache.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resources from cache for "+
				"namespace resource exclusion. %s", err)
		}
	}

	return handler.next.handle(restore)
}

type resourceFilter struct {
	cache cache.Indexer
	next  filterHandler
}

func newResourceFilter(cache cache.Indexer, handler filterHandler) filterHandler {
	return &resourceFilter{
		cache: cache,
		next:  handler,
	}
}

func (handler *resourceFilter) handle(restore *kahuapi.Restore) error {
	// perform include/exclude resources on cache
	// perform include/exclude namespace on cache

	excludeResources := sets.NewString()

	// process include resources
	includeResources := sets.NewString(restore.Spec.IncludeResources...)
	if len(includeResources) != 0 {
		availableResources := handler.cache.ListIndexFuncValues(backupObjectResourceIndex)
		for _, resource := range availableResources {
			// if available resource are not included exclude them
			if !includeResources.Has(resource) {
				excludeResources.Insert()
			}
		}
	}

	excludeResources.Insert(restore.Spec.ExcludeResources...)

	// remove objects from cache present in excluded resources
	var (
		excludeResourceList = make([]interface{}, 0)
	)
	for _, resource := range excludeResources.List() {
		resourceList, err := handler.cache.ByIndex(backupObjectResourceIndex, resource)
		if err != nil {
			return fmt.Errorf("failed to retrieve resources from cache for "+
				"resource exclusion. %s", err)
		}
		excludeResourceList = append(excludeResourceList, resourceList...)
	}

	for _, resource := range excludeResourceList {
		err := handler.cache.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resources from cache for "+
				"resource exclusion. %s", err)
		}
	}
	return handler.next.handle(restore)
}

type labelSelectorFilter struct {
	cache cache.Indexer
}

func newLabelSelectorFilter(cache cache.Indexer) filterHandler {
	return &labelSelectorFilter{
		cache: cache,
	}
}

func (handler *labelSelectorFilter) handle(restore *kahuapi.Restore) error {
	// perform include/exclude resources on cache
	if restore.Spec.LabelSelector != nil {
		for _, resource := range handler.cache.List() {
			unstructure, ok := resource.(*unstructured.Unstructured)
			if !ok {
				return fmt.Errorf("restore index cache with invalid object type %v",
					reflect.TypeOf(resource))
			}
			objectLabels := unstructure.GetLabels()
			selector, err := metav1.LabelSelectorAsSelector(restore.Spec.LabelSelector)
			if err != nil {
				return err
			}
			if !selector.Matches(labels.Set(objectLabels)) {
				if err := handler.cache.Delete(resource); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
