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

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"github.com/soda-cdm/kahu/utils"
)

type Interface interface {
	Resolve(restoreObjects cache.Indexer, backupObjects cache.Indexer) error
}

type ResolverGetter interface {
	GetResolver(kind string) (Resolver, error)
}

type Resolver interface {
	resolve(restoreObjects cache.Indexer,
		backupObjects cache.Indexer,
		resource *unstructured.Unstructured) error
}

type restoreDependencyResolver struct {
	logger    log.FieldLogger
	resolvers map[string]Resolver
}

func NewResolver(logger log.FieldLogger) Interface {
	dependencyResolver := &restoreDependencyResolver{
		logger: logger,
	}
	dependencyResolver.resolvers = getAllResolvers(logger, dependencyResolver)
	return dependencyResolver
}

func (r *restoreDependencyResolver) Resolve(restoreObjects cache.Indexer, backupObjects cache.Indexer) error {
	resourceList := restoreObjects.List()
	for _, resource := range resourceList {
		var restoreResource *unstructured.Unstructured
		switch unstructuredResource := resource.(type) {
		case *unstructured.Unstructured:
			restoreResource = unstructuredResource
		case unstructured.Unstructured:
			restoreResource = unstructuredResource.DeepCopy()
		default:
			r.logger.Warningf("Unknown cached resource type. %s", reflect.TypeOf(resource))
			continue
		}

		resolver, err := r.GetResolver(restoreResource.GetKind())
		if err != nil {
			r.logger.Debugf("resolver not available for %s", restoreResource.GetKind())
			continue
		}
		err = resolver.resolve(restoreObjects, backupObjects, restoreResource)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("unable to resolve resource dependency for %s.%s",
				restoreResource.GetKind(), restoreResource.GetName()))
		}
	}
	return nil
}

func (r *restoreDependencyResolver) GetResolver(kind string) (Resolver, error) {
	resolver, ok := r.resolvers[kind]
	if !ok {
		r.logger.Infof("Resolver not available for %s", kind)
		return resolver, fmt.Errorf("resolver not available for %s", kind)
	}
	return resolver, nil
}

func getAllResolvers(logger log.FieldLogger, getter ResolverGetter) map[string]Resolver {
	return map[string]Resolver{
		utils.PV:  &pvResolver{logger: logger, getter: getter},
		utils.PVC: &pvcResolver{logger: logger, getter: getter},
	}
}

type pvResolver struct {
	logger log.FieldLogger
	getter ResolverGetter
}

func (r *pvResolver) resolve(restoreObjects cache.Indexer,
	backupObjects cache.Indexer,
	resource *unstructured.Unstructured) error {

	var pv v1.PersistentVolume
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &pv)
	if err != nil {
		r.logger.Errorf("Failed to translate unstructured (%s) to "+
			"pv. %s", resource.GetName(), err)
		return errors.Wrap(err, "Failed to covert unstructured resource to PV for resolution")
	}

	if pv.Spec.ClaimRef == nil { // not bound to any PVC
		return nil
	}

	pvcRef := pv.Spec.ClaimRef

	pvcResources, err := backupObjects.ByIndex(backupObjectResourceIndex, pvcRef.Kind)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("PVC does not exist in cache for PV %s", pv.Name))
	}

	isExist := false
	var pvc *unstructured.Unstructured
	for _, unStructuredpvc := range pvcResources {
		switch unstructuredResource := unStructuredpvc.(type) {
		case *unstructured.Unstructured:
			pvc = unstructuredResource
		case unstructured.Unstructured:
			pvc = unstructuredResource.DeepCopy()
		default:
			r.logger.Warningf("Unknown cached resource type. %s", reflect.TypeOf(resource))
			continue
		}
		if pvc.GetNamespace() == pvcRef.Namespace &&
			pvc.GetName() == pvcRef.Name {
			err := restoreObjects.Add(pvc)
			if err != nil {
				return err
			}
			isExist = true
			break
		}
	}

	if !isExist {
		return fmt.Errorf("PVC does not exist in cache for PV %s", pv.Name)
	}

	pvcResolver, err := r.getter.GetResolver(pvc.GetKind())
	if err != nil {
		return errors.Wrap(err, "PVC resolver not found")
	}

	return pvcResolver.resolve(restoreObjects, backupObjects, pvc)
}

type pvcResolver struct {
	logger log.FieldLogger
	getter ResolverGetter
}

func (r *pvcResolver) resolve(restoreObjects cache.Indexer,
	backupObjects cache.Indexer,
	resource *unstructured.Unstructured) error {

	var pvc v1.PersistentVolumeClaim
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &pvc)
	if err != nil {
		r.logger.Errorf("Failed to translate unstructured (%s) to "+
			"pv. %s", resource.GetName(), err)
		return errors.Wrap(err, "Failed to covert unstructured resource to PVC for resolution")
	}

	if pvc.Spec.StorageClassName == nil { // PVC not create with any Storage class.
		return nil
	}

	storageClassName := *pvc.Spec.StorageClassName

	scResource, err := backupObjects.ByIndex(backupObjectResourceIndex, utils.SC)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("SC does not exist in cache for PVC %s", pvc.Name))
	}

	isExist := false
	var sc *unstructured.Unstructured
	for _, unStructuredpvc := range scResource {
		switch unstructuredResource := unStructuredpvc.(type) {
		case *unstructured.Unstructured:
			sc = unstructuredResource
		case unstructured.Unstructured:
			sc = unstructuredResource.DeepCopy()
		default:
			r.logger.Warningf("Unknown cached resource type. %s", reflect.TypeOf(resource))
			continue
		}
		if sc.GetName() == storageClassName {
			err := restoreObjects.Add(sc)
			if err != nil {
				return err
			}
			isExist = true
			break
		}
	}

	if !isExist {
		return fmt.Errorf("SC does not exist in cache for PVC %s", pvc.Name)
	}

	return nil
}
