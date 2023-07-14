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
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuscheme "github.com/soda-cdm/kahu/client/clientset/versioned/scheme"
	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	kahuinformer "github.com/soda-cdm/kahu/client/informers/externalversions"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/controllers/backup/backupper"
	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/framework/executor/resourcebackup"
	"github.com/soda-cdm/kahu/hooks"
	"github.com/soda-cdm/kahu/utils"
	utilcache "github.com/soda-cdm/kahu/utils/cache"
	"github.com/soda-cdm/kahu/utils/k8sresource"
	"github.com/soda-cdm/kahu/volume"
	"github.com/soda-cdm/kahu/volume/group"
)

const (
	defaultBackupper      = 50
	buildInfoFile         = "build"
	kubernetesVersionFile = "kubernetes-version"
	childrenFile          = "children"

	waitForSnapshotTimeout = 10 * time.Minute
)

type Config struct {
	SupportedResources string
}

type controller struct {
	ctx                context.Context
	config             *Config
	logger             log.FieldLogger
	genericController  controllers.Controller
	kubeClient         kubernetes.Interface
	dynamicClient      dynamic.Interface
	backupClient       kahuv1client.BackupInterface
	backupLister       kahulister.BackupLister
	blLister           kahulister.BackupLocationLister
	eventRecorder      record.EventRecorder
	discoveryHelper    discovery.DiscoveryHelper
	providerLister     kahulister.ProviderLister
	volumeBackupClient kahuv1client.VolumeBackupContentInterface
	hookExecutor       hooks.Hooks
	processedBackup    utils.Store
	volumeHandler      volume.Interface
	collector          resourceCollector
	backupper          backupper.Interface
	framework          framework.Interface
	//volFactory         volume.Interface
}

func NewController(
	ctx context.Context,
	config *Config,
	kubeClient kubernetes.Interface,
	kahuClient versioned.Interface,
	dynamicClient dynamic.Interface,
	informer kahuinformer.SharedInformerFactory,
	eventBroadcaster record.EventBroadcaster,
	discoveryHelper discovery.DiscoveryHelper,
	hookExecutor hooks.Hooks,
	volumeHandler volume.Interface,
	framework framework.Interface) (controllers.Controller, error) {

	logger := log.WithField("controller", controllerName)
	processedBackupCache := utils.NewStore(utils.DeletionHandlingMetaNamespaceKeyFunc)
	backupController := &controller{
		ctx:                ctx,
		logger:             logger,
		config:             config,
		kubeClient:         kubeClient,
		backupClient:       kahuClient.KahuV1beta1().Backups(),
		backupLister:       informer.Kahu().V1beta1().Backups().Lister(),
		blLister:           informer.Kahu().V1beta1().BackupLocations().Lister(),
		dynamicClient:      dynamicClient,
		discoveryHelper:    discoveryHelper,
		providerLister:     informer.Kahu().V1beta1().Providers().Lister(),
		volumeBackupClient: kahuClient.KahuV1beta1().VolumeBackupContents(),
		hookExecutor:       hookExecutor,
		processedBackup:    processedBackupCache,
		volumeHandler:      volumeHandler,
		collector:          NewResourceCollector(logger, dynamicClient, kubeClient, discoveryHelper),
		framework:          framework,
	}

	// construct controller interface to process worker queue
	genericController, err := controllers.NewControllerBuilder(controllerName).
		SetLogger(logger).
		SetHandler(backupController.processQueue).
		SetReSyncHandler(backupController.reSync).
		SetReSyncPeriod(defaultReSyncTimeLoop).
		Build()
	if err != nil {
		return nil, err
	}

	// register to informer to receive events and push events to worker queue
	informer.Kahu().
		V1beta1().
		Backups().
		Informer().
		AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc: genericController.Enqueue,
				UpdateFunc: func(oldObj, newObj interface{}) {
					genericController.Enqueue(newObj)
				},
			},
		)

	// initialize event recorder
	eventRecorder := eventBroadcaster.NewRecorder(kahuscheme.Scheme,
		corev1.EventSource{Component: controllerName})
	backupController.eventRecorder = eventRecorder

	// reference back
	backupController.genericController = genericController
	backupController.backupper = backupper.NewBackupper(logger, backupController.processBackup)

	// start backupper
	go backupController.backupper.Run(ctx, defaultBackupper)

	return genericController, err
}

func (ctrl *controller) reSync() {
	ctrl.logger.Info("Running soft reconciliation for backups")
	backups, err := ctrl.backupLister.List(labels.Everything())
	if err != nil {
		// re enqueue for processing
		ctrl.logger.Errorf("Unable to get backup list for re sync. %s", err)
		return
	}

	// enqueue all backup for soft reconciliation
	for _, backup := range backups {
		if backup.Status.State == kahuapi.BackupStateCompleted {
			continue
		}
		ctrl.genericController.Enqueue(backup)
	}
}

func (ctrl *controller) processQueue(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ctrl.logger.Errorf("splitting key into namespace and name, error %s", err)
		return err
	}

	ctrl.logger.Infof("Processing backup(%s) request", name)
	backup, err := ctrl.backupLister.Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Infof("Backup %s already deleted", name)
			return nil
		}
		// re enqueue for processing
		return errors.Wrap(err, fmt.Sprintf("error getting backup %s from lister", name))
	}

	// TODO: Add check for already processed backup
	newBackup := backup.DeepCopy()
	newObject, err := utils.StoreRevisionUpdate(ctrl.processedBackup, newBackup, "Backup")
	if err != nil {
		ctrl.logger.Errorf("%s", err)
	}
	if !newObject {
		return nil
	}

	if newBackup.DeletionTimestamp != nil {
		ctrl.backupper.Enqueue(backup.Name)
		return nil
	}

	switch newBackup.Status.State {
	case "", kahuapi.BackupStateNew:
		// setup finalizer if not present
		if isBackupInitNeeded(newBackup) {
			newBackup, err = ctrl.backupInitialize(newBackup)
			if err != nil {
				ctrl.logger.Errorf("failed to initialize finalizer backup(%s)", key)
				return err
			}
		}

		ctrl.logger.Infof("Validating backup(%s) specifications", backup.Name)
		isValid, err := ctrl.validateBackup(newBackup)
		if isValid != true {
			return nil
		}

		if newBackup, err = ctrl.ensureSupportedResourceList(newBackup); err != nil {
			return err
		}

		ctrl.logger.Infof("Backup spec validation successful")
		ctrl.broadcastEvent(newBackup, corev1.EventTypeNormal,
			EventValidationSuccess, "Backup spec validation success")

		// update backup progress
		if newBackup, err = ctrl.updateBackupStatus(newBackup, kahuapi.BackupStatus{
			State: kahuapi.BackupStateProcessing,
		}); err != nil {
			return err
		}
	}

	return ctrl.syncBackup(newBackup)
}

func (ctrl *controller) ensureSupportedResourceList(backup *kahuapi.Backup) (*kahuapi.Backup, error) {
	if utils.ContainsAnnotation(backup, AnnResourceSelector) {
		return backup, nil
	}

	if ctrl.config.SupportedResources != "" {
		newBackup := backup.DeepCopy()
		utils.SetAnnotation(newBackup, AnnResourceSelector, ctrl.config.SupportedResources)
		return ctrl.patchBackup(backup, newBackup)
	}

	return backup, nil
}

func (ctrl *controller) deleteBackup(backup *kahuapi.Backup) error {
	ctrl.logger.Infof("Initiating backup(%s) delete", backup.Name)

	//err := ctrl.removeVolumeBackup(backup)
	//if err != nil {
	//	ctrl.logger.Errorf("Unable to delete volume backup. %s", err)
	//	return err
	//}
	//
	//// check if all volume backup contents are deleted
	//vbsList, err := ctrl.volumeBackupClient.List(context.TODO(),
	//	metav1.ListOptions{LabelSelector: labels.Set{volumeContentBackupLabel: backup.Name}.AsSelector().String()})
	//if err != nil {
	//	ctrl.logger.Errorf("Unable to get volume backup list. %s", err)
	//	return err
	//}
	//if len(vbsList.Items) > 0 {
	//	ctrl.logger.Info("Volume backup list is not empty. Continue to wait for Volume backup delete")
	//	return nil
	//}
	//
	//ctrl.logger.Info("Volume backup deleted successfully")

	// remove backup based on policy
	// start backup location service
	if backup.Spec.ReclaimPolicy == kahuapi.ReclaimPolicyDelete {
		blService, err := ctrl.framework.Executors().ResourceBackupService(backup.Spec.MetadataLocation)
		if err != nil {
			return err
		}
		defer blService.Done()

		bl, err := blService.Start(ctrl.ctx)
		if err != nil {
			return err
		}
		defer bl.Close()

		err = bl.Delete(ctrl.ctx, backup.Name)
		if err != nil {
			ctrl.logger.Errorf("Unable to delete backup in backup location. %s", err)
			return err
		}
	}

	backupClone := backup.DeepCopy()
	utils.RemoveFinalizer(backupClone, backupFinalizer)
	utils.SetAnnotation(backupClone, annBackupCleanupDone, "true")
	backup, err := ctrl.patchBackup(backup, backupClone)
	if err != nil {
		ctrl.logger.Errorf("removing finalizer failed for %s", backupClone.Name)
	}

	err = utils.StoreClean(ctrl.processedBackup, backup, "Backup")
	if err != nil {
		ctrl.logger.Warningf("Failed to clean processed cache. %s", err)
	}
	return err
}

func ignoreBackupSync(backup *kahuapi.Backup) bool {
	return backup.Status.State == kahuapi.BackupStateDeleting ||
		backup.Status.State == kahuapi.BackupStateFailed ||
		backup.Status.State == kahuapi.BackupStateCompleted
}

func (ctrl *controller) syncBackup(backup *kahuapi.Backup) error {
	if ignoreBackupSync(backup) {
		return nil
	}

	// get resource from backup spec
	resources, err := ctrl.collector.FetchBySpec(backup)
	if err != nil {
		return err
	}

	// sync resource by backup specification with backup resource
	if !utils.ContainsAnnotation(backup, annBackupContentSynced) {
		backup, err = ctrl.syncBackupResources(resources, backup)
		if err != nil {
			return err
		}

		// patch with backup sync annotation
		newBackup := backup.DeepCopy()
		utils.SetAnnotation(newBackup, annBackupContentSynced, "true")
		backup, err = ctrl.patchBackup(backup, newBackup)
		if err != nil {
			return err
		}
	}

	ctrl.backupper.Enqueue(backup.Name)
	return nil
}

func (ctrl *controller) processBackup(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ctrl.logger.Errorf("splitting key into namespace and name, error %s", err)
		return err
	}

	ctrl.logger.Infof("Processing backup(%s) after sync", name)
	backup, err := ctrl.backupLister.Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Infof("Backup %s already deleted", name)
			return nil
		}
		// re enqueue for processing
		return errors.Wrap(err, fmt.Sprintf("error getting backup %s from lister", name))
	}

	if backup.DeletionTimestamp != nil {
		return ctrl.deleteBackup(backup)
	}

	if ignoreBackupSync(backup) {
		return nil
	}

	// get resource from backup spec
	resourceCache, err := ctrl.collector.FetchByStatus(backup)
	if err != nil {
		return err
	}

	// start backup location service
	blService, err := ctrl.framework.Executors().ResourceBackupService(backup.Spec.MetadataLocation)
	if err != nil {
		return err
	}
	defer blService.Done()
	bl, err := blService.Start(ctrl.ctx)
	if err != nil {
		return err
	}
	defer bl.Close()

	// start backup resources and its dependencies
	backup, err = ctrl.backupResources(resourceCache.List(), backup, resourceCache, bl)
	if err != nil {
		return err
	}

	// backup volume
	volumeResources, err := resourceCache.GetByGVK(k8sresource.PersistentVolumeClaimGVK)
	if err != nil {
		ctrl.logger.Errorf("Unable to get persistent volume resources from cache. %s", err)
		return err
	}
	backup, err = ctrl.processVolumeBackup(backup, volumeResources, bl)
	if err != nil {
		return err
	}

	// backup metadata information
	err = ctrl.backupEnvironmentInfo(backup.Name, bl)
	if err != nil {
		ctrl.logger.Errorf("Failed to backup environment info")
		return err
	}

	// update backup state as completed
	_, err = ctrl.updateBackupStatusWithEvent(backup, kahuapi.BackupStatus{
		State: kahuapi.BackupStateCompleted,
	}, corev1.EventTypeNormal, string(kahuapi.BackupStateCompleted), "Backup completed")
	return err
}

func (ctrl *controller) backupEnvironmentInfo(backupName string, bl resourcebackup.Interface) error {
	k8sVersion, err := ctrl.kubeClient.Discovery().ServerVersion()
	if err == nil {
		version, err := json.Marshal(k8sVersion)
		if err == nil {
			err = bl.UploadObject(ctrl.ctx, backupName, kubernetesVersionFile, version)
			if err != nil {
				ctrl.logger.Warningf("unable to upload kubernetes version. %s", err)
			}
		}
	}
	return bl.UploadObject(ctrl.ctx, backupName, buildInfoFile, []byte(utils.GetBuildInfo().String()))
}

func needBackup(resource k8sresource.Resource, backup *kahuapi.Backup) bool {
	for _, backupResource := range backup.Status.Resources {
		if backupResource.ResourceID() == resource.ResourceID() &&
			backupResource.Status == kahuapi.Completed {
			return false
		}
	}

	return true
}

func (ctrl *controller) backupResources(resources []k8sresource.Resource,
	backup *kahuapi.Backup,
	cache utilcache.Interface,
	bl resourcebackup.Interface) (*kahuapi.Backup, error) {
	var err error
	// process resources for backup
	for _, resource := range resources {
		backup, err = ctrl.backupResource(resource, backup, cache, bl)
		if err != nil {
			return backup, err
		}
	}

	return backup, err
}

func (ctrl *controller) backupResource(resource k8sresource.Resource,
	backup *kahuapi.Backup,
	cache utilcache.Interface,
	bl resourcebackup.Interface) (*kahuapi.Backup, error) {
	// check if already backed up
	if !needBackup(resource, backup) {
		return backup, nil
	}

	ctrl.logger.Infof("Backing up resource [%s] for %s", resource.ResourceID(), backup.Name)
	var err error
	backup, err = ctrl.updateResourceBackupState(resource, kahuapi.Processing, backup)
	if err != nil {
		return backup, err
	}

	updatedResource := resource
	var deps []k8sresource.Resource
	var depK8sResources []k8sresource.Resource
	plugins := ctrl.framework.PluginRegistry().GetPlugins(framework.PreBackup, resource.GroupVersionKind())
	for _, plugin := range plugins {
		ctrl.logger.Infof("Processing plugin [%s] for resource [%s] in backup %s", plugin.Name(),
			resource.ResourceID(), backup.Name)

		// call plugin
		updatedResource, deps, err = plugin.Execute(ctrl.ctx, framework.PreBackup, updatedResource)
		if err != nil {
			return backup, err
		}

		depK8sResources = append(depK8sResources, deps...)
	}

	if len(depK8sResources) > 0 {
		backup, err = ctrl.handleResourceDeps(backup, updatedResource, depK8sResources, cache, bl)
		if err != nil {
			return backup, err
		}
	}

	// ignore volumes, volume backup handled separately
	if resource.GetKind() == k8sresource.PersistentVolumeGVK.Kind {
		return backup, nil
	}

	err = bl.UploadK8SResource(ctrl.ctx, backup.Name, []k8sresource.Resource{updatedResource})
	if err != nil {
		return backup, err
	}

	ctrl.logger.Infof("Completed resource [%s] backup for %s", resource.ResourceID(), backup.Name)
	return ctrl.updateResourceBackupState(resource, kahuapi.Completed, backup)
}

func (ctrl *controller) handleResourceDeps(backup *kahuapi.Backup,
	resource k8sresource.Resource,
	deps []k8sresource.Resource,
	cache utilcache.Interface,
	bl resourcebackup.Interface) (*kahuapi.Backup, error) {
	// update cache with dependencies
	err := cache.AddChildren(resource, deps...)
	if err != nil {
		return backup, err
	}

	// sync backup with cache after dependencies
	backup, err = ctrl.syncBackupResources(cache, backup)
	if err != nil {
		return backup, err
	}
	// TODO: check if resource are pod, try calling hooks

	// handle pods dependencies
	backup, deps, err = ctrl.handlePodDeps(backup, resource, deps)
	if err != nil {
		return backup, err
	}

	// backup dependencies first
	backup, err = ctrl.backupResources(deps, backup, cache, bl)
	if err != nil {
		return backup, err
	}

	err = ctrl.uploadK8SResourceDepRelation(backup.Name, resource, deps, cache, bl)
	if err != nil {
		return backup, err
	}

	return backup, nil
}

func (ctrl *controller) handlePodDeps(backup *kahuapi.Backup,
	resource k8sresource.Resource,
	deps []k8sresource.Resource) (*kahuapi.Backup, []k8sresource.Resource, error) {
	if resource.GetKind() != k8sresource.PodGVK.Kind {
		return backup, deps, nil
	}

	// Currently only handle volume dependencies
	// filter volume dep
	volResources := make([]k8sresource.Resource, 0)
	for _, dep := range deps {
		if dep.GetKind() != k8sresource.PersistentVolumeClaimGVK.Kind {
			continue
		}
		volResources = append(volResources, dep)
	}

	// TODO: Add hook handling here

	var err error
	// handle volume deps for snapshot
	backup, podVolDeps, err := ctrl.handlePodVolumeSnapshot(backup, volResources)
	if err != nil {
		return backup, deps, err
	}

	// append volume deps
	deps = append(deps, podVolDeps...)

	return backup, deps, nil
}

func (ctrl *controller) handlePodVolumeSnapshot(backup *kahuapi.Backup,
	volumes []k8sresource.Resource) (*kahuapi.Backup, []k8sresource.Resource, error) {
	// get all pvcs
	pvcs, err := ctrl.getVolumes(backup, volumes)
	if err != nil {
		return backup, nil, err
	}

	volumeGroups, err := ctrl.volumeHandler.Group().ByPVC(pvcs, group.WithProvisioner())
	if err != nil {
		ctrl.logger.Errorf("Failed to ensure volume group. %s", err)
		if _, err = ctrl.updateBackupStatusWithEvent(backup, kahuapi.BackupStatus{
			State: kahuapi.BackupStateFailed,
		}, corev1.EventTypeWarning, EventVolumeSnapshotFailed,
			fmt.Sprintf("Failed to ensure volume group. %s", err)); err != nil {
			return backup, nil, errors.Wrap(err, "Volume backup update failed")
		}
		return backup, nil, errors.Wrap(err, "Failed to ensure volume group")
	}

	for _, volumeGroup := range volumeGroups {
		// check for snapshot support
		needSupport, err := ctrl.needSnapshot(volumeGroup.GetProvisionerName())
		if err != nil {
			return backup, nil, err
		}
		if !needSupport {
			continue
		}

		wait, err := ctrl.volumeHandler.Snapshot().ByVolumeGroup(backup.Name, volumeGroup)
		if err != nil {
			ctrl.logger.Errorf("Failed to ensure volume snapshot. %s", err)
			if _, err = ctrl.updateBackupStatusWithEvent(backup, kahuapi.BackupStatus{
				State: kahuapi.BackupStateFailed,
			}, corev1.EventTypeWarning, EventVolumeSnapshotFailed,
				fmt.Sprintf("Failed to ensure volume group. %s", err)); err != nil {
				return backup, nil, errors.Wrap(err, "Volume backup update failed")
			}
			return backup, nil, errors.Wrap(err, "Failed to ensure volume group")
		}

		// wait for external snapshotter to finish snapshotting
		if err = wait.WaitForSnapshotToReady(waitForSnapshotTimeout); err != nil {
			return backup, nil, err
		}
	}
	return backup, nil, nil
}

func (ctrl *controller) needSnapshot(provisionerName string) (bool, error) {
	provider, err := ctrl.volBackupProvider(provisionerName)
	if err != nil {
		return false, err
	}

	for _, flag := range provider.Spec.Flags {
		if flag == kahuapi.VolumeBackupNeedSnapshotSupport ||
			flag == kahuapi.VolumeBackupNeedVolumeSupport {
			return true, nil
		}
	}

	return false, nil
}

func (ctrl *controller) volBackupProvider(provisionerName string) (*kahuapi.Provider, error) {
	provider, err := ctrl.volumeHandler.Provider().GetNameByProvisioner(provisionerName)
	if err != nil {
		// try to get default volume backup provider
		provider, err = ctrl.volumeHandler.Provider().DefaultProvider()
		if err != nil {
			return nil, fmt.Errorf("volume backup provider not available for provisioner[%s]", provisionerName)
		}
	}

	volBackupProvider, err := ctrl.providerLister.Get(provider)
	if err != nil && apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("backup provider[%s] not available", provider)
	}
	if err != nil {
		return nil, fmt.Errorf("backup provider[%s] fetch error", provider)
	}

	return volBackupProvider, nil
}

func (ctrl *controller) backupLocation(provisionerName string,
	backup *kahuapi.Backup) (*kahuapi.BackupLocation, error) {
	provider, err := ctrl.volBackupProvider(provisionerName)
	if err != nil {
		return nil, err
	}

	for _, locationName := range backup.Spec.VolumeBackupLocations {
		location, err := ctrl.blLister.Get(locationName)
		if err != nil && apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("backup location[%s] not available", locationName)
		}
		if location.Spec.ProviderName == provider.Name {
			return location, nil
		}

	}

	// TODO: add to fetch default backup location

	return nil, fmt.Errorf("backup location not available for volume backup")
}

func (ctrl *controller) uploadK8SResourceDepRelation(backupName string,
	resource k8sresource.Resource,
	deps []k8sresource.Resource,
	cache utilcache.Interface,
	bl resourcebackup.Interface) error {

	resourceKey, err := cache.GetKey(resource)
	if err != nil {
		return err
	}

	depKeys := make([]string, 0)
	for _, dep := range deps {
		depKey, err := cache.GetKey(dep)
		if err != nil {
			return err
		}
		depKeys = append(depKeys, depKey)
	}

	err = bl.UploadObject(ctrl.ctx, backupName, depFileName(resourceKey), []byte(strings.Join(depKeys, ",")))
	if err != nil {
		return err
	}

	return nil
}

func depFileName(key string) string {
	return key + "." + childrenFile
}

func (ctrl *controller) updateResourceBackupState(resource k8sresource.Resource,
	state kahuapi.ResourceStatus,
	backup *kahuapi.Backup) (*kahuapi.Backup, error) {

	for i, backupRes := range backup.Status.Resources {
		if resource.ResourceID() == backupRes.ResourceID() {
			backup.Status.Resources[i].Status = state
			return ctrl.backupClient.UpdateStatus(ctrl.ctx, backup, metav1.UpdateOptions{})
		}
	}

	return backup, nil
}

func (ctrl *controller) syncBackupResources(resourceCache utilcache.Interface,
	backup *kahuapi.Backup) (*kahuapi.Backup, error) {
	// collect existing resources
	existingResources := sets.NewString()
	for _, resource := range backup.Status.Resources {
		existingResources.Insert(resource.ResourceID())
	}

	for _, resource := range resourceCache.List() {
		// ignore existing resource
		if existingResources.Has(resource.ResourceID()) {
			continue
		}
		ctrl.logger.Infof("Sync resource[%s] in backup[%s]", resource.ResourceID(), backup.Name)
		backup.Status.Resources = append(backup.Status.Resources, kahuapi.BackupResource{
			TypeMeta: metav1.TypeMeta{
				APIVersion: resource.GetAPIVersion(),
				Kind:       resource.GetKind(),
			},
			Namespace:    resource.GetNamespace(),
			ResourceName: resource.GetName(),
		})
	}

	return ctrl.backupClient.UpdateStatus(context.TODO(), backup, metav1.UpdateOptions{})
}

func (ctrl *controller) validateBackup(backup *kahuapi.Backup) (bool, error) {
	var validationErrors []string
	backupSpec := backup.Spec
	// namespace validation
	includeNamespaces := sets.NewString(backupSpec.IncludeNamespaces...)
	excludeNamespaces := sets.NewString(backupSpec.ExcludeNamespaces...)
	// check common namespace name in include/exclude list
	if intersection := includeNamespaces.Intersection(excludeNamespaces); intersection.Len() > 0 {
		validationErrors = append(validationErrors,
			fmt.Sprintf("common namespace name (%s) in include and exclude namespace list",
				strings.Join(intersection.List(), ",")))
	}

	// resource validation
	// include resource validation
	for _, resourceSpec := range backupSpec.IncludeResources {
		if _, err := regexp.Compile(resourceSpec.Name); err != nil {
			validationErrors = append(validationErrors,
				fmt.Sprintf("invalid include resource expression (%s)", resourceSpec.Name))
		}
	}

	// exclude resource validation
	for _, resourceSpec := range backupSpec.ExcludeResources {
		if _, err := regexp.Compile(resourceSpec.Name); err != nil {
			validationErrors = append(validationErrors,
				fmt.Sprintf("invalid exclude resource expression (%s)", resourceSpec.Name))
		}
	}

	if backupSpec.MetadataLocation == "" {
		validationErrors = append(validationErrors,
			fmt.Sprintf("metadata location can not be empty"))
	}

	if backupSpec.MetadataLocation != "" {
		backupLocation, err := ctrl.blLister.Get(backupSpec.MetadataLocation)
		if apierrors.IsNotFound(err) {
			validationErrors = append(validationErrors,
				fmt.Sprintf("metadata location (%s) not found", backupSpec.MetadataLocation))
		}
		if err == nil && backupLocation != nil {
			_, err := ctrl.providerLister.Get(backupLocation.Spec.ProviderName)
			if apierrors.IsNotFound(err) {
				validationErrors = append(validationErrors,
					fmt.Sprintf("invalid provider name (%s) configured for metadata location (%s)",
						backupLocation.Spec.ProviderName, backupSpec.MetadataLocation))
			}
		}
	}

	if len(validationErrors) == 0 {
		return true, nil
	}

	ctrl.logger.Errorf("Backup validation failed. %s", strings.Join(validationErrors, ", "))
	if _, err := ctrl.updateBackupStatusWithEvent(backup, kahuapi.BackupStatus{
		State:            kahuapi.BackupStateFailed,
		ValidationErrors: validationErrors,
	}, corev1.EventTypeWarning, string(kahuapi.BackupStateFailed),
		fmt.Sprintf("Backup validation failed. %s", strings.Join(validationErrors, ", "))); err != nil {
		return false, errors.Wrap(err, "backup validation failed")
	}

	return false, errors.New("backup validation failed")
}

func isBackupInitNeeded(backup *kahuapi.Backup) bool {
	return !utils.ContainsFinalizer(backup, backupFinalizer) ||
		backup.Status.State == "" ||
		backup.Status.State == kahuapi.BackupStateNew
}

func (ctrl *controller) backupInitialize(backup *kahuapi.Backup) (*kahuapi.Backup, error) {
	var err error
	backupClone := backup.DeepCopy()

	if !utils.ContainsFinalizer(backup, backupFinalizer) {
		utils.SetFinalizer(backupClone, backupFinalizer)
		backupClone, err = ctrl.patchBackup(backup, backupClone)
		if err != nil {
			ctrl.logger.Errorf("Unable to update finalizer for backup(%s)", backup.Name)
			return backup, errors.Wrap(err, "Unable to update finalizer")
		}
	}

	if backup.Status.StartTimestamp == nil {
		timeNow := metav1.Now()
		backupClone.Status.StartTimestamp = &timeNow
	}
	return ctrl.updateBackupStatus(backupClone, backupClone.Status)
}
