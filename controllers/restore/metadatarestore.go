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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"github.com/soda-cdm/kahu/utils"
)

const (
	crdName = "CustomResourceDefinition"
)

var (
	excludedResources = sets.NewString(
		"Node",
		"Namespace",
		"Event",
	)
)

type backupInfo struct {
	backup         *kahuapi.Backup
	backupLocation *kahuapi.BackupLocation
	backupProvider *kahuapi.Provider
}

func (ctx *restoreContext) processMetadataRestore(restore *kahuapi.Restore) error {
	// fetch backup info
	backupInfo, err := ctx.fetchBackupInfo(restore)
	if err != nil {
		return err
	}

	// construct backup identifier
	backupIdentifier, err := utils.GetBackupIdentifier(backupInfo.backup,
		backupInfo.backupLocation,
		backupInfo.backupProvider)
	if err != nil {
		return err
	}

	// fetch backup content and cache them
	err = ctx.fetchBackupContent(backupInfo.backupProvider, backupIdentifier, restore)
	if err != nil {
		restore.Status.Phase = kahuapi.RestorePhaseFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to get backup content. %s",
			err)
		restore, err = ctx.updateRestoreStatus(restore)
		return err
	}

	// filter resources from cache
	err = ctx.filter.handle(restore)
	if err != nil {
		restore.Status.Phase = kahuapi.RestorePhaseFailed
		errMsg := fmt.Sprintf("Failed to filter resources. %s", err)
		restore.Status.FailureReason = errMsg
		restore, err = ctx.updateRestoreStatus(restore)
		return err
	}

	// add mutation
	err = ctx.mutator.handle(restore)
	if err != nil {
		restore.Status.Phase = kahuapi.RestorePhaseFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to mutate resources. %s", err)
		restore, err = ctx.updateRestoreStatus(restore)
		return err
	}

	// process CRD resource first
	err = ctx.applyCRD()
	if err != nil {
		restore.Status.Phase = kahuapi.RestorePhaseFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to apply CRD resources. %s", err)
		restore, err = ctx.updateRestoreStatus(restore)
		return err
	}

	// process resources
	err = ctx.applyIndexedResource()
	if err != nil {
		restore.Status.Phase = kahuapi.RestorePhaseFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to apply resources. %s", err)
		restore, err = ctx.updateRestoreStatus(restore)
		return err
	}

	restore.Status.Phase = kahuapi.RestorePhaseCompleted
	restore, err = ctx.updateRestoreStatus(restore)
	return err
}

func (ctx *restoreContext) fetchBackup(restore *kahuapi.Restore) (*kahuapi.Backup, error) {
	// fetch backup
	backupName := restore.Spec.BackupName
	backup, err := ctx.backupLister.Get(backupName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("Backup(%s) do not exist", backupName)
			return nil, err
		}
		ctx.logger.Errorf("Failed to get backup. %s", err)
		return nil, err
	}

	return backup, err
}

func (ctx *restoreContext) fetchBackupLocation(locationName string,
	restore *kahuapi.Restore) (*kahuapi.BackupLocation, error) {
	// fetch backup location
	backupLocation, err := ctx.backupLocationLister.Get(locationName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("Backup location(%s) do not exist", locationName)
			return nil, err
		}
		ctx.logger.Errorf("Failed to get backup location. %s", err)
		return nil, err
	}

	return backupLocation, err
}

func (ctx *restoreContext) fetchProvider(providerName string,
	restore *kahuapi.Restore) (*kahuapi.Provider, error) {
	// fetch provider
	provider, err := ctx.providerLister.Get(providerName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("Metadata Provider(%s) do not exist", providerName)
			return nil, err
		}
		ctx.logger.Errorf("Failed to get metadata provider. %s", err)
		return nil, err
	}

	return provider, nil
}

func (ctx *restoreContext) fetchBackupInfo(restore *kahuapi.Restore) (*backupInfo, error) {
	backup, err := ctx.fetchBackup(restore)
	if err != nil {
		ctx.logger.Errorf("Failed to get backup information for backup(%s). %s",
			restore.Spec.BackupName, err)
		return nil, err
	}

	backupLocation, err := ctx.fetchBackupLocation(backup.Spec.MetadataLocation, restore)
	if err != nil {
		ctx.logger.Errorf("Failed to get backup location information for %s. %s",
			backup.Spec.MetadataLocation, err)
		return nil, err
	}

	provider, err := ctx.fetchProvider(backupLocation.Spec.ProviderName, restore)
	if err != nil {
		ctx.logger.Errorf("Failed to get backup location provider for %s. %s",
			backupLocation.Spec.ProviderName, err)
		return nil, err
	}

	return &backupInfo{
		backup:         backup,
		backupLocation: backupLocation,
		backupProvider: provider,
	}, nil
}

func (ctx *restoreContext) fetchMetaServiceClient(backupProvider *kahuapi.Provider,
	restore *kahuapi.Restore) (metaservice.MetaServiceClient, error) {
	if backupProvider.Spec.Type != kahuapi.ProviderTypeMetadata {
		return nil, fmt.Errorf("invalid metadata provider type (%s)",
			backupProvider.Spec.Type)
	}

	// fetch service name
	providerService, exist := backupProvider.Annotations[utils.BackupLocationServiceAnnotation]
	if !exist {
		return nil, fmt.Errorf("failed to get metadata provider(%s) service info",
			backupProvider.Name)
	}

	metaServiceClient, err := metaservice.GetMetaServiceClient(providerService)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata service client(%s)",
			providerService)
	}

	return metaServiceClient, nil
}

func (ctx *restoreContext) fetchBackupContent(backupProvider *kahuapi.Provider,
	backupIdentifier *metaservice.BackupIdentifier,
	restore *kahuapi.Restore) error {
	// fetch meta service client
	metaServiceClient, err := ctx.fetchMetaServiceClient(backupProvider, restore)
	if err != nil {
		ctx.logger.Errorf("Error fetching meta service client. %s", err)
		return err
	}

	// fetch metadata backup file
	restoreClient, err := metaServiceClient.Restore(context.TODO(), &metaservice.RestoreRequest{
		Id: backupIdentifier,
	})
	if err != nil {
		ctx.logger.Errorf("Error fetching meta service restore client. %s", err)
		return fmt.Errorf("error fetching meta service restore client. %s", err)
	}

	for {
		res, err := restoreClient.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Errorf("Failed fetching data. %s", err)
			break
		}

		obj := new(unstructured.Unstructured)
		err = json.Unmarshal(res.GetBackupResource().GetData(), obj)
		if err != nil {
			log.Errorf("Failed to unmarshal on backed up data %s", err)
			continue
		}

		ctx.logger.Infof("Received %s/%s from meta service",
			obj.GroupVersionKind(),
			obj.GetName())
		if ctx.excludeResource(obj) {
			ctx.logger.Infof("Excluding %s/%s from processing",
				obj.GroupVersionKind(),
				obj.GetName())
			continue
		}

		err = ctx.backupObjectIndexer.Add(obj)
		if err != nil {
			ctx.logger.Errorf("Unable to add resource %s/%s in restore cache. %s",
				obj.GroupVersionKind(),
				obj.GetName(),
				err)
			return err
		}
	}
	restoreClient.CloseSend()

	return nil
}

func (ctx *restoreContext) excludeResource(resource *unstructured.Unstructured) bool {
	if excludedResources.Has(resource.GetKind()) {
		return true
	}

	switch resource.GetKind() {
	case "Service":
		if resource.GetName() == "kubernetes" {
			return true
		}
	}

	return false
}

func (ctx *restoreContext) applyCRD() error {
	crds, err := ctx.backupObjectIndexer.ByIndex(backupObjectResourceIndex, crdName)
	if err != nil {
		ctx.logger.Errorf("error fetching CRDs from indexer %s", err)
		return err
	}

	unstructuredCRDs := make([]*unstructured.Unstructured, 0)
	for _, crd := range crds {
		ubstructure, ok := crd.(*unstructured.Unstructured)
		if !ok {
			ctx.logger.Warningf("Restore index cache has invalid object type. %v",
				reflect.TypeOf(crd))
			continue
		}
		unstructuredCRDs = append(unstructuredCRDs, ubstructure)
	}

	for _, unstructuredCRD := range unstructuredCRDs {
		err := ctx.applyResource(unstructuredCRD)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ctx *restoreContext) applyIndexedResource() error {
	indexedResources := ctx.backupObjectIndexer.List()

	unstructuredResources := make([]*unstructured.Unstructured, 0)
	for _, indexedResource := range indexedResources {
		unstructuredResource, ok := indexedResource.(*unstructured.Unstructured)
		if !ok {
			ctx.logger.Warningf("Restore index cache has invalid object type. %v",
				reflect.TypeOf(unstructuredResource))
			continue
		}

		// ignore CRDs
		if unstructuredResource.GetObjectKind().GroupVersionKind().Kind == crdName {
			continue
		}
		unstructuredResources = append(unstructuredResources, unstructuredResource)
	}

	for _, unstructuredResource := range unstructuredResources {
		ctx.logger.Infof("Processing %s/%s for restore",
			unstructuredResource.GroupVersionKind(),
			unstructuredResource.GetName())
		err := ctx.applyResource(unstructuredResource)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ctx *restoreContext) applyResource(resource *unstructured.Unstructured) error {
	gvk := resource.GroupVersionKind()
	gvr, _, err := ctx.discoveryHelper.ByGroupVersionKind(gvk)
	if err != nil {
		ctx.logger.Errorf("unable to fetch GroupVersionResource for %s", gvk)
		return err
	}

	resourceClient := ctx.dynamicClient.Resource(gvr).Namespace(resource.GetNamespace())

	err = ctx.preProcessResource(resource)
	if err != nil {
		ctx.logger.Errorf("unable to preprocess resource %s. %s", resource.GetName(), err)
		return err
	}

	existingResource, err := resourceClient.Get(context.TODO(), resource.GetName(), metav1.GetOptions{})
	if err == nil && existingResource != nil {
		// resource already exist. ignore creating
		ctx.logger.Warningf("ignoring %s.%s/%s restore. Resource already exist",
			existingResource.GetKind(),
			existingResource.GetAPIVersion(),
			existingResource.GetName())
		return nil
	}

	_, err = resourceClient.Create(context.TODO(), resource, metav1.CreateOptions{})
	if err != nil && apierrors.IsAlreadyExists(err) {
		// ignore if already exist
		return nil
	}
	return err
}

func (ctx *restoreContext) preProcessResource(resource *unstructured.Unstructured) error {
	// ensure namespace existence
	if err := ctx.ensureNamespace(resource); err != nil {
		return err
	}
	// remove resource version
	resource.SetResourceVersion("")

	// TODO(Amit Roushan): Add resource specific handling
	return nil
}

func (ctx *restoreContext) ensureNamespace(resource *unstructured.Unstructured) error {
	// check if namespace exist
	namespace := resource.GetNamespace()
	if namespace == "" {
		// ignore if namespace is empty
		// possibly cluster scope resource
		return nil
	}

	// check if namespace exist
	n, err := ctx.kubeClient.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
	if err == nil && n != nil {
		// namespace already exist
		return nil
	}

	// create namespace
	_, err = ctx.kubeClient.CoreV1().
		Namespaces().
		Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}, metav1.CreateOptions{})
	if err != nil {
		ctx.logger.Errorf("Unable to ensure namespace. %s", err)
		return err
	}

	return nil
}
