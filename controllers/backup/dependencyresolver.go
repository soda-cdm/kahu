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
	"reflect"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/utils"
)

type Interface interface {
	Resolve(cache.Indexer) error
}

type DependencyProvider interface {
	GetDependency(kubeClient kubernetes.Interface,
		resource *unstructured.Unstructured) ([]ResourceIdentifier, error)
}

type dependencyResolver struct {
	logger          log.FieldLogger
	kubeClient      kubernetes.Interface
	dynamicClient   dynamic.Interface
	discoveryHelper discovery.DiscoveryHelper
	depProvider     map[string]DependencyProvider
}

type ResourceIdentifier struct {
	GroupVersion string
	Kind         string
	Namespace    string
	Name         string
}

func NewResolver(logger log.FieldLogger,
	kubeClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper) Interface {
	dependencyResolver := &dependencyResolver{
		logger:          logger,
		kubeClient:      kubeClient,
		dynamicClient:   dynamicClient,
		discoveryHelper: discoveryHelper,
		depProvider:     getAllProviders(logger),
	}
	return dependencyResolver
}

func (r *dependencyResolver) Resolve(backupObjects cache.Indexer) error {
	resourceList := backupObjects.List()
	for _, resource := range resourceList {
		var backupResource *unstructured.Unstructured
		switch unstructuredResource := resource.(type) {
		case *unstructured.Unstructured:
			backupResource = unstructuredResource
		case unstructured.Unstructured:
			backupResource = unstructuredResource.DeepCopy()
		default:
			r.logger.Warningf("Unknown cached resource type. %s", reflect.TypeOf(resource))
			continue
		}

		r.logger.Infof("Resolving addition resources for %s.%s", backupResource.GetKind(),
			backupResource.GetName())
		err := r.resolve(backupResource, backupObjects)
		if err != nil {
			r.logger.Warningf("Unable to resolve dependency %s", backupResource.GetKind())
			continue
		}
	}
	return nil
}

func (r *dependencyResolver) resolve(backupResource *unstructured.Unstructured, backupObjects cache.Indexer) error {
	depProvider, ok := r.getProvider(backupResource.GetKind())
	if !ok {
		r.logger.Infof("Resolver not available for %s", backupResource.GetKind())
		return nil
	}

	dependencies, err := depProvider.GetDependency(r.kubeClient, backupResource)
	if err != nil {
		r.logger.Warningf("Unable to get dependency resolved for %s. %s", backupResource.GetKind(), err)
		return nil
	}

	r.logger.Infof("Dependency for %s are %+v", backupResource.GetKind(), dependencies)
	for _, dependency := range dependencies {
		if r.alreadyExist(dependency, backupObjects) {
			r.logger.Infof("Dependency %s.%s already exist", dependency.Kind, dependency.Name)
			continue
		}

		err := r.populateDependency(dependency, backupObjects)
		if err != nil {
			r.logger.Warningf("Unable to populate dependency %s", err)
		}
	}

	return nil
}

func (r *dependencyResolver) getProvider(kind string) (DependencyProvider, bool) {
	provider, ok := r.depProvider[kind]
	return provider, ok
}

func (r *dependencyResolver) alreadyExist(
	identifier ResourceIdentifier,
	backupObjects cache.Indexer) bool {
	objects, err := backupObjects.ByIndex(backupCacheResourceIndex, identifier.Kind)
	if err != nil {
		return false
	}

	for _, object := range objects {
		var resource *unstructured.Unstructured
		switch unstructuredResource := object.(type) {
		case *unstructured.Unstructured:
			resource = unstructuredResource
		case unstructured.Unstructured:
			resource = unstructuredResource.DeepCopy()
		default:
			r.logger.Warningf("Unknown cached resource type. %s", reflect.TypeOf(resource))
			continue
		}

		if identifier.GroupVersion == resource.GetAPIVersion() &&
			identifier.Kind == resource.GetKind() &&
			identifier.Name == resource.GetName() &&
			identifier.Namespace == resource.GetNamespace() {
			return true
		}
	}

	return false
}

func (r *dependencyResolver) populateDependency(
	identifier ResourceIdentifier,
	backupObjects cache.Indexer) error {

	apiResource, _, err := r.discoveryHelper.ByKind(identifier.Kind)
	if err != nil {
		return err
	}

	resourceInterface := r.dynamicClient.Resource(schema.GroupVersionResource{
		Group:    apiResource.Group,
		Version:  apiResource.Version,
		Resource: apiResource.Name,
	}).Namespace(identifier.Namespace)
	resource, err := resourceInterface.Get(context.TODO(), identifier.Name, metav1.GetOptions{})
	if err != nil {
		r.logger.Errorf("Unable to get resource for %+v", identifier)
		return err
	}

	resource.SetAPIVersion(schema.GroupVersion{
		Group:   apiResource.Group,
		Version: apiResource.Version,
	}.String())
	resource.SetKind(apiResource.Kind)

	err = backupObjects.Add(resource)
	if err != nil {
		return err
	}

	return r.resolve(resource, backupObjects)
}

func getAllProviders(logger log.FieldLogger) map[string]DependencyProvider {
	return map[string]DependencyProvider{
		utils.PV:          &pvResolver{logger: logger},
		utils.PVC:         &pvcResolver{logger: logger},
		utils.Pod:         &podResolver{logger: logger},
		utils.Deployment:  &deploymentResolver{logger: logger},
		utils.Replicaset:  &replicasetResolver{logger: logger},
		utils.DaemonSet:   &daemonsetResolver{logger: logger},
		utils.StatefulSet: &statefulsetResolver{logger: logger},
	}
}

type pvResolver struct {
	logger log.FieldLogger
}

func (r *pvResolver) GetDependency(_ kubernetes.Interface,
	resource *unstructured.Unstructured) ([]ResourceIdentifier, error) {
	additionalResources := make([]ResourceIdentifier, 0)
	var pv v1.PersistentVolume
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.UnstructuredContent(), &pv)
	if err != nil {
		r.logger.Errorf("Failed to translate unstructured (%s) to "+
			"pv. %s", resource.GetName(), err)
		return additionalResources, errors.Wrap(err, "Failed to covert unstructured resource to PV "+
			"for resolution")
	}

	if pv.Spec.ClaimRef == nil { // not bound to any PVC
		return additionalResources, nil
	}

	pvcRef := pv.Spec.ClaimRef
	return append(additionalResources, ResourceIdentifier{
		GroupVersion: pvcRef.APIVersion,
		Kind:         pvcRef.Kind,
		Name:         pvcRef.Name,
		Namespace:    pvcRef.Namespace,
	}), nil
}

type pvcResolver struct {
	logger log.FieldLogger
}

func (r *pvcResolver) GetDependency(_ kubernetes.Interface,
	resource *unstructured.Unstructured) ([]ResourceIdentifier, error) {
	additionalResources := make([]ResourceIdentifier, 0)
	var pvc v1.PersistentVolumeClaim
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &pvc)
	if err != nil {
		r.logger.Errorf("Failed to translate unstructured (%s) to "+
			"pv. %s", resource.GetName(), err)
		return additionalResources, errors.Wrap(err, "Failed to covert unstructured resource to PVC for resolution")
	}

	if pvc.Spec.StorageClassName == nil || // PVC not create with any Storage class.
		pvc.Spec.VolumeName == "" { // ignore PVC if not bond
		return additionalResources, nil
	}

	additionalResources = append(additionalResources, ResourceIdentifier{
		GroupVersion: schema.GroupVersion{Group: storagev1.GroupName, Version: "v1"}.String(),
		Kind:         utils.SC,
		Name:         *pvc.Spec.StorageClassName,
		Namespace:    "",
	})

	additionalResources = append(additionalResources, ResourceIdentifier{
		GroupVersion: schema.GroupVersion{Group: corev1.GroupName, Version: "v1"}.String(),
		Kind:         utils.PV,
		Name:         pvc.Spec.VolumeName,
		Namespace:    "",
	})

	return additionalResources, nil
}

type podResolver struct {
	logger log.FieldLogger
}

func (r *podResolver) GetDependency(_ kubernetes.Interface,
	resource *unstructured.Unstructured) ([]ResourceIdentifier, error) {
	additionalResources := make([]ResourceIdentifier, 0)
	var pod v1.Pod
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &pod)
	if err != nil {
		r.logger.Errorf("Failed to translate unstructured (%s) to "+
			"Pod. %s", resource.GetName(), err)
		return additionalResources, errors.Wrap(err, "Failed to covert unstructured resource to Pod"+
			" for resolution")
	}

	podDependency := getPodDependency(pod.Namespace, pod.Spec)

	return append(additionalResources, podDependency...), nil
}

type deploymentResolver struct {
	logger log.FieldLogger
}

func (r *deploymentResolver) GetDependency(_ kubernetes.Interface,
	resource *unstructured.Unstructured) ([]ResourceIdentifier, error) {
	additionalResources := make([]ResourceIdentifier, 0)
	var deployment appsv1.Deployment
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &deployment)
	if err != nil {
		r.logger.Errorf("Failed to translate unstructured (%s) to "+
			"Deployment. %s", resource.GetName(), err)
		return additionalResources, errors.Wrap(err, "Failed to covert unstructured resource to "+
			"Deployment for resolution")
	}

	podDependency := getPodDependency(deployment.Namespace, deployment.Spec.Template.Spec)

	return append(additionalResources, podDependency...), nil
}

type replicasetResolver struct {
	logger log.FieldLogger
}

func (r *replicasetResolver) GetDependency(_ kubernetes.Interface,
	resource *unstructured.Unstructured) ([]ResourceIdentifier, error) {
	additionalResources := make([]ResourceIdentifier, 0)
	var replicaset appsv1.ReplicaSet
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &replicaset)
	if err != nil {
		r.logger.Errorf("Failed to translate unstructured (%s) to "+
			"ResplicaSet. %s", resource.GetName(), err)
		return additionalResources, errors.Wrap(err, "Failed to covert unstructured resource to replicaset"+
			" for resolution")
	}

	podDependency := getPodDependency(replicaset.Namespace, replicaset.Spec.Template.Spec)

	return append(additionalResources, podDependency...), nil
}

type daemonsetResolver struct {
	logger log.FieldLogger
}

func (r *daemonsetResolver) GetDependency(_ kubernetes.Interface,
	resource *unstructured.Unstructured) ([]ResourceIdentifier, error) {
	additionalResources := make([]ResourceIdentifier, 0)
	var daemonset appsv1.DaemonSet
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &daemonset)
	if err != nil {
		r.logger.Errorf("Failed to translate unstructured (%s) to "+
			"DaemonSet. %s", resource.GetName(), err)
		return additionalResources, errors.Wrap(err, "Failed to covert unstructured resource to DaemonSet"+
			" for resolution")
	}

	podDependency := getPodDependency(daemonset.Namespace, daemonset.Spec.Template.Spec)

	return append(additionalResources, podDependency...), nil
}

type statefulsetResolver struct {
	logger log.FieldLogger
}

func (r *statefulsetResolver) GetDependency(kubeClient kubernetes.Interface,
	resource *unstructured.Unstructured) ([]ResourceIdentifier, error) {
	additionalResources := make([]ResourceIdentifier, 0)
	var statefulset appsv1.StatefulSet
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &statefulset)
	if err != nil {
		r.logger.Errorf("Failed to translate unstructured (%s) to "+
			"pv. %s", resource.GetName(), err)
		return additionalResources, errors.Wrap(err, "Failed to covert unstructured resource to PVC for "+
			"resolution")
	}

	podDependency := getPodDependency(statefulset.Namespace, statefulset.Spec.Template.Spec)

	if len(statefulset.Spec.VolumeClaimTemplates) > 0 {
		pvcList, err := kubeClient.CoreV1().PersistentVolumeClaims(statefulset.Namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: metav1.FormatLabelSelector(statefulset.Spec.Selector),
		})
		if err != nil {
			return additionalResources, errors.Wrap(err, "unable to list PVC fo statefulset")
		}

		for _, pvc := range pvcList.Items {
			additionalResources = append(additionalResources, ResourceIdentifier{
				GroupVersion: schema.GroupVersion{Group: corev1.GroupName, Version: "v1"}.String(),
				Kind:         utils.PVC,
				Name:         pvc.Name,
				Namespace:    statefulset.Namespace})
			additionalResources = append(additionalResources, ResourceIdentifier{
				GroupVersion: schema.GroupVersion{Group: corev1.GroupName, Version: "v1"}.String(),
				Kind:         utils.PV,
				Name:         pvc.Spec.VolumeName,
				Namespace:    ""})
			additionalResources = append(additionalResources, ResourceIdentifier{
				GroupVersion: schema.GroupVersion{Group: corev1.GroupName, Version: "v1"}.String(),
				Kind:         utils.SC,
				Name:         *pvc.Spec.StorageClassName,
				Namespace:    ""})
		}
	}

	return append(additionalResources, podDependency...), nil
}

func getPodDependency(namespace string, spec corev1.PodSpec) []ResourceIdentifier {
	additionalResources := make([]ResourceIdentifier, 0)

	for _, volume := range spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			additionalResources = append(additionalResources, ResourceIdentifier{
				GroupVersion: schema.GroupVersion{Group: corev1.GroupName, Version: "v1"}.String(),
				Kind:         utils.PVC,
				Name:         volume.PersistentVolumeClaim.ClaimName,
				Namespace:    namespace,
			})
			continue
		}

		if volume.ConfigMap != nil {
			additionalResources = append(additionalResources, ResourceIdentifier{
				GroupVersion: schema.GroupVersion{Group: corev1.GroupName, Version: "v1"}.String(),
				Kind:         utils.Configmap,
				Name:         volume.ConfigMap.Name,
				Namespace:    namespace,
			})
		}

		if volume.Secret != nil {
			additionalResources = append(additionalResources, ResourceIdentifier{
				GroupVersion: schema.GroupVersion{Group: corev1.GroupName, Version: "v1"}.String(),
				Kind:         utils.Secret,
				Name:         volume.Secret.SecretName,
				Namespace:    namespace,
			})
		}
	}

	return additionalResources
}
