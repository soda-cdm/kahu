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
	"regexp"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuclient "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/informers/externalversions"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/controllers/restore/restorer"
	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/hooks"
	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"github.com/soda-cdm/kahu/utils"
	utilscache "github.com/soda-cdm/kahu/utils/cache"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

const (
	defaultRestoreWorkers = 50
)

type controller struct {
	ctx                  context.Context
	logger               log.FieldLogger
	genericController    controllers.Controller
	kubeClient           kubernetes.Interface
	dynamicClient        dynamic.Interface
	restoreClient        kahuclient.RestoreInterface
	discoveryHelper      discovery.DiscoveryHelper
	restoreLister        kahulister.RestoreLister
	backupLister         kahulister.BackupLister
	backupLocationLister kahulister.BackupLocationLister
	providerLister       kahulister.ProviderLister
	restoreVolumeClient  kahuclient.VolumeRestoreContentInterface
	podCommandExecutor   hooks.PodCommandExecutor
	podGetter            cache.Getter
	processedRestore     utils.Store
	framework            framework.Interface
	restorer             restorer.Interface
	filter               filterHandler
	mutator              mutationHandler
	// resolver             ResolveInterface
}

func NewController(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	kahuClient versioned.Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper,
	informer externalversions.SharedInformerFactory,
	podCommandExecutor hooks.PodCommandExecutor,
	podGetter cache.Getter,
	framework framework.Interface) (controllers.Controller, error) {

	logger := log.WithField("controller", controllerName)
	processedRestoreCache := utils.NewStore(utils.DeletionHandlingMetaNamespaceKeyFunc)
	restoreController := &controller{
		ctx:                  ctx,
		logger:               logger,
		kubeClient:           kubeClient,
		dynamicClient:        dynamicClient,
		discoveryHelper:      discoveryHelper,
		restoreClient:        kahuClient.KahuV1beta1().Restores(),
		restoreLister:        informer.Kahu().V1beta1().Restores().Lister(),
		backupLister:         informer.Kahu().V1beta1().Backups().Lister(),
		backupLocationLister: informer.Kahu().V1beta1().BackupLocations().Lister(),
		providerLister:       informer.Kahu().V1beta1().Providers().Lister(),
		restoreVolumeClient:  kahuClient.KahuV1beta1().VolumeRestoreContents(),
		podCommandExecutor:   podCommandExecutor,
		podGetter:            podGetter,
		processedRestore:     processedRestoreCache,
		framework:            framework,
		filter:               newRestoreSpecFilterHandler(),
		mutator:              newRestoreResourceMutationHandler(),
		// resolver:             newResolver(),
	}

	// construct controller interface to process worker queue
	genericController, err := controllers.NewControllerBuilder(controllerName).
		SetLogger(logger).
		SetHandler(restoreController.processQueue).
		Build()
	if err != nil {
		return nil, err
	}

	// register to informer to receive events and push events to worker queue
	informer.Kahu().V1beta1().Restores().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: genericController.Enqueue,
			UpdateFunc: func(oldObj, newObj interface{}) {
				genericController.Enqueue(newObj)
			},
		},
	)

	// reference back
	restoreController.genericController = genericController
	restoreController.restorer = restorer.NewRestorer(logger, restoreController.processRestore)

	go restoreController.restorer.Run(ctx, defaultRestoreWorkers)
	return genericController, err
}

func (ctrl *controller) processQueue(index string) error {
	// get restore name
	_, name, err := cache.SplitMetaNamespaceKey(index)
	if err != nil {
		ctrl.logger.Errorf("splitting key into namespace and name, error %s", err)
		return err
	}

	// get restore from cache
	restore, err := ctrl.restoreLister.Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Debugf("Restore %s not found", name)
			return nil
		}
		ctrl.logger.Errorf("Getting the restore resource from lister, Error %s", err)
		return err
	}

	ctrl.logger.Infof("Processing restore for %s", restore.Name)
	newObj, err := utils.StoreRevisionUpdate(ctrl.processedRestore, restore, "Restore")
	if err != nil {
		ctrl.logger.Errorf("%s", err)
	}
	if !newObj {
		return nil
	}

	if restore.DeletionTimestamp != nil {
		ctrl.restorer.Enqueue(restore.Name)
		return nil
	}

	if !utils.ContainsFinalizer(restore, restoreFinalizer) {
		updatedRestore := restore.DeepCopy()
		utils.SetFinalizer(updatedRestore, restoreFinalizer)
		restore, err = ctrl.patchRestore(restore, updatedRestore)
		if err != nil {
			ctrl.logger.Error("Unable to update finalizer for restore")
			return err
		}
	}

	// handle restore with stages
	switch restore.Status.State {
	case "":
		restore, err = ctrl.updateRestoreState(restore, kahuapi.RestoreStateNew)
		if err != nil {
			return err
		}
		fallthrough
	case kahuapi.RestoreStateNew:
		ctrl.logger.Infof("Restore in %s phase", kahuapi.RestoreStateNew)
		if restore, err = ctrl.initRestore(restore); err != nil {
			return err
		}
		restore, err = ctrl.updateRestoreState(restore, kahuapi.RestoreStateValidating)
		if err != nil {
			return err
		}
		fallthrough
	case kahuapi.RestoreStateValidating:
		// validate restore
		if err = ctrl.validateRestore(restore); err != nil {
			return err
		}
		ctrl.logger.Info("Restore specification validation success")
		restore, err = ctrl.updateRestoreState(restore, kahuapi.RestoreStateProcessing)
		if err != nil {
			return err
		}
	}

	ctrl.restorer.Enqueue(restore.Name)
	return nil
}

func (ctrl *controller) deleteRestore(restore *kahuapi.Restore) error {
	ctrl.logger.Infof("Deleting restore %s", restore.Name)
	restore = restore.DeepCopy()
	//err := ctrl.removeVolumeRestore(restore)
	//if err != nil {
	//	ctrl.logger.Errorf("Unable to delete volume backup. %s", err)
	//	return err
	//}

	//// If restore is not fully successful, trigger resource cleanup
	//if !(restore.Status.State == kahuapi.RestoreStateCompleted &&
	//	restore.Status.Stage == kahuapi.RestoreStageFinished) {
	//	// cleanup metadata except pv/pvc which has restoreName annotation
	//	_, err = ctx.cleanupRestoredMetadata(restore)
	//	if err != nil {
	//		ctx.logger.Errorf("Unable to cleanup restored metadata for restore %s. Reason: %s", restore.Name, err)
	//		return errors.Wrap(err, "Unable to cleanup restored metadata")
	//	}
	//}
	//
	//// check if all volume backup contents are deleted
	//vrcList, err := ctrl.restoreVolumeClient.List(context.TODO(), metav1.ListOptions{
	//	LabelSelector: labels.Set{volumeContentRestoreLabel: restore.Name}.AsSelector().String()})
	//if err != nil {
	//	ctrl.logger.Errorf("Unable to get volume restore list. %s", err)
	//	return err
	//}
	//if len(vrcList.Items) > 0 {
	//	ctrl.logger.Infoln("Volume restore list is not empty. Continue to wait for Volume backup delete")
	//	return nil
	//}
	//
	//ctrl.logger.Info("Volume restore deleted successfully")
	//
	restoreClone := restore.DeepCopy()
	utils.RemoveFinalizer(restoreClone, restoreFinalizer)
	_, err := ctrl.patchRestore(restore, restoreClone)
	if err != nil {
		ctrl.logger.Errorf("removing finalizer failed for %s", restore.Name)
	}

	err = utils.StoreClean(ctrl.processedRestore, restore, "Restore")
	if err != nil {
		ctrl.logger.Warningf("Failed to clean processed cache. %s", err)
	}
	return err
}

func (ctrl *controller) initRestore(restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	// update start time
	if restore.Status.StartTimestamp == nil {
		time := metav1.Now()
		restore.Status.StartTimestamp = &time
	}

	if restore.Status.State == "" {
		restore.Status.State = kahuapi.RestoreStateNew
	}

	newRestore, err := ctrl.updateRestoreStatus(restore)
	if err != nil {
		ctrl.logger.Error("Unable to update initial restore state")
		return restore, err
	}

	return newRestore, err
}

func (ctrl *controller) needProcessing(restore *kahuapi.Restore) bool {
	return restore.Status.State != kahuapi.RestoreStateFailed &&
		restore.Status.State != kahuapi.RestoreStateCompleted &&
		restore.Status.State != kahuapi.RestoreStateDeleting
}

// processRestore perform restoration of resources
// Restoration process are divided into multiple steps and each step are based on restore resource state
// Only restore object are passed on in Phases. The mechanism will help in failure/crash scenario
func (ctrl *controller) processRestore(key string) error {
	var err error
	// get restore name
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ctrl.logger.Errorf("splitting key into namespace and name, error %s", err)
		return err
	}

	// get restore from cache
	restore, err := ctrl.restoreLister.Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Debugf("Restore %s not found", name)
			return nil
		}
		ctrl.logger.Errorf("Getting the restore resource from lister, Error %s", err)
		return err
	}

	if restore.DeletionTimestamp != nil {
		return ctrl.deleteRestore(restore)
	}

	// do not process if processed already
	if !ctrl.needProcessing(restore) {
		return nil
	}

	restore = restore.DeepCopy()
	depTree := make(map[string]sets.String)
	restore, resourceCache, err := ctrl.getRestoreResources(restore, depTree)
	if err != nil {
		return err
	}

	// sort resource on restore priority
	resources := ctrl.prioritiseResources(resourceCache.List())
	// restore resources
	if restore, err = ctrl.restoreResources(restore, resources, resourceCache, depTree); err != nil {
		return err
	}

	// update restore state as completed
	restore.Status.State = kahuapi.RestoreStateCompleted
	_, err = ctrl.updateRestoreStatus(restore)
	return err
}

func (ctrl *controller) getRestoreResources(
	restore *kahuapi.Restore,
	depTree map[string]sets.String) (*kahuapi.Restore, utilscache.Interface, error) {
	var err error
	backupResources := utilscache.NewCache()
	if restore, err = ctrl.populateBackupResources(restore, backupResources, depTree); err != nil {
		return restore, backupResources, err
	}

	restoreResources := backupResources.DeepCopy()
	// filter resources from backup resource based on restore spec
	err = ctrl.filter.handle(restore, restoreResources)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to filter resources. %s", err)
		restore, err = ctrl.updateRestoreStatus(restore)
		if err != nil {
			return restore, restoreResources, err
		}
		return restore, restoreResources, errors.New("Failed to filter resources from cache")
	}

	// resolve dependencies for restore candidates
	err = populateDependencies(restoreResources, backupResources, depTree)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to get dependency resources. %s", err)
		restore, err = ctrl.updateRestoreStatus(restore)
		if err != nil {
			return restore, restoreResources, err
		}
		return restore, restoreResources, errors.New("Failed to resolve dependencies")
	}

	// add mutation
	err = ctrl.mutator.handle(restore, restoreResources)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to mutate resources. %s", err)
		restore, err = ctrl.updateRestoreStatus(restore)
		if err != nil {
			return restore, restoreResources, err
		}
		return restore, restoreResources, errors.New("Failed to add mutation")
	}

	// update resolved resources in restore object
	if restore, err = ctrl.patchRestoreObjects(restore, restoreResources); err != nil {
		return restore, restoreResources, err
	}

	return restore, restoreResources, nil
}

func (ctrl *controller) validateRestore(restore *kahuapi.Restore) error {
	validationErrors := make([]string, 0)
	// namespace validation
	includeNamespaces := sets.NewString(restore.Spec.IncludeNamespaces...)
	excludeNamespaces := sets.NewString(restore.Spec.ExcludeNamespaces...)
	// check common namespace name in include/exclude list
	if intersection := includeNamespaces.Intersection(excludeNamespaces); intersection.Len() > 0 {
		validationErrors = append(validationErrors,
			fmt.Sprintf("common namespace name (%s) in include and exclude namespace list",
				strings.Join(intersection.List(), ",")))
	}

	// resource validation
	// check regular expression validity
	for _, resourceSpec := range restore.Spec.IncludeResources {
		if _, err := regexp.Compile(resourceSpec.Name); err != nil {
			validationErrors = append(validationErrors,
				fmt.Sprintf("invalid include resource specification name %s", resourceSpec.Name))
		}
	}

	for _, resourceSpec := range restore.Spec.ExcludeResources {
		if _, err := regexp.Compile(resourceSpec.Name); err != nil {
			validationErrors = append(validationErrors,
				fmt.Sprintf("invalid exclude resource specification name %s", resourceSpec.Name))
		}
	}

	if len(validationErrors) > 0 {
		ctrl.logger.Errorf("Restore validation failed. %s", strings.Join(validationErrors, ", "))
		restore.Status.ValidationErrors = validationErrors
		restore.Status.State = kahuapi.RestoreStateFailed
		_, err := ctrl.updateRestoreStatus(restore)
		if err != nil {
			return err
		}
		return errors.New("restore validation failed")
	}

	return nil
}

func (ctrl *controller) populateBackupResources(restore *kahuapi.Restore,
	resourceCache utilscache.Interface,
	depTree map[string]sets.String) (*kahuapi.Restore, error) {
	var err error
	backupName := restore.Spec.BackupName
	ctrl.logger.Infof("Populating backed[%s] up resources", backupName)

	backup, err := ctrl.backupLister.Get(backupName)
	if err != nil {
		ctrl.logger.Errorf("Backup[%s] not available. %s", backupName, err)
		if apierrors.IsNotFound(err) {
			restore, err = ctrl.updateRestoreState(restore, kahuapi.RestoreStateFailed)
			if err != nil {
				return restore, err
			}
			return restore, fmt.Errorf("backup[%s] not available", backupName)
		}
		return restore, err
	}

	blName := backup.Spec.MetadataLocation
	bl, err := ctrl.framework.Executors().ResourceBackupService(blName)
	if err != nil {
		ctrl.logger.Errorf("Failed to get backup location service. %s", err)
		return restore, err
	}

	blService, err := bl.Start(ctrl.ctx)
	if err != nil {
		ctrl.logger.Errorf("Failed to start backup location service. %s", err)
		return restore, err
	}
	defer bl.Done()
	defer blService.Close()

	// process backup resources
	fnDownload := func(resource *metaservice.Resource) error {
		if err := ctrl.processMetaServiceResource(resource, resourceCache, depTree); err != nil {
			return err
		}
		return nil
	}
	err = blService.Download(ctrl.ctx, backupName, fnDownload)
	if err != nil {
		ctrl.logger.Errorf("Failed to process backup resources. %s", err)
		return restore, err
	}

	return restore, nil
}

func (ctrl *controller) patchRestoreObjects(
	restore *kahuapi.Restore,
	restoreResources utilscache.Interface) (*kahuapi.Restore, error) {
	resourceList := restoreResources.List()
	restoreObjects := make([]kahuapi.RestoreResource, 0)

	for _, resource := range resourceList {
		restoreObjects = append(restoreObjects, kahuapi.RestoreResource{
			Namespace:    resource.GetNamespace(),
			ResourceName: resource.GetName(),
			TypeMeta: metav1.TypeMeta{
				APIVersion: resource.GetAPIVersion(),
				Kind:       resource.GetKind(),
			},
		})
	}

	newRestore := restore.DeepCopy()
	newRestore.Status.Resources = restoreObjects
	return ctrl.updateRestoreStatus(newRestore)
}

func (ctrl *controller) patchRestore(oldRestore,
	updatedRestore *kahuapi.Restore) (*kahuapi.Restore, error) {
	origBytes, err := json.Marshal(oldRestore)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original backup")
	}

	updatedBytes, err := json.Marshal(updatedRestore)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated backup")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for backup")
	}

	newRestore, err := ctrl.restoreClient.Patch(context.TODO(),
		oldRestore.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "error patching backup")
	}

	_, err = utils.StoreRevisionUpdate(ctrl.processedRestore, newRestore, "Restore")
	if err != nil {
		return newRestore, errors.Wrap(err, "Failed to updated processed restore cache")
	}

	return newRestore, nil
}

func (ctrl *controller) checkForRestoreResConflict(restore *kahuapi.Restore) error {
	//Resources := ctx.resolveObjects
	//resourceList := Resources.List()
	//var restoreResource *unstructured.Unstructured
	//
	//for _, resource := range resourceList {
	//	switch unstructuredResource := resource.(type) {
	//	case *unstructured.Unstructured:
	//		restoreResource = unstructuredResource
	//	case unstructured.Unstructured:
	//		restoreResource = unstructuredResource.DeepCopy()
	//	default:
	//		ctx.logger.Warningf("Unknown cached resource type. %s", reflect.TypeOf(resource))
	//		continue
	//	}
	//
	//	isResConflict := ctx.checkWorkloadConflict(restore, restoreResource)
	//	if isResConflict == true {
	//		msg := fmt.Sprintf("Resource(%s) kind(%s) namespace(%s) conflicts with existing resource label"+
	//			" in cluster ", restoreResource.GetName(), restoreResource.GetKind(), restoreResource.GetNamespace())
	//		ctx.logger.Errorf(msg)
	//		return errors.New(msg)
	//	}
	//}

	return nil
}

func (ctrl *controller) patchVolRestoreContent(oldVrc,
	updatedVrc *kahuapi.VolumeRestoreContent) (*kahuapi.VolumeRestoreContent, error) {
	origBytes, err := json.Marshal(oldVrc)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original vrc")
	}

	updatedBytes, err := json.Marshal(updatedVrc)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated vrc")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for vrc")
	}

	newVrc, err := ctrl.restoreVolumeClient.Patch(context.TODO(),
		oldVrc.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "error patching vrc")
	}

	return newVrc, nil
}

func (ctrl *controller) updateRestoreStatus(restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	// get restore status from lister
	updatedRestore, err := ctrl.restoreClient.UpdateStatus(context.TODO(), restore,
		v1.UpdateOptions{})
	if err != nil {
		return restore, err
	}

	_, err = utils.StoreRevisionUpdate(ctrl.processedRestore, updatedRestore, "Restore")
	if err != nil {
		return updatedRestore, errors.Wrap(err, "Failed to updated processed restore cache")
	}

	return updatedRestore, err
}

func (ctrl *controller) updateRestoreResourceState(resource k8sresource.Resource,
	status kahuapi.ResourceStatus,
	restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	var err error
	for i, restoreRes := range restore.Status.Resources {
		if resource.ResourceID() == restoreRes.ResourceID() {
			restore.Status.Resources[i].Status = status
			break
		}
	}

	restore, err = ctrl.restoreClient.UpdateStatus(ctrl.ctx, restore, metav1.UpdateOptions{})
	if err != nil {
		return restore, err
	}

	_, err = utils.StoreRevisionUpdate(ctrl.processedRestore, restore, "Restore")
	if err != nil {
		ctrl.logger.Warning("Failed to updated processed restore cache")
		return restore, nil
	}
	return restore, nil
}

func (ctrl *controller) updateRestoreState(restore *kahuapi.Restore,
	state kahuapi.RestoreState) (*kahuapi.Restore, error) {
	restore.Status.State = state
	return ctrl.updateRestoreStatus(restore)
}

func (ctrl *controller) processMetaServiceResource(resource *metaservice.Resource,
	resourceCache utilscache.Interface,
	depTree map[string]sets.String) error {
	if resource.GetK8SResource() != nil {
		if err := ctrl.cacheK8SResource(resource, resourceCache); err != nil {
			ctrl.logger.Errorf("Failed to cache k8s resources. %s", err)
			return err
		}
		// either k8s resource or meta resource
		return nil
	}

	ctrl.cacheDependencyTree(resource, depTree)
	return nil
}

func (ctrl *controller) cacheK8SResource(resource *metaservice.Resource,
	cache utilscache.Interface) error {
	obj := k8sresource.Resource{}
	err := json.Unmarshal(resource.Data, &obj)
	if err != nil {
		log.Errorf("Failed to unmarshal on backed up data %s", err)
		return errors.Wrap(err, "unable to unmarshal received resource meta")
	}

	ctrl.logger.Infof("Received %s/%s from meta service", obj.GroupVersionKind(), obj.GetName())
	if ctrl.excludeResource(obj) {
		ctrl.logger.Infof("Excluding %s/%s from processing", obj.GroupVersionKind(), obj.GetName())
		return nil
	}

	err = cache.Add(obj)
	if err != nil {
		ctrl.logger.Errorf("Unable to add resource %s/%s in restore cache. %s", obj.GroupVersionKind(),
			obj.GetName(), err)
		return err
	}
	return nil
}

func (ctrl *controller) cacheDependencyTree(resource *metaservice.Resource,
	depTree map[string]sets.String) {
	ctrl.logger.Infof("Received %s from meta service", resource.GetKey())
	if !strings.Contains(resource.GetKey(), ".children") {
		return
	}

	key := strings.TrimSuffix(resource.GetKey(), ".children")
	deps := strings.Split(string(resource.GetData()), ",")
	value := sets.NewString(deps...)
	depTree[key] = value
}

func (ctrl *controller) excludeResource(resource k8sresource.Resource) bool {
	if excludeResources.Has(resource.GetKind()) {
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

func populateDependencies(restoreObjects,
	backupObjects utilscache.Interface,
	depTree map[string]sets.String) error {
	for _, resource := range restoreObjects.List() {
		key, err := restoreObjects.GetKey(resource)
		if err != nil {
			log.Errorf("failed to get key during dependency resolution")
			return err
		}

		// check if any dependency record exists in depTree
		depKeys, ok := depTree[key]
		if !ok {
			continue
		}

		// get dependencies from backed up resources
		for _, depKey := range depKeys.List() {
			depResource, exist, err := backupObjects.GetByKey(depKey)
			if err != nil {
				log.Errorf("Issue in getting dependency. %s", err)
				return err
			}
			if !exist {
				log.Error("Dependency do not exist in backup resource")
				return errors.New("Dependency do not exist in backup resource")
			}

			err = restoreObjects.Add(depResource)
			if err != nil {
				log.Errorf("Issue in adding dependency. %s", err)
				return err
			}
		}
	}

	return nil
}
