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
	"regexp"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	utilscache "github.com/soda-cdm/kahu/utils/cache"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

type filterHandler interface {
	handle(restore *kahuapi.Restore, cache utilscache.Interface) error
}

func newRestoreSpecFilterHandler() filterHandler {
	logger := log.WithField("context", "restore-spec-filter")
	return newNamespaceFilter(logger,
		newResourceFilter(logger,
			newLabelSelectorFilter(logger)))
}

type namespaceFilter struct {
	next   filterHandler
	logger log.FieldLogger
}

func newNamespaceFilter(
	logger log.FieldLogger,
	handler filterHandler) filterHandler {
	return &namespaceFilter{
		next:   handler,
		logger: logger,
	}
}

func (handler *namespaceFilter) handle(restore *kahuapi.Restore, restoreResources utilscache.Interface) error {
	// perform include/exclude namespace on cache
	excludeNamespaces := sets.NewString()
	// process include namespace
	includeNamespaces := sets.NewString(restore.Spec.IncludeNamespaces...)

	backupNamespaces, err := restoreResources.GetByGVK(k8sresource.NamespaceGVK)
	if err != nil {
		return err
	}

	// filter include namespaces
	if len(includeNamespaces) != 0 {
		for _, namespace := range backupNamespaces {
			// if available namespace are not included exclude them
			if !includeNamespaces.Has(namespace.GetName()) {
				excludeNamespaces.Insert(namespace.GetName())
			}
		}
	}

	excludeNamespaces.Insert(restore.Spec.ExcludeNamespaces...)

	// remove objects from cache present in excluded namespaces
	excludeResources := make([]k8sresource.Resource, 0)

	for _, resource := range restoreResources.List() {
		if resource.GetKind() == k8sresource.NamespaceGVK.Kind &&
			excludeNamespaces.Has(resource.GetName()) {
			excludeResources = append(excludeResources, resource)
			continue
		}

		if excludeNamespaces.Has(resource.GetName()) {
			excludeResources = append(excludeResources, resource)
		}
	}

	for _, resource := range excludeResources {
		err = restoreResources.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resources from cache for "+
				"namespace resource exclusion. %s", err)
		}
	}

	return handler.next.handle(restore, restoreResources)
}

type resourceFilter struct {
	logger log.FieldLogger
	next   filterHandler
}

func newResourceFilter(
	logger log.FieldLogger,
	handler filterHandler) filterHandler {
	return &resourceFilter{
		next:   handler,
		logger: logger,
	}
}

func isMatch(resource k8sresource.Resource,
	resourceSpec kahuapi.ResourceSpec) bool {
	resourceKind := resource.GetKind()
	resourceName := resource.GetName()

	if resourceSpec.Kind != resourceKind {
		return false
	}

	if resourceSpec.IsRegex {
		match, err := regexp.MatchString(resourceSpec.Name, resourceName)
		if err != nil {
			return false
		}
		return match
	}

	return false
}

func isResourceNeedExclude(resource k8sresource.Resource,
	includeResourceSpecs []kahuapi.ResourceSpec,
	excludeResourceSpecs []kahuapi.ResourceSpec) bool {
	// exclude if in the exclusion list
	for _, spec := range excludeResourceSpecs {
		if isMatch(resource, spec) {
			return true
		}
	}

	// exclude if not in inclusion list
	exclude := false
	for _, spec := range includeResourceSpecs {
		if isMatch(resource, spec) {
			return false
		}
		exclude = true
	}

	return exclude
}

func (handler *resourceFilter) handle(restore *kahuapi.Restore, cache utilscache.Interface) error {
	// perform include/exclude resources on cache

	excludeResourceSpecs := make([]kahuapi.ResourceSpec, 0)
	includeResourceSpecs := make([]kahuapi.ResourceSpec, 0)
	includeResourceSpecs = append(includeResourceSpecs, restore.Spec.IncludeResources...)
	excludeResourceSpecs = append(excludeResourceSpecs, restore.Spec.ExcludeResources...)

	excludeResourceList := make([]k8sresource.Resource, 0)
	resourceList := cache.List()
	for _, resource := range resourceList {
		if isResourceNeedExclude(resource,
			includeResourceSpecs,
			excludeResourceSpecs) {
			excludeResourceList = append(excludeResourceList, resource)
		}
	}

	for _, resource := range excludeResourceList {
		err := cache.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resources from cache for "+
				"resource exclusion. %s", err)
		}
	}

	return handler.next.handle(restore, cache)
}

type labelSelectorFilter struct {
	logger log.FieldLogger
}

func newLabelSelectorFilter(logger log.FieldLogger) filterHandler {
	return &labelSelectorFilter{
		logger: logger,
	}
}

func (handler *labelSelectorFilter) handle(restore *kahuapi.Restore, cache utilscache.Interface) error {
	// perform include/exclude resources on cache
	if restore.Spec.LabelSelector != nil {
		for _, resource := range cache.List() {
			objectLabels := resource.GetLabels()
			selector, err := metav1.LabelSelectorAsSelector(restore.Spec.LabelSelector)
			if err != nil {
				return err
			}
			if !selector.Matches(labels.Set(objectLabels)) {
				if err := cache.Delete(resource); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
