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
	"reflect"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/utils"
)

var excludeResourceType = sets.NewString(
	utils.Node,
	utils.Event,
	utils.VolumeSnapshot,
	utils.EndpointSlice)

const (
	AnnResourceSelector = "kahu.io/backup-resources"
)

type Resources interface {
	Sync(backup *kahuapi.Backup) (*kahuapi.Backup, error)
	GetNamespaces() []string
	GetResourcesByKind(kind string) []*unstructured.Unstructured
	GetClusterScopedResources() []*unstructured.Unstructured
	GetResources() []*unstructured.Unstructured
}

type backupResources struct {
	logger             log.FieldLogger
	dynamicClient      dynamic.Interface
	kubeClient         kubernetes.Interface
	discoveryHelper    discovery.DiscoveryHelper
	updater            Updater
	resourceCache      cache.Indexer
	dependencyResolver Interface
}

func NewBackupResources(
	logger log.FieldLogger,
	dynamicClient dynamic.Interface,
	kubeClient kubernetes.Interface,
	discoveryHelper discovery.DiscoveryHelper,
	updater Updater) Resources {
	return &backupResources{
		logger:          logger.WithField("context", "resolver"),
		dynamicClient:   dynamicClient,
		kubeClient:      kubeClient,
		discoveryHelper: discoveryHelper,
		updater:         updater,
		resourceCache: cache.NewIndexer(uidKeyFunc,
			backupObjectIndexers(logger)),
		dependencyResolver: NewResolver(logger, kubeClient, dynamicClient, discoveryHelper),
	}
}

func uidKeyFunc(obj interface{}) (string, error) {
	if key, ok := obj.(cache.ExplicitKey); ok {
		return string(key), nil
	}
	meta, err := meta.Accessor(obj)
	if err != nil {
		return "", fmt.Errorf("object has no meta: %v", err)
	}

	return string(meta.GetUID()), nil
}

func backupObjectIndexers(_ log.FieldLogger) cache.Indexers {
	return cache.Indexers{
		backupCacheNamespaceIndex: func(obj interface{}) ([]string, error) {
			keys := make([]string, 0)
			var namespaceResource *unstructured.Unstructured
			switch t := obj.(type) {
			case *unstructured.Unstructured:
				namespaceResource = t
			case unstructured.Unstructured:
				namespaceResource = t.DeepCopy()
			default:
				return keys, nil
			}

			if namespaceResource.GetKind() == "Namespace" {
				keys = append(keys, namespaceResource.GetName())
			}

			return keys, nil
		},
		backupCacheResourceIndex: func(obj interface{}) ([]string, error) {
			keys := make([]string, 0)
			var resource runtime.Unstructured
			switch t := obj.(type) {
			case runtime.Unstructured:
				resource = t
			default:
				return keys, nil
			}

			return append(keys, resource.GetObjectKind().GroupVersionKind().Kind), nil
		},
		backupCacheObjectClusterResourceIndex: func(obj interface{}) ([]string, error) {
			keys := make([]string, 0)
			var resource *unstructured.Unstructured
			switch t := obj.(type) {
			case *unstructured.Unstructured:
				resource = t
			case unstructured.Unstructured:
				resource = t.DeepCopy()
			default:
				return keys, nil
			}

			if resource.GetNamespace() == "" {
				keys = append(keys, resource.GetName())
			}

			return keys, nil
		},
	}
}

func unstructuredResourceKeyFunc(resource *unstructured.Unstructured) string {
	if resource.GetNamespace() == "" {
		return fmt.Sprintf("%s.%s/%s", resource.GetKind(),
			resource.GetAPIVersion(),
			resource.GetName())
	}
	return fmt.Sprintf("%s.%s/%s/%s", resource.GetKind(),
		resource.GetAPIVersion(),
		resource.GetNamespace(),
		resource.GetName())
}

func (r *backupResources) Sync(backup *kahuapi.Backup) (*kahuapi.Backup, error) {
	// TODO check annotation if resolution done with backup spec.
	// once done get content from backup status
	// populate all backup resources in cache
	if metav1.HasAnnotation(backup.ObjectMeta, annBackupContentSynced) {
		return backup, nil
	}

	// populate backup resource cache with backup spec
	err := r.populateCacheFromBackupSpec(backup)
	if err != nil {
		return backup, err
	}

	return r.syncWithServer(backup)
}

func (r *backupResources) populateCacheFromBackupSpec(backup *kahuapi.Backup) error {
	// filter API Resources with excluded backup resource and backup spec
	apiResources, err := r.filteredAPIResources(backup)
	if err != nil {
		return errors.Wrap(err, "unable to get API resources")
	}

	// collect namespaces
	err = r.collectNamespacesWithBackupSpec(backup)
	if err != nil {
		return err
	}

	// collect namespaces
	namespaces := r.getCachedNamespaceNames()
	r.logger.Infof("Selected namespaces for backup %s", strings.Join(namespaces, ", "))

	apiResources = r.getFilteredNamespacedResources(apiResources)
	selector := labels.Everything()
	if backup.Spec.Label != nil {
		selector, err = metav1.LabelSelectorAsSelector(backup.Spec.Label)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("invalid label selector %s", backup.Spec.Label.String()))
		}
	}

	for _, namespace := range namespaces {
		r.logger.Infof("Collecting resources for namespace %s", namespace)
		resources, err := r.collectNamespaceResources(namespace, apiResources, selector)
		if err != nil {
			r.logger.Errorf("unable to retrieve resource for namespace %s", namespace)
			return errors.Wrap(err,
				fmt.Sprintf("unable to retrieve resource for namespace %s", namespace))
		}

		// filter collected resources by resource filter
		filteredResources := r.applyResourceFilters(backup, resources)

		// TODO: Add mutator for resources like unset service clusterIP

		r.logger.Infof("Collected resources for namespace %s", namespace)
		for _, resource := range filteredResources {
			r.logger.Infof("Resource %s", unstructuredResourceKeyFunc(resource))
			err := r.resourceCache.Add(resource.DeepCopy())
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("unable to populate backup "+
					"cache for resource %s", resource.GetName()))
			}
		}
	}

	// dependency resolution based on spec.
	// Ex: Need to back up PVC if a deployment with volume is getting backed up
	return r.dependencyResolver.Resolve(r.resourceCache)
}

func (r *backupResources) filteredAPIResources(backup *kahuapi.Backup) ([]*metav1.APIResource, error) {
	apiResources, err := r.discoveryHelper.GetNamespaceScopedAPIResources()
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

	return r.filterApiResourceByBackup(
		r.filteredAPIResourcesByAnnotation(filteredResources, backup), backup), nil
}

func (r *backupResources) filteredAPIResourcesByAnnotation(apiResources []*metav1.APIResource,
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

func (r *backupResources) filterApiResourceByBackup(
	apiResources []*metav1.APIResource,
	backup *kahuapi.Backup) []*metav1.APIResource {
	return r.filterApiResourceByExcludeResources(
		r.filterApiResourceByIncludeResources(apiResources, backup),
		backup)
}

func (r *backupResources) filterApiResourceByIncludeResources(
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

func (r *backupResources) filterApiResourceByExcludeResources(
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

func (r *backupResources) syncWithServer(backup *kahuapi.Backup) (*kahuapi.Backup, error) {
	resources := r.GetResources()
	backupResources := make([]kahuapi.BackupResource, 0)

	for _, resource := range resources {
		backupResources = append(backupResources, kahuapi.BackupResource{
			TypeMeta: metav1.TypeMeta{
				APIVersion: resource.GetAPIVersion(),
				Kind:       resource.GetKind(),
			},
			ResourceName: resource.GetName(),
			Namespace:    resource.GetNamespace(),
		})
	}

	r.logger.Infof("Final backup data : %+v", backupResources)

	return r.updater.updateBackupStatus(backup, kahuapi.BackupStatus{
		Resources: backupResources,
	})
}

func (r *backupResources) GetClusterScopedResources() []*unstructured.Unstructured {
	resources := make([]*unstructured.Unstructured, 0)
	indexNames := r.resourceCache.ListIndexFuncValues(backupCacheObjectClusterResourceIndex)
	for _, indexName := range indexNames {
		indexResources, err := r.resourceCache.ByIndex(backupCacheObjectClusterResourceIndex, indexName)
		if err != nil {
			r.logger.Errorf("Unable to get cluster scope resources")
			break
		}
		for _, indexResource := range indexResources {
			switch resource := indexResource.(type) {
			case unstructured.Unstructured:
				resources = append(resources, resource.DeepCopy())
			case *unstructured.Unstructured:
				resources = append(resources, resource)
			default:
				r.logger.Warningf("invalid resource format "+
					"in backup cache. %s", reflect.TypeOf(resource))
			}
		}
	}

	return resources
}

func (r *backupResources) GetNamespaceScopedResources() []*unstructured.Unstructured {
	resources := make([]*unstructured.Unstructured, 0)
	indexNames := r.resourceCache.ListIndexFuncValues(backupCacheObjectClusterResourceIndex)
	for _, indexName := range indexNames {
		indexResources, err := r.resourceCache.ByIndex(backupCacheObjectClusterResourceIndex, indexName)
		if err != nil {
			r.logger.Errorf("Unable to get cluster scope resources")
			break
		}
		for _, indexResource := range indexResources {
			switch resource := indexResource.(type) {
			case unstructured.Unstructured:
				resources = append(resources, resource.DeepCopy())
			case *unstructured.Unstructured:
				resources = append(resources, resource)
			default:
				r.logger.Warningf("invalid resource format "+
					"in backup cache. %s", reflect.TypeOf(resource))
			}
		}
	}

	return resources
}

func (r *backupResources) GetNamespaces() []string {
	return r.getCachedNamespaceNames()
}

func (r *backupResources) GetResources() []*unstructured.Unstructured {
	resources := make([]*unstructured.Unstructured, 0)
	resourceList := r.resourceCache.List()
	for _, t := range resourceList {
		switch resource := t.(type) {
		case unstructured.Unstructured:
			resources = append(resources, resource.DeepCopy())
		case *unstructured.Unstructured:
			resources = append(resources, resource.DeepCopy())
		default:
			r.logger.Warningf("invalid resource format "+
				"in backup cache. %s", reflect.TypeOf(resource))
		}
	}

	return resources
}

func (r *backupResources) GetResourcesByKind(kind string) []*unstructured.Unstructured {
	resources := make([]*unstructured.Unstructured, 0)

	indexResources, err := r.resourceCache.ByIndex(backupCacheResourceIndex, kind)
	if err != nil {
		r.logger.Errorf("Unable to get cluster scope resources")
		return resources
	}
	for _, indexResource := range indexResources {
		switch resource := indexResource.(type) {
		case unstructured.Unstructured:
			resources = append(resources, resource.DeepCopy())
		case *unstructured.Unstructured:
			resources = append(resources, resource.DeepCopy())
		default:
			r.logger.Warningf("invalid resource format "+
				"in backup cache. %s", reflect.TypeOf(resource))
		}
	}

	return resources
}

func (r *backupResources) collectNamespacesWithBackupSpec(backup *kahuapi.Backup) error {
	// collect namespaces
	namespaces, err := r.kubeClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		r.logger.Errorf("Unable to list namespace. %s", err)
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
		unMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(namespace.DeepCopy())
		if err != nil {
			r.logger.Warningf("Failed to translate namespace(%s) to "+
				"unstructured. %s", name, err)
		}
		err = r.resourceCache.Add(&unstructured.Unstructured{
			Object: unMap,
		})
		if err != nil {
			r.logger.Warningf("Failed to add namespace(%s). %s", name, err)
			return err
		}
	}

	return nil
}

func (r *backupResources) getCachedNamespaceNames() []string {
	// collect namespace names
	return r.resourceCache.ListIndexFuncValues(backupCacheNamespaceIndex)
}

func (r *backupResources) getFilteredNamespacedResources(
	apiResources []*metav1.APIResource) []*metav1.APIResource {
	// TODO (Amit Roushan): Need to add filter for supported resources
	// only support Pod, PVC and PV currently

	return apiResources
}

func (r *backupResources) applyResourceFilters(
	backup *kahuapi.Backup,
	resources []*unstructured.Unstructured) []*unstructured.Unstructured {
	filteredResources := make([]*unstructured.Unstructured, 0)
	for _, resource := range resources {
		if r.isResourceNeedBackup(backup, resource) {
			filteredResources = append(filteredResources, resource)
		}
	}

	return filteredResources
}

func (r *backupResources) isResourceNeedBackup(
	backup *kahuapi.Backup,
	resource *unstructured.Unstructured) bool {

	// ignore if resource has owner ref
	if resource.GetOwnerReferences() != nil {
		return false
	}

	// evaluate exclude resources
	for _, spec := range backup.Spec.ExcludeResources {
		resourceKind := resource.GetKind()
		resourceName := resource.GetName()
		if spec.Kind == resourceKind && spec.IsRegex {
			regex, err := regexp.Compile(spec.Name)
			if err != nil {
				r.logger.Warningf("Unable to compile regex %s", spec.Name)
				continue
			}
			// if regex  match with resourceName then return false else try
			// contine for other excludedResources
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
				r.logger.Warningf("Unable to compile regex %s", spec.Name)
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

func (r *backupResources) collectNamespaceResources(
	namespace string,
	apiResources []*metav1.APIResource,
	selector labels.Selector) ([]*unstructured.Unstructured, error) {

	resources := make([]*unstructured.Unstructured, 0)
	for _, resource := range apiResources {
		gvr := schema.GroupVersionResource{
			Group:    resource.Group,
			Version:  resource.Version,
			Resource: resource.Name,
		}
		unstructuredList, err := r.dynamicClient.Resource(gvr).Namespace(namespace).
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
		unstructuredResources := make([]*unstructured.Unstructured, 0)
		for _, item := range unstructuredList.Items {
			item.SetAPIVersion(schema.GroupVersion{
				Group:   resource.Group,
				Version: resource.Version,
			}.String())
			item.SetKind(resource.Kind)
			unstructuredResources = append(unstructuredResources, item.DeepCopy())
		}

		// add in final list
		resources = append(resources, unstructuredResources...)
	}

	return resources, nil
}
