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

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
	utilscache "github.com/soda-cdm/kahu/utils/cache"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

type mutationHandler interface {
	handle(restore *kahuapi.Restore, restoreResources utilscache.Interface) error
}

func newRestoreResourceMutationHandler() mutationHandler {
	logger := log.WithField("module", "restore-mutator")
	return newNamespaceMutator(
		newPVCMutation(logger,
			newServiceMutation(logger,
				newRestoreIdentifierMutation(logger,
					newRoleBindingMutator(
						newClusterRoleBindingMutator(
							newOwnerRefMutation(logger)))))))
}

type namespaceMutation struct {
	next mutationHandler
}

func newNamespaceMutator(next mutationHandler) mutationHandler {
	return &namespaceMutation{
		next: next,
	}
}

func (handler *namespaceMutation) handle(restore *kahuapi.Restore, restoreResources utilscache.Interface) error {
	// perform namespace mutation
	for oldNamespace, newNamespace := range restore.Spec.NamespaceMapping {
		for _, resource := range restoreResources.List() {
			if resource.GetNamespace() != oldNamespace {
				continue
			}

			newObject := resource.DeepCopy()
			newObject.SetNamespace(newNamespace)

			// delete old cached object
			err := restoreResources.Delete(resource)
			if err != nil {
				return fmt.Errorf("failed to delete resource from cache for "+
					"namespace resource mutation. %s", err)
			}

			// add new object in cache
			err = restoreResources.Add(newObject)
			if err != nil {
				return fmt.Errorf("failed to add resource in cache for "+
					"namespace resource mutation. %s", err)
			}
		}
	}

	return handler.next.handle(restore, restoreResources)
}

type serviceMutation struct {
	logger log.FieldLogger
	next   mutationHandler
}

func newServiceMutation(logger log.FieldLogger, next mutationHandler) mutationHandler {
	return &serviceMutation{
		logger: logger,
		next:   next,
	}
}

func (handler *serviceMutation) handle(restore *kahuapi.Restore, restoreResources utilscache.Interface) error {
	// perform service IP
	resourceList, err := restoreResources.GetByGVK(k8sresource.ServiceGVK)
	if err != nil {
		return fmt.Errorf("failed to retrieve resources from cache for "+
			"service mutation. %s", err)
	}

	for _, resource := range resourceList {
		var service corev1.Service
		err := k8sresource.FromResource(resource, &service)
		if err != nil {
			return fmt.Errorf("failed to translate to service for mutation. %s", err)
		}

		if service.Spec.ClusterIP != "None" && !preserveClusterIpAddr(restore) {
			// reset ClusterIP
			service.Spec.ClusterIP = ""
			// reset ClusterIPs
			service.Spec.ClusterIPs = make([]string, 0)
		}
		// check for node port preservation
		if !preserveNodePort(restore) {
			// reset node port
			resetNodePort(&service)
		}

		// delete old cached object
		err = restoreResources.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"service mutation. %s", err)
		}

		resource, err = k8sresource.ToResource(&service)
		if err != nil {
			return fmt.Errorf("failed to translate to unstructure during service mutation. %s", err)
		}

		// add new object in cache
		err = restoreResources.Add(resource)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"service mutation. %s", err)
		}

	}

	return handler.next.handle(restore, restoreResources)
}

func preserveNodePort(restore *kahuapi.Restore) bool {
	preserve := restore.Spec.PreserveNodePort
	return preserve != nil && *preserve == true
}

func preserveClusterIpAddr(restore *kahuapi.Restore) bool {
	preserve := restore.Spec.PreserveClusterIpAddr
	return preserve != nil && *preserve == true
}

func resetNodePort(service *corev1.Service) {
	for i := range service.Spec.Ports {
		service.Spec.Ports[i].NodePort = 0
	}
}

type ownerRefMutation struct {
	logger log.FieldLogger
}

func newOwnerRefMutation(logger log.FieldLogger) mutationHandler {
	return &ownerRefMutation{
		logger: logger,
	}
}

func (handler *ownerRefMutation) handle(_ *kahuapi.Restore, restoreResources utilscache.Interface) error {
	for _, resource := range restoreResources.List() {
		if len(resource.GetOwnerReferences()) == 0 {
			continue
		}

		// remove owner reference
		newObject := resource.DeepCopy()
		newObject.SetOwnerReferences(nil)

		// delete old cached object
		err := restoreResources.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"namespace resource mutation. %s", err)
		}

		// add new object in cache
		err = restoreResources.Add(newObject)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"namespace resource mutation. %s", err)
		}
	}

	return nil
}

type restoreIdentificationMutation struct {
	logger log.FieldLogger
	next   mutationHandler
}

func newRestoreIdentifierMutation(logger log.FieldLogger, next mutationHandler) mutationHandler {
	return &restoreIdentificationMutation{
		logger: logger,
		next:   next,
	}
}

func (handler *restoreIdentificationMutation) handle(restore *kahuapi.Restore,
	restoreResources utilscache.Interface) error {
	// Add restore identification annotation to all the resources being restored
	handler.logger.Infoln("Adding restore identifier annotation for the resources being restored")
	resources := restoreResources.List()

	for _, resource := range resources {
		// ignore CRDs
		if resource.GetObjectKind().GroupVersionKind().Kind == crdName {
			continue
		}

		// get the resourcename and kind
		kind := resource.GetKind()
		resourceName := resource.GetName()

		if kind == "ServiceAccount" && resourceName == "default" {
			handler.logger.Infof("service account name is default. So not mutated")
			continue
		}

		annots := resource.GetAnnotations()
		if annots == nil {
			annots = make(map[string]string, 0)
		}
		annots[annRestoreIdentifier] = restore.Name
		resource.SetAnnotations(annots)

		// delete old cached object
		err := restoreResources.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"restore identification mutation. %s", err)
		}

		// add new object in cache
		err = restoreResources.Add(resource)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"restore identification mutation. %s", err)
		}

	}
	return handler.next.handle(restore, restoreResources)
}

type pvcMutation struct {
	logger log.FieldLogger
	next   mutationHandler
}

func newPVCMutation(logger log.FieldLogger, next mutationHandler) mutationHandler {
	return &pvcMutation{
		logger: logger,
		next:   next,
	}
}

func (handler *pvcMutation) handle(restore *kahuapi.Restore, restoreResources utilscache.Interface) error {
	// get all restore PVC
	resourceList, err := restoreResources.GetByGVK(k8sresource.PersistentVolumeClaimGVK)
	if err != nil {
		return fmt.Errorf("failed to retrieve resources from cache for "+
			"pvc mutation. %s", err)
	}

	for _, resource := range resourceList {
		var pvc corev1.PersistentVolumeClaim
		err := k8sresource.FromResource(resource, &pvc)
		if err != nil {
			return fmt.Errorf("failed to translate to pvc for mutation. %s", err)
		}

		if pvc.Spec.DataSource != nil {
			pvc.Spec.DataSource = nil
		}

		if pvc.Spec.DataSourceRef != nil {
			pvc.Spec.DataSourceRef = nil
		}

		// delete old cached object
		err = restoreResources.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"pvc mutation. %s", err)
		}

		resource, err = k8sresource.ToResource(&pvc)
		if err != nil {
			return fmt.Errorf("failed to translate to unstructure during pvc mutation. %s", err)
		}

		// add new object in cache
		err = restoreResources.Add(resource)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"pvc mutation. %s", err)
		}

	}

	return handler.next.handle(restore, restoreResources)
}

type roleBindingMutation struct {
	next mutationHandler
}

func newRoleBindingMutator(next mutationHandler) mutationHandler {
	return &roleBindingMutation{
		next: next,
	}
}

func (handler *roleBindingMutation) handle(restore *kahuapi.Restore, restoreResources utilscache.Interface) error {
	// extract RoleBinding resources
	resourceList, err := restoreResources.GetByGVK(k8sresource.RoleBindingGVK)
	if err != nil {
		return fmt.Errorf("failed to retrieve resources from cache for "+
			"role binding mutation. %s", err)
	}

	for _, resource := range resourceList {
		var rolBinding rbacv1.RoleBinding
		err = k8sresource.FromResource(resource, &rolBinding)
		if err != nil {
			return fmt.Errorf("failed to translate to service for mutation. %s", err)
		}

		mutateSubjectNamespace(rolBinding.Subjects, restore.Spec.NamespaceMapping)

		newResource, err := k8sresource.ToResource(&rolBinding)
		if err != nil {
			return fmt.Errorf("failed to translate to unstructure during service mutation. %s", err)
		}

		// delete old cached object
		err = restoreResources.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"role binding resource mutation. %s", err)
		}

		// add new object in cache
		err = restoreResources.Add(newResource)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"role binding resource mutation. %s", err)
		}
	}

	return handler.next.handle(restore, restoreResources)
}

type clusterRoleBindingMutation struct {
	next mutationHandler
}

func newClusterRoleBindingMutator(next mutationHandler) mutationHandler {
	return &clusterRoleBindingMutation{
		next: next,
	}
}

func (handler *clusterRoleBindingMutation) handle(restore *kahuapi.Restore, restoreResources utilscache.Interface) error {
	// extract ClusterRoleBinding resources
	resourceList, err := restoreResources.GetByGVK(k8sresource.ClusterRoleBindingGVK)
	if err != nil {
		return fmt.Errorf("failed to retrieve resources from cache for "+
			"cluster role binding mutation. %s", err)
	}

	for _, resource := range resourceList {
		var clusterRoleBinding rbacv1.ClusterRoleBinding
		err = k8sresource.FromResource(resource, &clusterRoleBinding)
		if err != nil {
			return fmt.Errorf("failed to translate to service for mutation. %s", err)
		}

		// mutate subject
		mutateSubjectNamespace(clusterRoleBinding.Subjects, restore.Spec.NamespaceMapping)

		newResource, err := k8sresource.ToResource(&clusterRoleBinding)
		if err != nil {
			return fmt.Errorf("failed to translate to unstructure during service mutation. %s", err)
		}

		// delete old cached object
		err = restoreResources.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"cluster role binding resource mutation. %s", err)
		}

		// add new object in cache
		err = restoreResources.Add(newResource)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"cluster role binding resource mutation. %s", err)
		}
	}

	return handler.next.handle(restore, restoreResources)
}

func mutateSubjectNamespace(subjects []rbacv1.Subject, namespaceMap map[string]string) {
	for i, subject := range subjects {
		if subject.Kind == utils.ServiceAccount && subject.Namespace != "" {
			newNS, ok := namespaceMap[subject.Namespace]
			if !ok {
				continue
			}
			subjects[i].Namespace = newNS
		}
	}
}
