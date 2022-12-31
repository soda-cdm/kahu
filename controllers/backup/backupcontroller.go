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
	"fmt"
	"github.com/soda-cdm/kahu/volume"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuscheme "github.com/soda-cdm/kahu/client/clientset/versioned/scheme"
	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	kahuinformer "github.com/soda-cdm/kahu/client/informers/externalversions"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/hooks"
	"github.com/soda-cdm/kahu/utils"
)

type Config struct {
	SupportedResources string
}

type controller struct {
	ctx                  context.Context
	config               *Config
	logger               log.FieldLogger
	genericController    controllers.Controller
	kubeClient           kubernetes.Interface
	dynamicClient        dynamic.Interface
	backupClient         kahuv1client.BackupInterface
	backupLister         kahulister.BackupLister
	backupLocationLister kahulister.BackupLocationLister
	eventRecorder        record.EventRecorder
	discoveryHelper      discovery.DiscoveryHelper
	providerLister       kahulister.ProviderLister
	volumeBackupClient   kahuv1client.VolumeBackupContentInterface
	hookExecutor         hooks.Hooks
	processedBackup      utils.Store
	volumeHandler        volume.Interface
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
	volumeHandler volume.Interface) (controllers.Controller, error) {

	logger := log.WithField("controller", controllerName)
	processedBackupCache := utils.NewStore(utils.DeletionHandlingMetaNamespaceKeyFunc)
	backupController := &controller{
		ctx:                  ctx,
		logger:               logger,
		config:               config,
		kubeClient:           kubeClient,
		backupClient:         kahuClient.KahuV1beta1().Backups(),
		backupLister:         informer.Kahu().V1beta1().Backups().Lister(),
		backupLocationLister: informer.Kahu().V1beta1().BackupLocations().Lister(),
		dynamicClient:        dynamicClient,
		discoveryHelper:      discoveryHelper,
		providerLister:       informer.Kahu().V1beta1().Providers().Lister(),
		volumeBackupClient:   kahuClient.KahuV1beta1().VolumeBackupContents(),
		hookExecutor:         hookExecutor,
		processedBackup:      processedBackupCache,
		volumeHandler:        volumeHandler,
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
		v1.EventSource{Component: controllerName})
	backupController.eventRecorder = eventRecorder

	// reference back
	backupController.genericController = genericController

	// start volume backup reconciler
	go newReconciler(defaultReconcileTimeLoop,
		backupController.logger.WithField("source", "reconciler"),
		backupController.volumeBackupClient,
		backupController.backupClient,
		backupController.backupLister).Run(ctx.Done())

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
	new, err := utils.StoreRevisionUpdate(ctrl.processedBackup, newBackup, "Backup")
	if err != nil {
		ctrl.logger.Errorf("%s", err)
	}
	if !new {
		return nil
	}

	if newBackup.DeletionTimestamp != nil {
		return ctrl.deleteBackup(newBackup)
	}

	switch newBackup.Status.Stage {
	case "", kahuapi.BackupStageInitial:
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
		newBackup, err = ctrl.updateBackupStatusWithEvent(newBackup, kahuapi.BackupStatus{
			State: kahuapi.BackupStateCompleted,
		}, v1.EventTypeNormal, EventValidationSuccess, "Backup spec validation success")
		if err != nil {
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

	err := ctrl.removeVolumeBackup(backup)
	if err != nil {
		ctrl.logger.Errorf("Unable to delete volume backup. %s", err)
		return err
	}

	// check if all volume backup contents are deleted
	vbsList, err := ctrl.volumeBackupClient.List(context.TODO(),
		metav1.ListOptions{LabelSelector: labels.Set{volumeContentBackupLabel: backup.Name}.AsSelector().String()})
	if err != nil {
		ctrl.logger.Errorf("Unable to get volume backup list. %s", err)
		return err
	}
	if len(vbsList.Items) > 0 {
		ctrl.logger.Info("Volume backup list is not empty. Continue to wait for Volume backup delete")
		return nil
	}

	ctrl.logger.Info("Volume backup deleted successfully")

	err = ctrl.deleteMetadataBackup(backup)
	if err != nil {
		ctrl.logger.Errorf("Unable to delete meta backup. %s", err)
		return err
	}

	backupUpdate := backup.DeepCopy()
	utils.RemoveFinalizer(backupUpdate, backupFinalizer)
	utils.SetAnnotation(backupUpdate, annBackupCleanupDone, "true")
	backup, err = ctrl.patchBackup(backup, backupUpdate)
	if err != nil {
		ctrl.logger.Errorf("removing finalizer failed for %s", backup.Name)
	}

	err = utils.StoreClean(ctrl.processedBackup, backup, "Backup")
	if err != nil {
		ctrl.logger.Warningf("Failed to clean processed cache. %s", err)
	}
	return err
}

func (ctrl *controller) syncBackup(backup *kahuapi.Backup) (err error) {
	if backup.Status.State == kahuapi.BackupStateDeleting ||
		backup.Status.State == kahuapi.BackupStateFailed {
		if backup.Status.Stage != kahuapi.BackupStageVolumes {
			return err
		}
	}

	switch backup.Status.Stage {
	case kahuapi.BackupStageInitial:
		backup, err = ctrl.updateBackupStatusWithEvent(backup,
			kahuapi.BackupStatus{Stage: kahuapi.BackupStagePreHook, State: kahuapi.BackupStateNew},
			v1.EventTypeNormal, string(kahuapi.BackupStagePreHook), "Start prehook")
		if err != nil {
			ctrl.logger.Errorf("Update backup(%s) processing: failed to "+
				"update initial stage for pre hook", backup.Name)
			return err
		}
		fallthrough
	case kahuapi.BackupStagePreHook:
		// Execute pre hooks
		backup, err = ctrl.updateBackupStatusWithEvent(backup,
			kahuapi.BackupStatus{State: kahuapi.BackupStateProcessing},
			v1.EventTypeNormal, string(kahuapi.BackupStagePreHook), "Check and process prehook")
		if err != nil {
			ctrl.logger.Errorf("Update backup(%s) processing: failed to "+
				"update Prehook state to processing", backup.Name)
			return err
		}
		// Execute pre hooks
		err = ctrl.hookExecutor.ExecuteHook(ctrl.logger, &backup.Spec, hooks.PreHookPhase)
		if err != nil {
			if backup, err = ctrl.updateBackupStatusWithEvent(backup,
				kahuapi.BackupStatus{State: kahuapi.BackupStateFailed},
				v1.EventTypeWarning, EventPreHookFailed, "Pre hook execution failed"); err != nil {
				ctrl.logger.Infof("Update to cleanup state for Pre hook reversal failed")
				return err
			}
			ctrl.reversePreHooksExecutionOnFailure(backup,
				kahuapi.BackupStagePreHook, EventPreHookFailed)
			ctrl.logger.Infof("Pre hook reversal finished (%s)", backup.Name)
			return nil
		}
		ctrl.eventRecorder.Event(backup, v1.EventTypeNormal,
			string(kahuapi.BackupStagePreHook), "Pre hook execution successful")

		backup, err = ctrl.updateBackupStatusWithEvent(backup,
			kahuapi.BackupStatus{Stage: kahuapi.BackupStageVolumes, State: kahuapi.BackupStateNew},
			v1.EventTypeNormal, string(kahuapi.BackupStageVolumes), "Start backup volumes")
		if err != nil {
			return err
		}
		fallthrough
	case kahuapi.BackupStageVolumes:
		backup, err = ctrl.syncVolumeBackup(backup)
		if err != nil {
			return err
		}

		if backup.Status.State == kahuapi.BackupStateFailed {
			if metav1.HasAnnotation(backup.ObjectMeta, annVolumeBackupFailHooks) {
				return nil
			}
			if backup, err = ctrl.updateBackupStatusWithEvent(backup,
				kahuapi.BackupStatus{State: kahuapi.BackupStateFailed},
				v1.EventTypeWarning, EventVolumeBackupFailed, "volume backup failed"); err != nil {
				ctrl.logger.Errorf("Update to cleanup state for Pre hook reversal failed")
				return err
			}
			ctrl.reversePreHooksExecutionOnFailure(backup,
				kahuapi.BackupStageVolumes, EventVolumeBackupFailed)
			ctrl.annotateBackup(annVolumeBackupFailHooks, backup)
			ctrl.logger.Infof("Pre hook reversal finished (%s)", backup.Name)
			return nil
		}

		if backup.Status.State != kahuapi.BackupStateCompleted {
			ctrl.logger.Info("Waiting to finish volume backup")
			return nil
		}
		ctrl.logger.Infof("Volume backup(%s) successful", backup.Name)

		backup, err = ctrl.updateBackupStatusWithEvent(backup,
			kahuapi.BackupStatus{Stage: kahuapi.BackupStagePostHook, State: kahuapi.BackupStateProcessing},
			v1.EventTypeNormal, string(kahuapi.BackupStagePostHook), "Check and process post hook")
		if err != nil {
			ctrl.logger.Errorf("Update backup(%s) processing: failed to "+
				"update Posthook state to processing", backup.Name)
			return err
		}
		fallthrough

	case kahuapi.BackupStagePostHook:
		// Execute post hooks
		err = ctrl.hookExecutor.ExecuteHook(ctrl.logger, &backup.Spec, hooks.PostHookPhase)
		if err != nil {
			ctrl.logger.Errorf("Failed to execute post hooks: %s", err.Error())
			if _, err = ctrl.updateBackupStatusWithEvent(backup,
				kahuapi.BackupStatus{State: kahuapi.BackupStateFailed},
				v1.EventTypeWarning, EventPostHookFailed, "post hook execution failed"); err != nil {
				return err
			}
			return nil
		}
		ctrl.eventRecorder.Event(backup, v1.EventTypeNormal,
			string(kahuapi.BackupStagePostHook), "Post hook execution successful")
		backup, err = ctrl.updateBackupStatusWithEvent(backup,
			kahuapi.BackupStatus{Stage: kahuapi.BackupStageResources, State: kahuapi.BackupStateNew},
			v1.EventTypeNormal, string(kahuapi.BackupStageResources), "Start resource backup")
		if err != nil {
			ctrl.logger.Errorf("Update backup(%s) processing: failed to "+
				"update resource backup stage for volume backup", backup.Name)
			return err
		}

		fallthrough
	case kahuapi.BackupStageResources:
		backup, err = ctrl.syncResourceBackup(backup)
		if err != nil {
			return err
		}
		if backup.Status.State != kahuapi.BackupStateCompleted {
			ctrl.logger.Info("Waiting to finish resource backup")
			return nil
		}
		// populate all meta service
		backup, err = ctrl.updateBackupStatusWithEvent(backup, kahuapi.BackupStatus{
			Stage: kahuapi.BackupStageFinished,
			State: kahuapi.BackupStateNew,
		}, v1.EventTypeNormal, string(kahuapi.BackupStageResources),
			"Backup resources successful")
		if err != nil {
			return err
		}
		fallthrough
	case kahuapi.BackupStageFinished:
		if backup.Status.State == kahuapi.BackupStateCompleted {
			ctrl.logger.Infof("Backup is finished already")
		}
		// populate all meta service
		time := metav1.Now()
		_, err = ctrl.updateBackupStatusWithEvent(backup, kahuapi.BackupStatus{
			Stage:               kahuapi.BackupStageFinished,
			State:               kahuapi.BackupStateCompleted,
			CompletionTimestamp: &time,
		}, v1.EventTypeNormal, string(kahuapi.BackupStageResources),
			"Backup successful")
		return err
	default:
		return err
	}
}

func (ctrl *controller) syncVolumeBackup(
	backup *kahuapi.Backup) (*kahuapi.Backup, error) {
	if backup.Status.State == kahuapi.BackupStateCompleted {
		return backup, nil
	}
	// check if volume backup required
	if metav1.HasAnnotation(backup.ObjectMeta, annVolumeBackupCompleted) {
		return ctrl.updateBackupStatusWithEvent(backup,
			kahuapi.BackupStatus{State: kahuapi.BackupStateCompleted},
			v1.EventTypeNormal, EventVolumeBackupSuccess, "Volume backup successful")
	}

	// only allow fresh volume backup
	if backup.Status.State != kahuapi.BackupStateNew {
		return backup, nil
	}

	backupResources := NewBackupResources(ctrl.logger,
		ctrl.dynamicClient, ctrl.kubeClient, ctrl.discoveryHelper, ctrl)
	newBackup, err := backupResources.Sync(backup)
	if err != nil {
		ctrl.logger.Errorf("failed to populate backup resources %s", err)
		return backup, err
	}

	newBackup, err = ctrl.processVolumeBackup(newBackup, backupResources)
	if err != nil {
		return newBackup, err
	}

	return ctrl.updateBackupStatusWithEvent(newBackup, kahuapi.BackupStatus{
		State: kahuapi.BackupStateProcessing,
	}, v1.EventTypeNormal, EventVolumeBackupScheduled, "Volume backup Scheduled")
}

func (ctrl *controller) syncResourceBackup(
	backup *kahuapi.Backup) (*kahuapi.Backup, error) {
	return ctrl.processMetadataBackup(backup)
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
		backupLocation, err := ctrl.backupLocationLister.Get(backupSpec.MetadataLocation)
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
	}, v1.EventTypeWarning, string(kahuapi.BackupStateFailed),
		fmt.Sprintf("Backup validation failed. %s", strings.Join(validationErrors, ", "))); err != nil {
		return false, errors.Wrap(err, "backup validation failed")
	}

	return false, errors.New("backup validation failed")
}

func isBackupInitNeeded(backup *kahuapi.Backup) bool {
	if !utils.ContainsFinalizer(backup, backupFinalizer) ||
		backup.Status.Stage == "" ||
		backup.Status.Stage == kahuapi.BackupStageInitial {
		return true
	}

	return false
}

func (ctrl *controller) reversePreHooksExecutionOnFailure(backup *kahuapi.Backup,
	stage kahuapi.BackupStage, event string) {
	// Execute post hooks for reverse, previous hook executions
	ctrl.logger.Infof("Try executing post hooks to reverse pre hook, when failure on: %s", event)
	err := ctrl.hookExecutor.ExecuteHook(ctrl.logger, &backup.Spec, hooks.PostHookPhase)
	if err != nil {
		ctrl.logger.Error("Failed to execute reverse post hooks")
		ctrl.eventRecorder.Event(backup, v1.EventTypeWarning, event,
			"Post hook execution to revert Pre hook also failed")
		return
	}
	ctrl.eventRecorder.Event(backup, v1.EventTypeWarning, event,
		"Post hook execution to revert Pre hook is success")
	return
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

	if backupClone.Status.Stage == "" {
		backupClone.Status.Stage = kahuapi.BackupStageInitial
	}
	if backup.Status.StartTimestamp == nil {
		time := metav1.Now()
		backupClone.Status.StartTimestamp = &time
	}
	return ctrl.updateBackupStatus(backupClone, backupClone.Status)
}

// addTypeInformationToObject adds TypeMeta information to a runtime.Object based upon the loaded scheme.Scheme
// inspired by: https://github.com/kubernetes/cli-runtime/blob/v0.19.2/pkg/printers/typesetter.go#L41
func addTypeInformationToObject(obj runtime.Object) (schema.GroupVersionKind, error) {
	gvks, _, err := scheme.Scheme.ObjectKinds(obj)
	if err != nil {
		return schema.GroupVersionKind{}, fmt.Errorf("missing apiVersion or kind and cannot assign it; %w", err)
	}

	for _, gvk := range gvks {
		if len(gvk.Kind) == 0 {
			continue
		}
		if len(gvk.Version) == 0 || gvk.Version == runtime.APIVersionInternal {
			continue
		}

		obj.GetObjectKind().SetGroupVersionKind(gvk)
		return gvk, nil
	}

	return schema.GroupVersionKind{}, err
}
