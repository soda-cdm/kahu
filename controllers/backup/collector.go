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

package backup

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/utils"
	"github.com/soda-cdm/kahu/utils/cache"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

var excludeResourceType = sets.NewString(
	utils.Node,
	utils.Event,
	utils.VolumeSnapshot,
	utils.EndpointSlice)

const (
	AnnResourceSelector = "kahu.io/backup-resources"
)

type resourceCollector interface {
	FetchBySpec(backup *kahuapi.Backup) (cache.Interface, error)
	FetchByStatus(backup *kahuapi.Backup) (cache.Interface, error)
}

type collector struct {
	logger          log.FieldLogger
	dynamicClient   dynamic.Interface
	kubeClient      kubernetes.Interface
	discoveryHelper discovery.DiscoveryHelper
}

func NewResourceCollector(
	logger log.FieldLogger,
	dynamicClient dynamic.Interface,
	kubeClient kubernetes.Interface,
	discoveryHelper discovery.DiscoveryHelper) resourceCollector {
	return &collector{
		logger:          logger.WithField("context", "collector"),
		dynamicClient:   dynamicClient,
		kubeClient:      kubeClient,
		discoveryHelper: discoveryHelper,
	}
}

func (ctor *collector) FetchBySpec(backup *kahuapi.Backup) (cache.Interface, error) {
	if utils.ContainsAnnotation(backup, annBackupContentSynced) {
		return ctor.FetchByStatus(backup)
	}

	resourceCache := cache.NewCache()
	err := ctor.populateCacheBySpec(resourceCache, backup)
	return resourceCache, err
}

func (ctor *collector) FetchByStatus(backup *kahuapi.Backup) (cache.Interface, error) {
	resourceCache := cache.NewCache()

	for _, backupResource := range backup.Status.Resources {
		gvk := schema.FromAPIVersionAndKind(backupResource.APIVersion, backupResource.Kind)
		gvr, apiResource, err := ctor.discoveryHelper.ByGroupVersionKind(gvk)
		if err != nil {
			return resourceCache, err
		}

		var unstResource *unstructured.Unstructured
		if apiResource.Namespaced {
			unstResource, err = ctor.dynamicClient.Resource(gvr).Namespace(backupResource.Namespace).Get(context.TODO(),
				backupResource.ResourceName,
				metav1.GetOptions{})
			if err != nil {
				return resourceCache, err
			}
		} else {
			unstResource, err = ctor.dynamicClient.Resource(gvr).Get(context.TODO(),
				backupResource.ResourceName,
				metav1.GetOptions{})
			if err != nil {
				return resourceCache, err
			}
		}

		err = resourceCache.Add(k8sresource.Resource{
			Unstructured: unstructured.Unstructured{
				Object: unstResource.Object,
			},
		})
		if err != nil {
			ctor.logger.Errorf("Failed to add resource in cache. %s", err)
			return nil, err
		}
	}

	return resourceCache, nil
}

func (ctor *collector) populateCacheBySpec(resourceCache cache.Interface, backup *kahuapi.Backup) error {
	// filter API Resources with excluded backup resource and backup spec
	apiResources, err := ctor.filteredAPIResources(backup)
	if err != nil {
		return errors.Wrap(err, "unable to get API resources")
	}

	// collect namespaces
	err = ctor.collectNamespacesBySpec(resourceCache, backup)
	if err != nil {
		return err
	}

	// collect namespaces
	namespaces, err := ctor.getCachedNamespaceNames(resourceCache)
	if err != nil {
		return err
	}
	ctor.logger.Infof("Selected namespaces for backup %s", strings.Join(namespaces, ", "))

	selector := labels.Everything()
	if backup.Spec.Label != nil {
		selector, err = metav1.LabelSelectorAsSelector(backup.Spec.Label)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("invalid label selector %s", backup.Spec.Label.String()))
		}
	}

	for _, namespace := range namespaces {
		ctor.logger.Infof("Collecting resources for namespace %s", namespace)
		resources, err := ctor.collectNamespaceResources(namespace, apiResources, selector)
		if err != nil {
			ctor.logger.Errorf("unable to retrieve resource for namespace %s", namespace)
			return errors.Wrap(err,
				fmt.Sprintf("unable to retrieve resource for namespace %s", namespace))
		}

		// filter collected resources by resource filter
		filteredResources := ctor.applySpecFilters(backup, resources)

		ctor.logger.Infof("Collected resources for namespace %s", namespace)
		for _, resource := range filteredResources {
			ctor.logger.Infof("Resource %s, Name=%s", resource.GroupVersionKind().String(), resource.GetName())
			err := resourceCache.Add(resource)
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("unable to populate backup "+
					"cache for resource %s", resource.GetName()))
			}
		}
	}

	return nil
}

func (ctor *collector) filteredAPIResources(backup *kahuapi.Backup) ([]*metav1.APIResource, error) {
	apiResources, err := ctor.discoveryHelper.GetNamespaceScopedAPIResources()
	if err != nil {
		return nil, errors.Wrap(err, "unable to get namespaces API resource")
	}

	filteredResources := make([]*metav1.APIResource, 0)
	for _, apiResource := range apiResources {
		// filter excluded resources
		if excludeResourceType.Has(apiResource.Kind) {
			continue
		}
		filteredResources = append(filteredResources, apiResource)
	}

	return ctor.filterApiResourceByBackup(
		ctor.filteredAPIResourcesByAnnotation(filteredResources, backup), backup), nil
}

func (ctor *collector) filteredAPIResourcesByAnnotation(apiResources []*metav1.APIResource,
	backup *kahuapi.Backup) []*metav1.APIResource {
	if utils.ContainsAnnotation(backup, AnnResourceSelector) {
		resources, exist := utils.GetAnnotation(backup, AnnResourceSelector)
		if !exist {
			return apiResources
		}

		supportedResources := sets.NewString(strings.Split(resources, ",")...)
		filteredResources := make([]*metav1.APIResource, 0)
		for _, apiResource := range apiResources {
			if supportedResources.Has(apiResource.Kind) {
				filteredResources = append(filteredResources, apiResource)
			}
		}
		return filteredResources
	}

	return apiResources
}

func (ctor *collector) filterApiResourceByBackup(
	apiResources []*metav1.APIResource,
	backup *kahuapi.Backup) []*metav1.APIResource {
	return ctor.filterApiResourceByExcludeResources(
		ctor.filterApiResourceByIncludeResources(apiResources, backup), backup)
}

func (ctor *collector) filterApiResourceByIncludeResources(
	apiResources []*metav1.APIResource,
	backup *kahuapi.Backup) []*metav1.APIResource {
	// extract kind for exclusion
	includeKind := sets.NewString()
	for _, resourceSpec := range backup.Spec.IncludeResources {
		includeKind.Insert(resourceSpec.Kind)
	}

	if includeKind.Len() == 0 {
		return apiResources
	}

	filteredResources := make([]*metav1.APIResource, 0)
	for _, apiResource := range apiResources {
		// filter excluded resources
		if includeKind.Has(apiResource.Kind) {
			filteredResources = append(filteredResources, apiResource)
		}
	}

	return filteredResources
}

func (ctor *collector) filterApiResourceByExcludeResources(
	apiResources []*metav1.APIResource,
	backup *kahuapi.Backup) []*metav1.APIResource {
	// extract kind for exclusion
	filterKind := sets.NewString()
	for _, resourceSpec := range backup.Spec.ExcludeResources {
		switch resourceSpec.Name {
		case "", "*":
			filterKind.Insert(resourceSpec.Kind)
		}
	}

	if filterKind.Len() == 0 {
		return apiResources
	}

	filteredResources := make([]*metav1.APIResource, 0)
	for _, apiResource := range apiResources {
		// filter excluded resources
		if filterKind.Has(apiResource.Kind) {
			continue
		}
		filteredResources = append(filteredResources, apiResource)
	}

	return filteredResources
}

func (ctor *collector) collectNamespacesBySpec(cache cache.Interface, backup *kahuapi.Backup) error {
	// collect namespaces
	namespaces, err := ctor.kubeClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		ctor.logger.Errorf("Unable to list namespace. %s", err)
		return errors.Wrap(err, "unable to get namespaces")
	}

	namespaceList := make(map[string]v1.Namespace, 0)
	includeList := sets.NewString(backup.Spec.IncludeNamespaces...)
	excludeList := sets.NewString(backup.Spec.ExcludeNamespaces...)

	for _, namespace := range namespaces.Items {
		if excludeList.Has(namespace.Name) {
			continue
		}
		if includeList.Len() > 0 &&
			!includeList.Has(namespace.Name) {
			continue
		}
		namespaceList[namespace.Name] = namespace
	}

	for name, namespace := range namespaceList {
		namespace.Kind = "Namespace"
		namespace.APIVersion = "v1"
		namespaceResource, err := k8sresource.ToResource(&namespace)
		if err != nil {
			ctor.logger.Errorf("Failed to translate namespace(%s) to "+
				"unstructured. %s", name, err)
			return err
		}

		err = cache.Add(namespaceResource)
		if err != nil {
			ctor.logger.Warningf("Failed to add namespace(%s). %s", name, err)
			return err
		}
	}

	return nil
}

func (ctor *collector) getCachedNamespaceNames(resourceCache cache.Interface) ([]string, error) {
	// collect namespace names
	namespaces, err := resourceCache.GetByGVK(k8sresource.NamespaceGVK)
	if err != nil {
		ctor.logger.Errorf("Failed to list namespace for cache. %s", err)
		return nil, err
	}

	namespaceNames := sets.NewString()
	for _, namespace := range namespaces {
		namespaceNames.Insert(namespace.GetName())
	}
	return namespaceNames.List(), nil
}

func (ctor *collector) applySpecFilters(
	backup *kahuapi.Backup,
	resources []k8sresource.Resource) []k8sresource.Resource {
	filteredResources := make([]k8sresource.Resource, 0)
	for _, resource := range resources {
		if ctor.isResourceNeedBackup(backup, resource) {
			filteredResources = append(filteredResources, resource)
		}
	}

	return filteredResources
}

func (ctor *collector) isResourceNeedBackup(
	backup *kahuapi.Backup,
	resource k8sresource.Resource) bool {

	// TODO: handle in restore
	//// ignore if resource has owner ref
	//if resource.GetOwnerReferences() != nil {
	//	return false
	//}

	// evaluate exclude resources
	for _, spec := range backup.Spec.ExcludeResources {
		resourceKind := resource.GetKind()
		resourceName := resource.GetName()
		if spec.Kind == resourceKind && spec.IsRegex {
			regex, err := regexp.Compile(spec.Name)
			if err != nil {
				ctor.logger.Warningf("Unable to compile regex %s", spec.Name)
				continue
			}
			// if regex  match with resourceName then return false else try
			// continue for other excludedResources
			if regex.Match([]byte(resourceName)) {
				return false
			}
		} else if spec.Kind == resourceKind {
			if resourceName == "" || resourceName == spec.Name {
				return false
			}
		}
	}

	if len(backup.Spec.IncludeResources) == 0 {
		return true
	}

	// evaluate include resources
	for _, spec := range backup.Spec.IncludeResources {
		resourceKind := resource.GetKind()
		resourceName := resource.GetName()
		if spec.Kind == resourceKind && spec.IsRegex {
			regex, err := regexp.Compile(spec.Name)
			if err != nil {
				ctor.logger.Warningf("Unable to compile regex %s", spec.Name)
				continue
			}
			// if regex  match with resourceName then return true else try
			// contine for other includedResources
			if regex.Match([]byte(resourceName)) {
				return true
			}
		} else if spec.Kind == resourceKind {
			if resourceName == "" || resourceName == spec.Name {
				return true
			}
		}
	}

	return false
}

func (ctor *collector) collectNamespaceResources(
	namespace string,
	apiResources []*metav1.APIResource,
	selector labels.Selector) ([]k8sresource.Resource, error) {

	resources := make([]k8sresource.Resource, 0)
	for _, resource := range apiResources {
		gvr := schema.GroupVersionResource{
			Group:    resource.Group,
			Version:  resource.Version,
			Resource: resource.Name,
		}
		unstructuredList, err := ctor.dynamicClient.Resource(gvr).Namespace(namespace).
			List(context.TODO(), metav1.ListOptions{
				LabelSelector: selector.String(),
			})
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, errors.Wrap(err, fmt.Sprintf("failed to list resource "+
				"info for %s", gvr))
		}

		// populate GVK
		k8sResources := make([]k8sresource.Resource, 0)
		for _, item := range unstructuredList.Items {
			item.SetAPIVersion(schema.GroupVersion{
				Group:   resource.Group,
				Version: resource.Version,
			}.String())
			item.SetKind(resource.Kind)
			k8sResources = append(k8sResources, k8sresource.Resource{
				Unstructured: unstructured.Unstructured{
					Object: item.Object,
				},
			})
		}

		// add in final list
		resources = append(resources, k8sResources...)
	}

	return resources, nil
}
