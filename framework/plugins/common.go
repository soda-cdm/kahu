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

package plugins

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

const (
	defaultSAName = "default"
)

func getPodDependencies(namespace string,
	spec corev1.PodSpec) ([]k8sresource.ResourceReference, error) {
	additionalResources := make([]k8sresource.ResourceReference, 0)

	// collect all volumes
	for _, volume := range spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			additionalResources = append(additionalResources, k8sresource.ResourceReference{
				GroupVersionKind: k8sresource.PersistentVolumeClaimGVK,
				Name:             volume.PersistentVolumeClaim.ClaimName,
				Namespace:        namespace,
			})
			continue
		}

		if volume.ConfigMap != nil {
			additionalResources = append(additionalResources, k8sresource.ResourceReference{
				GroupVersionKind: k8sresource.ConfigmapGVK,
				Name:             volume.ConfigMap.Name,
				Namespace:        namespace,
			})
		}

		if volume.Secret != nil {
			additionalResources = append(additionalResources, k8sresource.ResourceReference{
				GroupVersionKind: k8sresource.SecretGVK,
				Name:             volume.Secret.SecretName,
				Namespace:        namespace,
			})
		}
	}

	// add container dependencies
	additionalResources = append(additionalResources, getContainerDependencies(namespace, spec.Containers)...)

	// add init container dependencies
	additionalResources = append(additionalResources, getContainerDependencies(namespace, spec.InitContainers)...)

	// add ephemeral container dependencies
	additionalResources = append(additionalResources, getEphemeralContainerDependencies(namespace,
		spec.EphemeralContainers)...)

	// add service account dependency
	if spec.ServiceAccountName != "" &&
		spec.ServiceAccountName != defaultSAName {
		additionalResources = append(additionalResources, k8sresource.ResourceReference{
			Namespace:        namespace,
			Name:             spec.ServiceAccountName,
			GroupVersionKind: k8sresource.ServiceAccountGVK,
		})
	}

	return additionalResources, nil
}

func getContainerDependencies(namespace string, containers []corev1.Container) []k8sresource.ResourceReference {
	additionalResources := make([]k8sresource.ResourceReference, 0)

	for _, container := range containers {
		additionalResources = append(additionalResources, getContainerEnvDependencies(namespace, container.Env)...)
		additionalResources = append(additionalResources, getContainerEnvFromDependencies(namespace, container.EnvFrom)...)
	}

	return additionalResources
}

func getEphemeralContainerDependencies(namespace string,
	containers []corev1.EphemeralContainer) []k8sresource.ResourceReference {
	additionalResources := make([]k8sresource.ResourceReference, 0)

	for _, container := range containers {
		additionalResources = append(additionalResources, getContainerEnvDependencies(namespace, container.Env)...)
		additionalResources = append(additionalResources, getContainerEnvFromDependencies(namespace, container.EnvFrom)...)
	}

	return additionalResources
}

func getContainerEnvDependencies(namespace string, envs []corev1.EnvVar) []k8sresource.ResourceReference {
	additionalResources := make([]k8sresource.ResourceReference, 0)

	for _, env := range envs {
		if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
			additionalResources = append(additionalResources, k8sresource.ResourceReference{
				Name:             env.ValueFrom.ConfigMapKeyRef.Name,
				Namespace:        namespace,
				GroupVersionKind: k8sresource.ConfigmapGVK,
			})
		}

		if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			additionalResources = append(additionalResources, k8sresource.ResourceReference{
				Name:             env.ValueFrom.SecretKeyRef.Name,
				Namespace:        namespace,
				GroupVersionKind: k8sresource.SecretGVK,
			})
		}
	}
	return additionalResources
}

func getContainerEnvFromDependencies(namespace string, envs []corev1.EnvFromSource) []k8sresource.ResourceReference {
	additionalResources := make([]k8sresource.ResourceReference, 0)

	for _, env := range envs {
		if env.ConfigMapRef != nil {
			additionalResources = append(additionalResources, k8sresource.ResourceReference{
				Name:             env.ConfigMapRef.Name,
				Namespace:        namespace,
				GroupVersionKind: k8sresource.ConfigmapGVK,
			})
		}

		if env.SecretRef != nil {
			additionalResources = append(additionalResources, k8sresource.ResourceReference{
				Name:             env.SecretRef.Name,
				Namespace:        namespace,
				GroupVersionKind: k8sresource.SecretGVK,
			})
		}
	}

	return additionalResources

}

func fetchResources(ctx context.Context,
	resourcesRef []k8sresource.ResourceReference,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper) ([]k8sresource.Resource, error) {
	resources := make([]k8sresource.Resource, 0)
	var resourceInterface dynamic.ResourceInterface
	for _, ref := range resourcesRef {
		_, apiResource, err := discoveryHelper.ByGroupVersionKind(ref.GroupVersionKind)
		if err != nil {
			return nil, err
		}

		resourceInterface = dynamicClient.Resource(schema.GroupVersionResource{
			Group:    apiResource.Group,
			Version:  apiResource.Version,
			Resource: apiResource.Name,
		}).Namespace(ref.Namespace)
		if !apiResource.Namespaced {
			resourceInterface = dynamicClient.Resource(schema.GroupVersionResource{
				Group:    apiResource.Group,
				Version:  apiResource.Version,
				Resource: apiResource.Name,
			})
		}

		resource, err := resourceInterface.Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		resource.SetAPIVersion(schema.GroupVersion{
			Group:   apiResource.Group,
			Version: apiResource.Version,
		}.String())
		resource.SetKind(apiResource.Kind)

		k8sResource, err := k8sresource.ToResource(resource)
		if err != nil {
			return nil, err
		}
		resources = append(resources, k8sResource)
	}

	return resources, nil
}
