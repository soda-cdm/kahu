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

package discovery

import (
	"sync"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
)

// DiscoveryHelper exposes functions for Kubernetes discovery API.
type DiscoveryHelper interface {
	// APIGroups returns current supported APIGroups in kubernetes cluster
	APIGroups() []metav1.APIGroup

	// ServerVersion returns kubernetes version information
	ServerVersion() *version.Info

	// Resources returns the current set of resources retrieved from discovery
	Resources() []*metav1.APIResourceList

	// ByGroupVersionKind gets a fully-resolved GroupVersionResource and an
	// APIResource for the provided GroupVersionKind.
	ByGroupVersionKind(input schema.GroupVersionKind) (schema.GroupVersionResource,
		metav1.APIResource,
		error)

	// Refresh updates API resource list with discovery helper
	Refresh() error
}

type discoverHelper struct {
	discoveryClient    discovery.DiscoveryInterface
	logger             logrus.FieldLogger
	lock               sync.RWMutex
	resources          []*metav1.APIResourceList
	byGroupVersionKind map[schema.GroupVersionKind]metav1.APIResource
	k8sAPIGroups       []metav1.APIGroup
	k8sVersion         *version.Info
}

var _ DiscoveryHelper = &discoverHelper{}

func NewDiscoveryHelper(discoveryClient discovery.DiscoveryInterface,
	logger logrus.FieldLogger) (DiscoveryHelper, error) {
	helper := &discoverHelper{
		discoveryClient: discoveryClient,
		logger:          logger,
	}
	if err := helper.Refresh(); err != nil {
		return nil, err
	}
	return helper, nil
}

func (helper *discoverHelper) ByGroupVersionKind(input schema.GroupVersionKind) (schema.GroupVersionResource,
	metav1.APIResource, error) {
	helper.lock.RLock()
	defer helper.lock.RUnlock()

	if resource, ok := helper.byGroupVersionKind[input]; ok {
		return schema.GroupVersionResource{
			Group:    input.Group,
			Version:  input.Version,
			Resource: resource.Name,
		}, resource, nil
	}

	err := helper.Refresh()
	if err != nil {
		return schema.GroupVersionResource{}, metav1.APIResource{}, err
	}

	if resource, ok := helper.byGroupVersionKind[input]; ok {
		return schema.GroupVersionResource{
			Group:    input.Group,
			Version:  input.Version,
			Resource: resource.Name,
		}, resource, nil
	}

	return schema.GroupVersionResource{},
		metav1.APIResource{},
		errors.Errorf("APIResource not found for GroupVersionKind %v ", input)
}

func (helper *discoverHelper) Refresh() error {
	helper.lock.Lock()
	defer helper.lock.Unlock()

	apiResourcesList, err := helper.discoveryClient.ServerPreferredResources()
	if err != nil {
		if discoveryErr, ok := err.(*discovery.ErrGroupDiscoveryFailed); ok {
			for groupVersion, err := range discoveryErr.Groups {
				helper.logger.WithError(err).Warnf("Failed to discover group: %v", groupVersion)
			}
		}
		return err
	}

	helper.resources = discovery.FilteredBy(
		discovery.ResourcePredicateFunc(filterByVerbs),
		apiResourcesList,
	)

	helper.byGroupVersionKind = make(map[schema.GroupVersionKind]metav1.APIResource)
	for _, resourceGroup := range helper.resources {
		gv, err := schema.ParseGroupVersion(resourceGroup.GroupVersion)
		if err != nil {
			return errors.Wrapf(err, "unable to parse GroupVersion %s", resourceGroup.GroupVersion)
		}

		for _, resource := range resourceGroup.APIResources {
			gvk := gv.WithKind(resource.Kind)
			helper.byGroupVersionKind[gvk] = resource
		}
	}

	apiGroupList, err := helper.discoveryClient.ServerGroups()
	if err != nil {
		return errors.WithStack(err)
	}
	helper.k8sAPIGroups = apiGroupList.Groups

	serverVersion, err := helper.discoveryClient.ServerVersion()
	if err != nil {
		return errors.WithStack(err)
	}

	helper.k8sVersion = serverVersion

	return nil
}

func (helper *discoverHelper) Resources() []*metav1.APIResourceList {
	helper.lock.RLock()
	defer helper.lock.RUnlock()
	return helper.resources
}

func (helper *discoverHelper) APIGroups() []metav1.APIGroup {
	helper.lock.RLock()
	defer helper.lock.RUnlock()
	return helper.k8sAPIGroups
}

func (helper *discoverHelper) ServerVersion() *version.Info {
	helper.lock.RLock()
	defer helper.lock.RUnlock()
	return helper.k8sVersion
}

func filterByVerbs(groupVersion string, r *metav1.APIResource) bool {
	return discovery.SupportsAllVerbs{Verbs: []string{"list", "create", "get", "delete"}}.Match(groupVersion, r)
}
