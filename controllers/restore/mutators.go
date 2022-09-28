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

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
)

type mutationHandler interface {
	handle(restore *kahuapi.Restore, indexer cache.Indexer) error
}

func constructMutationHandler(logger log.FieldLogger) mutationHandler {
	return newNamespaceMutator(
		newServiceMutation(logger, newPrefixMutation(logger)))
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

		newObject := object.DeepCopy()
		err := unstructured.SetNestedField(newObject.Object, "", "spec", "clusterIP")
		if err != nil {
			return fmt.Errorf("failed to unset cluster IP in service mutation. %s", err)
		}

		err = unstructured.SetNestedSlice(newObject.Object, make([]interface{}, 0), "spec", "clusterIPs")
		if err != nil {
			return fmt.Errorf("failed to unset cluster IPs in service mutation. %s", err)
		}

		// delete old cached object
		err = indexer.Delete(resource)
		if err != nil {
			return fmt.Errorf("failed to delete resource from cache for "+
				"service mutation. %s", err)
		}

		// add new object in cache
		err = indexer.Add(newObject)
		if err != nil {
			return fmt.Errorf("failed to add resource in cache for "+
				"service mutation. %s", err)
		}
	}

	return handler.next.handle(restore, indexer)
}

type prefixMutation struct {
	logger log.FieldLogger
}

func newPrefixMutation(logger log.FieldLogger) mutationHandler {
	return &prefixMutation{
		logger: logger,
	}
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
		unstructuredResource.SetName(newName)

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
		case "Deployment", "DaemonSet", "ReplicaSet", "StatefulSet":
			unstructuredResource = handler.updateDeploymentContent(prefixString, unstructuredResource)
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
