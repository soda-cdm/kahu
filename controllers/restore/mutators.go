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
	"strconv"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
)

type mutationHandler interface {
	handle(restore *kahuapi.Restore, indexer cache.Indexer) error
}

func constructMutationHandler(logger log.FieldLogger) mutationHandler {
	return newNamespaceMutator(
		newPVCMutation(logger,
			newServiceMutation(logger,
				newClusterRoleBindingMutator(
					newRoleBindingMutator(
						newRestoreIdentifierMutation(logger,
							newPrefixMutation(logger)))))))
}

type namespaceMutation struct {
	next mutationHandler
}

func newNamespaceMutator(next mutationHandler) mutationHandler {
	return &namespaceMutation{
		next: next,
	}
}

func (handler *namespaceMutation) handle(restore *kahuapi.Restore, indexer cache.Indexer) error {
	// perform namespace mutation
	for oldNamespace, newNamespace := range restore.Spec.NamespaceMapping {
		resourceList, err := indexer.ByIndex(backupObjectNamespaceIndex, oldNamespace)
		if err != nil {
			return fmt.Errorf("failed to retrieve resources from cache for "+
				"namespace mutation. %s", err)
		}

		for _, resource := range resourceList {
			object, ok := resource.(*unstructured.Unstructured)
			if !ok {
				return fmt.Errorf("restore index cache with invalid object type %v",
					reflect.TypeOf(resource))
			}

			newObject := object.DeepCopy()
			newObject.SetNamespace(newNamespace)

			// delete old cached object
			err := indexer.Delete(resource)
			if err != nil {
				return fmt.Errorf("failed to delete resource from cache for "+
					"namespace resource mutation. %s", err)
			}

			// add new object in cache
			err = indexer.Add(newObject)
			if err != nil {
				return fmt.Errorf("failed to add resource in cache for "+
					"namespace resource mutation. %s", err)
			}
		}
	}

	return handler.next.handle(restore, indexer)
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

func (handler *serviceMutation) handle(restore *kahuapi.Restore, indexer cache.Indexer) error {
	// perform service IP
	resourceList, err := indexer.ByIndex(backupObjectResourceIndex, utils.Service)
	if err != nil {
		return fmt.Errorf("failed to retrieve resources from cache for "+
			"namespace mutation. %s", err)
	}

	for _, resource := range resourceList {
		object, ok := resource.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("restore index cache with invalid object type %v",
				reflect.TypeOf(resource))
		}

		var service corev1.Service
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &service)
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
		err = indexer.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"service mutation. %s", err)
		}

		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&service)
		if err != nil {
			return fmt.Errorf("failed to translate to unstructure during service mutation. %s", err)
		}
		object.Object = unstructuredMap

		// add new object in cache
		err = indexer.Add(object)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"service mutation. %s", err)
		}

	}

	return handler.next.handle(restore, indexer)
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
	for i, _ := range service.Spec.Ports {
		service.Spec.Ports[i].NodePort = 0
	}
}

type prefixMutation struct {
	logger log.FieldLogger
}

func newPrefixMutation(logger log.FieldLogger) mutationHandler {
	return &prefixMutation{
		logger: logger,
	}
}

func (handler *prefixMutation) isStatefulSetPresent(indexer cache.Indexer, prefix string) (bool, map[string]string) {
	indexedResources := indexer.List()
	statefulSetPVC := make(map[string]string, 0)
	var found bool
	for _, indexedResource := range indexedResources {
		unstructuredResource, ok := indexedResource.(*unstructured.Unstructured)
		if !ok {
			handler.logger.Warningf("Restore index cache has invalid object type. %v", reflect.TypeOf(unstructuredResource))
			continue
		}
		kind := unstructuredResource.GetKind()
		if kind == "StatefulSet" {
			found = true
			statefulSetName := unstructuredResource.GetName()
			replicas, found, err := unstructured.NestedInt64(unstructuredResource.UnstructuredContent(), "spec", "replicas")
			if !found || err != nil {
				handler.logger.Warningf("replicas section not found in spec")
				return false, statefulSetPVC
			}
			volumeClaimTemplates, found, err := unstructured.NestedSlice(unstructuredResource.UnstructuredContent(), "spec", "volumeClaimTemplates")
			if !found || err != nil {
				handler.logger.Warningf("volumeClaimTemplates section not found in spec")
				return false, statefulSetPVC
			}
			handler.logger.Infof("started adding prefix to volumes section")
			for _, v := range volumeClaimTemplates {
				claimName, found, err := unstructured.NestedString(v.(map[string]interface{}), "metadata", "name")
				if !found || err != nil {
					handler.logger.Warningf("pvcName not found!")
					return false, statefulSetPVC
				}
				for replica := 0; replica < int(replicas); replica++ {
					preparePvcName := claimName + "-" + statefulSetName + "-" + strconv.Itoa(replica)
					if _, ok := statefulSetPVC[preparePvcName]; !ok {
						sPVC := prefix + claimName + "-" + prefix + statefulSetName + "-" + strconv.Itoa(replica)
						statefulSetPVC[preparePvcName] = sPVC
					}
				}
			}
		}
	}
	return found, statefulSetPVC
}

func (handler *prefixMutation) handle(restore *kahuapi.Restore, indexer cache.Indexer) error {
	// if restore ResourcePrefix is empty then don't do any thing
	if restore.Spec.ResourcePrefix == "" {
		return nil
	}

	// get the prefixString
	prefixString := restore.Spec.ResourcePrefix

	indexedResources := indexer.List()
	unstructuredResources := make([]*unstructured.Unstructured, 0)
	isStatefulSetPresent, statefulSetPVC := handler.isStatefulSetPresent(indexer, prefixString)

	for _, indexedResource := range indexedResources {
		unstructuredResource, ok := indexedResource.(*unstructured.Unstructured)
		if !ok {
			handler.logger.Warningf("Restore index cache has invalid object type. %v",
				reflect.TypeOf(unstructuredResource))
			continue
		}

		// ignore CRDs
		if unstructuredResource.GetObjectKind().GroupVersionKind().Kind == crdName {
			continue
		}
		// get the resourcename and kind
		kind := unstructuredResource.GetKind()
		resourceName := unstructuredResource.GetName()

		handler.logger.Infof("AddPrefix: the resource kind:%s and name:%s", kind, resourceName)
		newName := prefixString + resourceName

		if kind == "ServiceAccount" && resourceName == "default" {
			handler.logger.Infof("serviceaccountname is default. So prefix will not be added")
			continue
		}

		// update the new name by adding prefix string
		_, ok = statefulSetPVC[resourceName]
		if kind == "PersistentVolumeClaim" && isStatefulSetPresent && ok {
			newName := statefulSetPVC[resourceName]
			handler.logger.Infof("setting pvc name for statefulset:%s", newName)
			unstructuredResource.SetName(newName)
		} else {
			unstructuredResource.SetName(newName)
		}

		// update the resource dependednt resource name also, for kind pod, deployment etc
		switch kind {
		case "Pod":
			unstructuredResource = handler.updatePodContent(prefixString, unstructuredResource)
			if unstructuredResource != nil {
				unstructuredResources = append(unstructuredResources, unstructuredResource)
			}
		case "PersistentVolumeClaim":
			unstructuredResource = handler.updatePVCContent(prefixString, unstructuredResource)
			if unstructuredResource != nil {
				unstructuredResources = append(unstructuredResources, unstructuredResource)
			}
		case "Deployment", "DaemonSet", "ReplicaSet":
			unstructuredResource = handler.updateDeploymentContent(prefixString, unstructuredResource)
			if unstructuredResource != nil {
				unstructuredResources = append(unstructuredResources, unstructuredResource)
			}
		case "StatefulSet":
			unstructuredResource = handler.updateStatefulSetContent(prefixString, unstructuredResource)
			if unstructuredResource != nil {
				unstructuredResources = append(unstructuredResources, unstructuredResource)
			}
		default:
			continue
		}

		newObject := unstructuredResource.DeepCopy()

		// delete old cached object
		err := indexer.Delete(indexedResource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"prefix mutation. %s", err)
		}

		// add new object in cache
		err = indexer.Add(newObject)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"prefix mutation. %s", err)
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

func (handler *restoreIdentificationMutation) handle(restore *kahuapi.Restore, indexer cache.Indexer) error {
	// Add restore identification annotation to all the resources being restored
	handler.logger.Infoln("Adding restore identifier annotation for the resources being restored")
	indexedResources := indexer.List()

	for _, indexedResource := range indexedResources {
		unstructuredResource, ok := indexedResource.(*unstructured.Unstructured)
		if !ok {
			handler.logger.Warningf("Restore index cache has invalid object type. %v",
				reflect.TypeOf(unstructuredResource))
			continue
		}

		// ignore CRDs
		if unstructuredResource.GetObjectKind().GroupVersionKind().Kind == crdName {
			continue
		}
		// get the resourcename and kind
		kind := unstructuredResource.GetKind()
		resourceName := unstructuredResource.GetName()

		if kind == "ServiceAccount" && resourceName == "default" {
			handler.logger.Infof("service account name is default. So not mutated")
			continue
		}

		annots := unstructuredResource.GetAnnotations()
		if annots == nil {
			annots = make(map[string]string, 0)
		}
		annots[annRestoreIdentifier] = restore.Name

		unstructuredResource.SetAnnotations(annots)

		newObject := unstructuredResource.DeepCopy()

		// delete old cached object
		err := indexer.Delete(indexedResource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"restore identification mutation. %s", err)
		}

		// add new object in cache
		err = indexer.Add(newObject)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"restore identification mutation. %s", err)
		}

	}
	return handler.next.handle(restore, indexer)
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

func (handler *pvcMutation) handle(restore *kahuapi.Restore, indexer cache.Indexer) error {
	// get all restore PVC
	resourceList, err := indexer.ByIndex(backupObjectResourceIndex, utils.PVC)
	if err != nil {
		return fmt.Errorf("failed to retrieve resources from cache for "+
			"pvc mutation. %s", err)
	}

	for _, resource := range resourceList {
		object, ok := resource.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("restore index cache with invalid object type %v",
				reflect.TypeOf(resource))
		}

		var pvc corev1.PersistentVolumeClaim
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &pvc)
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
		err = indexer.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"pvc mutation. %s", err)
		}

		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pvc)
		if err != nil {
			return fmt.Errorf("failed to translate to unstructure during pvc mutation. %s", err)
		}
		object.Object = unstructuredMap

		// add new object in cache
		err = indexer.Add(object)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"pvc mutation. %s", err)
		}

	}

	return handler.next.handle(restore, indexer)
}

type roleBindingMutation struct {
	next mutationHandler
}

func newRoleBindingMutator(next mutationHandler) mutationHandler {
	return &roleBindingMutation{
		next: next,
	}
}

func (handler *roleBindingMutation) handle(restore *kahuapi.Restore, indexer cache.Indexer) error {
	// extract RoleBinding resources
	resourceList, err := indexer.ByIndex(backupObjectResourceIndex, utils.RoleBinding)
	if err != nil {
		return fmt.Errorf("failed to retrieve resources from cache for "+
			"role binding mutation. %s", err)
	}

	for _, resource := range resourceList {
		object, ok := resource.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("restore index cache with invalid object type %v",
				reflect.TypeOf(resource))
		}

		var rolBinding rbacv1.RoleBinding
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &rolBinding)
		if err != nil {
			return fmt.Errorf("failed to translate to service for mutation. %s", err)
		}

		mutateSubjectNamespace(rolBinding.Subjects, restore.Spec.NamespaceMapping)
		// mutate subject name if prefix
		mutateSubjectName(rolBinding.Subjects, restore.Spec.ResourcePrefix)
		// mutate role ref name if prefix
		if restore.Spec.ResourcePrefix != "" && rolBinding.RoleRef.Kind == utils.Role {
			rolBinding.RoleRef.Name = restore.Spec.ResourcePrefix + rolBinding.RoleRef.Name
		}

		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&rolBinding)
		if err != nil {
			return fmt.Errorf("failed to translate to unstructure during service mutation. %s", err)
		}
		object.Object = unstructuredMap

		// delete old cached object
		err = indexer.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"role binding resource mutation. %s", err)
		}

		// add new object in cache
		err = indexer.Add(object)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"role binding resource mutation. %s", err)
		}
	}

	return handler.next.handle(restore, indexer)
}

type clusterRoleBindingMutation struct {
	next mutationHandler
}

func newClusterRoleBindingMutator(next mutationHandler) mutationHandler {
	return &clusterRoleBindingMutation{
		next: next,
	}
}

func (handler *clusterRoleBindingMutation) handle(restore *kahuapi.Restore, indexer cache.Indexer) error {
	// extract ClusterRoleBinding resources
	resourceList, err := indexer.ByIndex(backupObjectResourceIndex, utils.ClusterRoleBinding)
	if err != nil {
		return fmt.Errorf("failed to retrieve resources from cache for "+
			"cluster role binding mutation. %s", err)
	}

	for _, resource := range resourceList {
		object, ok := resource.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("restore index cache with invalid object type %v",
				reflect.TypeOf(resource))
		}

		var clusterRoleBinding rbacv1.ClusterRoleBinding
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &clusterRoleBinding)
		if err != nil {
			return fmt.Errorf("failed to translate to service for mutation. %s", err)
		}

		// mutate subject
		mutateSubjectNamespace(clusterRoleBinding.Subjects, restore.Spec.NamespaceMapping)
		// mutate subject name if prefix
		mutateSubjectName(clusterRoleBinding.Subjects, restore.Spec.ResourcePrefix)
		// mutate role ref name if prefix
		if restore.Spec.ResourcePrefix != "" && clusterRoleBinding.RoleRef.Kind == utils.ClusterRole {
			clusterRoleBinding.RoleRef.Name = restore.Spec.ResourcePrefix + clusterRoleBinding.RoleRef.Name
		}

		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&clusterRoleBinding)
		if err != nil {
			return fmt.Errorf("failed to translate to unstructure during service mutation. %s", err)
		}
		object.Object = unstructuredMap

		// delete old cached object
		err = indexer.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"cluster role binding resource mutation. %s", err)
		}

		// add new object in cache
		err = indexer.Add(object)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"cluster role binding resource mutation. %s", err)
		}
	}

	return handler.next.handle(restore, indexer)
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

func mutateSubjectName(subjects []rbacv1.Subject, prefix string) {
	if prefix == "" {
		return
	}
	for i, subject := range subjects {
		if subject.Kind == utils.ServiceAccount && subject.Name != "default" {
			subjects[i].Name = prefix + subject.Name
		}
	}
}
