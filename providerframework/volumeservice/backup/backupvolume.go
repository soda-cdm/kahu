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
	"strings"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuscheme "github.com/soda-cdm/kahu/client/clientset/versioned/scheme"
	kahuclient "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/informers/externalversions"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	providerSvc "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/utils"
	"github.com/soda-cdm/kahu/utils/operation"
)

const (
	controllerName               = "volume-content-backup"
	defaultContextTimeout        = 30 * time.Minute
	defaultSyncTime              = 5 * time.Minute
	defaultBackupProgressTimeout = 30 * time.Minute
	volumeBackupFinalizer        = "kahu.io/volume-backup-protection"

	EventVolumeBackupFailed       = "VolumeBackupFailed"
	EventVolumeBackupStarted      = "VolumeBackupStarted"
	EventVolumeBackupCompleted    = "VolumeBackupCompleted"
	EventVolumeBackupDeleteFailed = "VolumeBackupDeleteFailed"
	EventVolumeBackupCancelFailed = "VolumeBackupCancelFailed"
)

type controller struct {
	logger               log.FieldLogger
	providerName         string
	genericController    controllers.Controller
	volumeBackupLister   kahulister.VolumeBackupContentLister
	updater              volumeBackupUpdater
	driver               providerSvc.VolumeBackupClient
	backupLocationLister kahulister.BackupLocationLister
	backupProviderLister kahulister.ProviderLister
	backupLister         kahulister.BackupLister
	operationManager     operation.Manager
	processedVBC         utils.Store
	kubeClient           kubernetes.Interface
}

type volumeBackupUpdater struct {
	volumeBackupClient kahuclient.VolumeBackupContentInterface
	eventRecorder      record.EventRecorder
	store              utils.Store
}

func NewController(ctx context.Context,
	providerName string,
	kahuClient versioned.Interface,
	informer externalversions.SharedInformerFactory,
	eventBroadcaster record.EventBroadcaster,
	backupProviderClient providerSvc.VolumeBackupClient,
	kubeClient kubernetes.Interface) (controllers.Controller, error) {

	logger := log.WithField("controller", controllerName)

	processedVBCCache := utils.NewStore(utils.DeletionHandlingMetaNamespaceKeyFunc)
	updater := volumeBackupUpdater{
		volumeBackupClient: kahuClient.KahuV1beta1().VolumeBackupContents(),
		// initialize event recorder
		eventRecorder: eventBroadcaster.NewRecorder(kahuscheme.Scheme, v1.EventSource{Component: controllerName}),
		store:         processedVBCCache,
	}
	backupController := &controller{
		logger:               logger,
		providerName:         providerName,
		updater:              updater,
		volumeBackupLister:   informer.Kahu().V1beta1().VolumeBackupContents().Lister(),
		driver:               backupProviderClient,
		backupLocationLister: informer.Kahu().V1beta1().BackupLocations().Lister(),
		backupProviderLister: informer.Kahu().V1beta1().Providers().Lister(),
		backupLister:         informer.Kahu().V1beta1().Backups().Lister(),
		operationManager:     operation.NewOperationManager(ctx, logger.WithField("context", "backup-operation")),
		processedVBC: processedVBCCache,
		kubeClient:   kubeClient,
	}

	// construct controller interface to process worker queue
	genericController, err := controllers.NewControllerBuilder(controllerName).
		SetLogger(logger).
		SetHandler(backupController.processQueue).
		SetReSyncHandler(backupController.reSync).
		SetReSyncPeriod(defaultSyncTime).
		Build()
	if err != nil {
		return nil, err
	}

	// register to informer to receive events and push events to worker queue
	informer.Kahu().
		V1beta1().
		VolumeBackupContents().
		Informer().
		AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc: genericController.Enqueue,
				UpdateFunc: func(oldObj, newObj interface{}) {
					genericController.Enqueue(newObj)
				},
			},
		)

	// reference back
	backupController.genericController = genericController
	return genericController, err
}

func (ctrl *controller) reSync() {
	volumeBackupList, err := ctrl.volumeBackupLister.List(labels.Everything())
	if err != nil {
		ctrl.logger.Errorf("Unable to get volume backup list. %s", err)
		return
	}

	for _, volBackup := range volumeBackupList {
		if volBackup.Status.Phase == kahuapi.VolumeBackupContentPhaseFailed ||
			volBackup.Status.Phase == kahuapi.VolumeBackupContentPhaseCompleted {
			continue
		}

		ctrl.genericController.Enqueue(volBackup)
	}
}

func getVolumeBackup(index string,
	volBackupLister kahulister.VolumeBackupContentLister) (*kahuapi.VolumeBackupContent, error) {
	_, name, err := cache.SplitMetaNamespaceKey(index)
	if err != nil {
		return nil, err
	}

	return volBackupLister.Get(name)
}

func (ctrl *controller) processQueue(index string) error {
	ctrl.logger.Infof("Received volume backup request for %s", index)
	volBackup, err := getVolumeBackup(index, ctrl.volumeBackupLister)
	if err == nil {
		newObj, err := utils.StoreRevisionUpdate(ctrl.processedVBC, volBackup, "VolumeBackupContent")
		if err != nil {
			ctrl.logger.Errorf("%s", err)
		}
		if !newObj {
			ctrl.logger.Infof("Ignoring outdated volume backup request for %s", index)
			return nil
		}

		ctrl.logger.Infof("Processing volume backup request for %s", index)

		newVolBackup := volBackup.DeepCopy()
		if newVolBackup.DeletionTimestamp != nil {
			return ctrl.processDeleteVolumeBackup(newVolBackup)
		}

		newVolBackup, err = ctrl.ensureVolBackupInit(newVolBackup)
		if err != nil {
			return err
		}

		newVolBackup, err = ctrl.ensureFinalizer(newVolBackup)
		if err != nil {
			return err
		}

		// process create and sync
		return ctrl.processVolumeBackup(newVolBackup)
	}
	if apierrors.IsNotFound(err) {
		return nil
	}

	return fmt.Errorf("error getting volume backup content %s from informer", index)
}

func (ctrl *controller) isValidDeleteWithBackup(ownerReference metav1.OwnerReference,
	volBackup *kahuapi.VolumeBackupContent) (bool, error) {
	backup, err := ctrl.backupLister.Get(ownerReference.Name)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	} else if apierrors.IsNotFound(err) {
		return true, nil
	}

	if backup.UID != ownerReference.UID { // if owner with same name but different UID exist
		return true, nil
	}
	if backup.DeletionTimestamp == nil {
		ctrl.updater.Event(volBackup, v1.EventTypeWarning, utils.EventOwnerNotDeleted,
			"Owner backup not deleted")
		ctrl.logger.Errorf("Backup %s not deleted. Ignoring VolumeBackupContent(%s) delete",
			ownerReference.Name, volBackup.Name)
		return false, nil
	}

	return true, nil
}

func (ctrl *controller) isValidDelete(volBackup *kahuapi.VolumeBackupContent) (bool, error) {
	// process VolumeBackupContent delete if respective restore object is getting deleted
	for _, ownerReference := range volBackup.OwnerReferences {
		switch ownerReference.Kind {
		case utils.GVKBackup.Kind:
			valid, err := ctrl.isValidDeleteWithBackup(ownerReference, volBackup)
			if err != nil {
				return false, err
			}
			if !valid {
				return false, nil
			}
		default:
			continue
		}
	}

	return true, nil
}

func getVolIdentifiers(volBackupStatus *kahuapi.VolumeBackupContentStatus) []*providerSvc.BackupIdentity {
	backupIdentifiers := make([]*providerSvc.BackupIdentity, 0)
	for _, volumeState := range volBackupStatus.BackupState {
		backupIdentifiers = append(backupIdentifiers, &providerSvc.BackupIdentity{
			BackupHandle:     volumeState.BackupHandle,
			BackupAttributes: volumeState.BackupAttributes,
		})
	}
	return backupIdentifiers
}

func (ctrl *controller) cancelVolBackup(
	volBackup *kahuapi.VolumeBackupContent,
	backupIdentifiers []*providerSvc.BackupIdentity,
	backupParams map[string]string) error {
	logger := ctrl.logger.WithField("volume-backup", volBackup.Name)
	ctx, cancel := context.WithTimeout(context.TODO(), defaultContextTimeout)
	defer cancel()
	_, err := ctrl.driver.CancelBackup(ctx, &providerSvc.CancelBackupRequest{
		BackupInfo: backupIdentifiers, Parameters: backupParams,
	})
	if err != nil {
		if utils.CheckServerUnavailable(err) {
			ctrl.logger.Errorf("Driver unavailable: Continue to retry for volume backup cancel for %s",
				volBackup.Name)
			return err
		}
		ctrl.updater.Event(volBackup, v1.EventTypeWarning, EventVolumeBackupCancelFailed,
			fmt.Sprintf("Unable to Cancel backup. %s", err.Error()))
		logger.Errorf("Unable to Cancel backup. %s", err)
		return err
	}

	return nil
}

func (ctrl *controller) processDeleteVolumeBackup(volBackup *kahuapi.VolumeBackupContent) error {
	logger := ctrl.logger.WithField("volume-backup", volBackup.Name)
	logger.Infof("Initializing deletion for volume backup content(%s)", volBackup.Name)
	valid, err := ctrl.isValidDelete(volBackup)
	if err != nil {
		return err
	} else if !valid {
		return nil
	}

	volBackup, err = ctrl.updater.updatePhase(volBackup, kahuapi.VolumeBackupContentPhaseDeleting)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		logger.Errorf("Unable to update deletion status with err %s", err)
		return err
	}

	backupParams := volBackup.Spec.Parameters
	backupIdentifiers := getVolIdentifiers(&volBackup.Status)
	if volBackup.Status.Phase == kahuapi.VolumeBackupContentPhaseInProgress {
		if err = ctrl.cancelVolBackup(volBackup, backupIdentifiers, backupParams); err != nil {
			return err
		}
	}

	logger.Infof("Initiating volume delete driver call for %s ", volBackup.Name)
	ctx, cancel := context.WithTimeout(context.TODO(), defaultContextTimeout)
	defer cancel()
	_, err = ctrl.driver.DeleteBackup(ctx, &providerSvc.DeleteBackupRequest{BackupContentName: volBackup.Name,
		BackupInfo: backupIdentifiers, Parameters: backupParams,
	})
	if err != nil {
		if utils.CheckServerUnavailable(err) {
			ctrl.logger.Errorf("Driver unavailable: Continue to retry for volume backup delete for %s",
				volBackup.Name)
			return nil
		}
		ctrl.updater.Event(volBackup, v1.EventTypeWarning, EventVolumeBackupDeleteFailed,
			fmt.Sprintf("Unable to delete backup. %s", err.Error()))
		logger.Errorf("Unable to delete backup. %s", err)
		return err
	}

	if err := ctrl.ensureFinalizerRemove(volBackup); err != nil {
		return err
	}

	err = utils.StoreClean(ctrl.processedVBC, volBackup, "VolumeBackupContent")
	if err != nil {
		ctrl.logger.Warningf("Failed to clean processed cache. %s", err)
	}
	return nil
}

func (ctrl *controller) ensureFinalizerRemove(volBackup *kahuapi.VolumeBackupContent) error {
	if utils.ContainsFinalizer(volBackup, volumeBackupFinalizer) {
		backupClone := volBackup.DeepCopy()
		utils.RemoveFinalizer(backupClone, volumeBackupFinalizer)
		if _, err := ctrl.updater.patchBackup(volBackup, backupClone); err != nil {
			ctrl.logger.Errorf("removing finalizer failed for %s", volBackup.Name)
			return err
		}
	}
	return nil
}

func validateBackupResponse(volNames sets.String,
	backupIdentifiers []*providerSvc.BackupIdentifier) error {
	// ensure requested volume has backup identifiers
	if volNames.Len() != len(backupIdentifiers) {
		return errors.New("driver do not return all volume backup handler")
	}

	backupVols := sets.NewString()
	for _, backupIdentifier := range backupIdentifiers {
		backupVols.Insert(backupIdentifier.GetPvName())
	}

	if !volNames.HasAll(backupVols.List()...) {
		return fmt.Errorf("driver do not return volume %s",
			strings.Join(volNames.Difference(backupVols).List(), ", "))
	}

	return nil
}

func (ctrl *controller) getPersistentVols(
	vbcVols []v1.PersistentVolume) ([]*v1.PersistentVolume, sets.String, error) {
	volNames := sets.NewString()
	volumes := make([]*v1.PersistentVolume, 0)
	for _, vbcVol := range vbcVols {
		ctrl.logger.Infof("Getting volume details for pv  (%s) ", vbcVol.Name)
		pv, err := ctrl.kubeClient.CoreV1().PersistentVolumes().Get(context.TODO(), vbcVol.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			ctrl.logger.Errorf("request PV (%s) backup not found", vbcVol.Name)
			return volumes, volNames, fmt.Errorf("request PV (%s) backup not found", vbcVol.Name)
		}
		if err != nil {
			ctrl.logger.Errorf("Failed to fetch volume details for the pv (%s) err (%s) ", vbcVol.Name, err)
			return volumes, volNames, err
		}

		volNames.Insert(pv.Name)
		// ensure APIVersion and Kind for PV
		pv.APIVersion = vbcVol.APIVersion
		pv.Kind = vbcVol.Kind
		volumes = append(volumes, pv)
	}

	return volumes, volNames, nil
}

func (ctrl *controller) handleVolumeBackup(
	volBackup *kahuapi.VolumeBackupContent) (*kahuapi.VolumeBackupContent, error) {
	volNames := sets.NewString()

	volumes, volNames, err := ctrl.getPersistentVols(volBackup.Spec.Volumes)
	if err != nil {
		return volBackup, err
	}

	ctx, cancel := context.WithTimeout(context.TODO(), defaultContextTimeout)
	defer cancel()
	response, err := ctrl.driver.StartBackup(ctx, &providerSvc.StartBackupRequest{
		BackupContentName: volBackup.Name, Pv: volumes, Parameters: volBackup.Spec.Parameters})
	if err != nil {
		ctrl.logger.Errorf("Unable to start backup. %s", err)
		return volBackup, err
	}

	if response != nil && len(response.Errors) > 0 {
		msg := fmt.Sprintf("Unable to start backup. err: (%s)", strings.Join(response.Errors, " ,"))
		ctrl.logger.Error(msg)
		return volBackup, fmt.Errorf("%s", msg)
	}

	if err = validateBackupResponse(volNames, response.GetBackupInfo()); err != nil {
		ctrl.logger.Errorf("Invalid backup response from driver. %s", err)
		return volBackup, err
	}

	backupState := make([]kahuapi.VolumeBackupState, 0)
	for _, backupIdentifier := range response.GetBackupInfo() {
		backupState = append(backupState, kahuapi.VolumeBackupState{
			VolumeName:         backupIdentifier.GetPvName(),
			BackupHandle:       backupIdentifier.GetBackupIdentity().GetBackupHandle(),
			BackupAttributes:   backupIdentifier.GetBackupIdentity().GetBackupAttributes(),
			LastProgressUpdate: metav1.Now().Unix(),
		})
	}

	// update backup status
	newBackup := volBackup.DeepCopy()
	newBackup.Status.Phase = kahuapi.VolumeBackupContentPhaseInProgress
	newBackup.Status.BackupState = backupState
	return newBackup, err
}

func (ctrl *controller) processVolumeBackup(backup *kahuapi.VolumeBackupContent) error {
	logger := ctrl.logger.WithField("backup", backup.Name)
	switch backup.Status.Phase {
	case "", kahuapi.VolumeBackupContentPhaseInit:
		newBackup, err := ctrl.handleVolumeBackup(backup)
		if err != nil {
			if utils.CheckServerUnavailable(err) {
				ctrl.logger.Errorf("Driver unavailable: Continue to retry for volume backup for %s", backup.Name)
				return nil
			}
			if _, err = ctrl.updater.updateStatusWithFailure(newBackup, err.Error()); err != nil {
				ctrl.logger.Errorf("Unable to start backup. %s", err)
			}
			return err
		}
		// update status with in progress
		if newBackup, err = ctrl.updater.updateStatusWithEvent(backup, newBackup,
			v1.EventTypeNormal, EventVolumeBackupStarted, "Started volume backup"); err != nil {
			ctrl.logger.Errorf("Unable to update volume backup start. %s", err)
			return err
		}

		fallthrough
	case kahuapi.VolumeBackupContentPhaseInProgress:
		ctx := ctrl.newVolumeBackupProgressContext(ctrl.logger)
		err := ctrl.operationManager.Run(backup.Name, operation.Handler{
			Operation: ctx.processBackupProgress,
			OnFailure: ctx.handleBackupProgressFailure,
			OnSuccess: ctx.handleBackupProgressSuccess})
		if operation.IsAlreadyExists(err) {
			return nil
		}
		ctrl.logger.Infof("Backup operation in progress for %s", backup.Name)
		return nil
	case kahuapi.VolumeBackupContentPhaseFailed:
		logger.Infof("Volume backup failed already")
		return nil
	default:
		logger.Infof("Ignoring volume backup state (%s).", backup.Status.Phase)
		return nil
	}
}

type backupProgressContext struct {
	updater         volumeBackupUpdater
	volBackupLister kahulister.VolumeBackupContentLister
	driver          providerSvc.VolumeBackupClient
	logger          log.FieldLogger
}

func (ctrl *controller) newVolumeBackupProgressContext(logger log.FieldLogger) *backupProgressContext {
	return &backupProgressContext{
		volBackupLister: ctrl.volumeBackupLister,
		updater:         ctrl.updater,
		driver:          ctrl.driver,
		logger:          logger.WithField("context", "volume-backup-progress"),
	}
}

func (ctx *backupProgressContext) skipBackupProgressProcessing(volBackup *kahuapi.VolumeBackupContent) bool {
	if volBackup.DeletionTimestamp != nil {
		ctx.logger.Warningf("Volume backup % is getting deleted. Exiting volume backup progress",
			volBackup.Name)
		return true
	} else if volBackup.Status.Phase != kahuapi.VolumeBackupContentPhaseInProgress {
		ctx.logger.Infof("Volume backup is not in-progress. Skipping %s "+
			"backup progress check", volBackup.Name)
		return true
	}

	return false
}

func (ctx *backupProgressContext) processBackupProgress(index string) (bool, error) {
	ctx.logger.Infof("Checking backup progress for %s", index)
	volBackup, err := getVolumeBackup(index, ctx.volBackupLister)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("volume backup (%s) already deleted", index)
			return true, nil
		}
		ctx.logger.Errorf("Unable to get volume backup. %s", err)
		return false, nil
	}

	if ctx.skipBackupProgressProcessing(volBackup) {
		return true, nil
	}

	newVolBackup := volBackup.DeepCopy()
	completed, err := ctx.syncBackupStat(newVolBackup)
	if err != nil {
		if utils.CheckServerUnavailable(err) {
			ctx.logger.Errorf("Driver unavailable: Continue to retry for volume backup stat for %s",
				volBackup.Name)
			return false, nil
		}
		ctx.logger.Errorf("Failed to sync volume backup stat. %s", err)
		return false, err
	}

	if completed {
		newVolBackup.Status.Phase = kahuapi.VolumeBackupContentPhaseCompleted
	}

	ctx.logger.Infof("Updating backup progress for %s", index)
	volBackup, err = ctx.updater.updateStatus(volBackup, newVolBackup)
	if err != nil {
		ctx.logger.Errorf("Unable to update volume backup states for %s. %s", volBackup.Name, err)
		return false, nil
	}

	if completed {
		ctx.logger.Infof("Volume backup completed for %s", index)
		ctx.updater.Event(volBackup, v1.EventTypeNormal, EventVolumeBackupCompleted, "Completed volume backup")
		return true, nil
	}

	return false, nil
}

func (ctx *backupProgressContext) syncBackupStat(
	volBackup *kahuapi.VolumeBackupContent) (bool, error) {
	backupInfo := make([]*providerSvc.BackupIdentity, 0)
	for _, state := range volBackup.Status.BackupState {
		backupInfo = append(backupInfo, &providerSvc.BackupIdentity{
			BackupHandle:     state.BackupHandle,
			BackupAttributes: state.BackupAttributes,
		})
	}

	driverCtx, cancel := context.WithTimeout(context.TODO(), defaultContextTimeout)
	defer cancel()
	stats, err := ctx.driver.GetBackupStat(driverCtx, &providerSvc.GetBackupStatRequest{
		BackupInfo: backupInfo,
		Parameters: volBackup.Spec.Parameters,
	})
	if err != nil {
		ctx.logger.Errorf("Unable to get volume backup state for %s. %s", volBackup.Name, err)
		return false, err
	}

	// update backup status
	for _, stat := range stats.BackupStats {
		for i, state := range volBackup.Status.BackupState {
			if state.BackupHandle == stat.BackupHandle &&
				stat.Progress > state.Progress {
				volBackup.Status.BackupState[i].LastProgressUpdate = metav1.Now().Unix()
				volBackup.Status.BackupState[i].Progress = stat.Progress
			}
		}
	}

	progressCompleted := true
	for _, state := range volBackup.Status.BackupState {
		if state.Progress < 100 {
			progressCompleted = false
			break
		}
	}

	progressTimeout := true
	for _, volStat := range volBackup.Status.BackupState {
		if time.Since(time.Unix(volStat.LastProgressUpdate, 0)) < defaultBackupProgressTimeout {
			progressTimeout = false
		}
	}

	if progressTimeout {
		volBackup.Status.FailureReason = fmt.Sprintf("Volume backup timout. No progress update for "+
			"max wait duration %s", defaultBackupProgressTimeout.String())
		volBackup.Status.Phase = kahuapi.VolumeBackupContentPhaseFailed
	}

	return progressCompleted, nil
}

func (ctx *backupProgressContext) handleBackupProgressFailure(index string, progressErr error) {
	volBackup, err := getVolumeBackup(index, ctx.volBackupLister)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("volume backup (%s) already deleted", index)
			return
		}
		ctx.logger.Errorf("Unable to get volume backup. %s", err)
		return
	}

	if ctx.skipBackupProgressProcessing(volBackup) {
		return
	}

	_, err = ctx.updater.updateStatusWithFailure(volBackup, progressErr.Error())
	if err != nil {
		ctx.logger.Errorf("Failed to update backup progress failure(%s). %s", progressErr, err)
	}
	return
}

func (ctx *backupProgressContext) handleBackupProgressSuccess(index string) {
	volBackup, err := getVolumeBackup(index, ctx.volBackupLister)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("volume backup (%s) already deleted", index)
			return
		}
		ctx.logger.Errorf("Unable to get volume backup. %s", err)
		return
	}

	if ctx.skipBackupProgressProcessing(volBackup) {
		return
	}

	_, err = ctx.updater.updatePhase(volBackup, kahuapi.VolumeBackupContentPhaseCompleted)
	if err != nil {
		ctx.logger.Errorf("Failed to update backup progress success. %s", err)
	}
	return
}

func (ctrl *controller) ensureFinalizer(
	volBackup *kahuapi.VolumeBackupContent) (*kahuapi.VolumeBackupContent, error) {
	if !utils.ContainsFinalizer(volBackup, volumeBackupFinalizer) {
		volBackupClone := volBackup.DeepCopy()
		utils.SetFinalizer(volBackupClone, volumeBackupFinalizer)
		return ctrl.updater.patchBackup(volBackup, volBackupClone)
	}
	return volBackup, nil
}

func (ctrl *controller) ensureVolBackupInit(
	volBackup *kahuapi.VolumeBackupContent) (*kahuapi.VolumeBackupContent, error) {
	volBackupClone := volBackup.DeepCopy()
	dirty := false
	if volBackup.Status.Phase == "" {
		dirty = true
		volBackupClone.Status.Phase = kahuapi.VolumeBackupContentPhaseInit
	}
	if volBackup.Status.StartTimestamp == nil {
		dirty = true
		metaTime := metav1.Now()
		volBackupClone.Status.StartTimestamp = &metaTime
	}

	if dirty {
		return ctrl.updater.patchBackupStatus(volBackup, volBackupClone)
	}
	return volBackup, nil
}

func (updater *volumeBackupUpdater) Event(backup *kahuapi.VolumeBackupContent,
	eventType, reason, message string) {
	updater.eventRecorder.Event(backup, eventType, reason, message)
}

func (updater *volumeBackupUpdater) updateStatusWithEvent(backup, newBackup *kahuapi.VolumeBackupContent,
	eventType, reason, message string) (*kahuapi.VolumeBackupContent, error) {
	newBackup, err := updater.updateStatus(backup, newBackup)
	if err != nil {
		return newBackup, err
	}

	updater.Event(newBackup, eventType, reason, message)
	return newBackup, err
}

func (updater *volumeBackupUpdater) updateStatusWithFailure(backup *kahuapi.VolumeBackupContent,
	msg string) (*kahuapi.VolumeBackupContent, error) {
	newBackup := backup.DeepCopy()
	newBackup.Status.Phase = kahuapi.VolumeBackupContentPhaseFailed
	newBackup.Status.FailureReason = msg
	backup, err := updater.patchBackupStatus(backup, newBackup)
	if err != nil {
		return backup, err
	}

	updater.Event(backup, v1.EventTypeWarning, EventVolumeBackupFailed, msg)
	return backup, err
}

func (updater *volumeBackupUpdater) updateStatus(backup,
	newBackup *kahuapi.VolumeBackupContent) (*kahuapi.VolumeBackupContent, error) {
	return updater.patchBackupStatus(backup, newBackup)
}

func (updater *volumeBackupUpdater) updatePhaseWithEvent(backup *kahuapi.VolumeBackupContent,
	phase kahuapi.VolumeBackupContentPhase,
	eventType, reason, message string) (*kahuapi.VolumeBackupContent, error) {
	newBackup, err := updater.updatePhase(backup, phase)
	if err != nil {
		return newBackup, err
	}

	updater.Event(newBackup, eventType, reason, message)
	return newBackup, err
}

func (updater *volumeBackupUpdater) updatePhase(backup *kahuapi.VolumeBackupContent,
	phase kahuapi.VolumeBackupContentPhase) (*kahuapi.VolumeBackupContent, error) {
	backupClone := backup.DeepCopy()
	backupClone.Status.Phase = phase
	return updater.patchBackupStatus(backup, backupClone)
}

func (updater *volumeBackupUpdater) patchBackup(
	oldBackup,
	newBackup *kahuapi.VolumeBackupContent) (*kahuapi.VolumeBackupContent, error) {
	origBytes, err := json.Marshal(oldBackup)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original backup")
	}

	updatedBytes, err := json.Marshal(newBackup)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated backup")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for backup")
	}

	updatedBackup, err := updater.volumeBackupClient.Patch(context.TODO(),
		oldBackup.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "error patching backup")
	}

	_, err = utils.StoreRevisionUpdate(updater.store, updatedBackup, "VolumeBackupContent")
	if err != nil {
		return updatedBackup, errors.Wrap(err, "Failed to updated processed VBC cache")
	}

	return updatedBackup, nil
}

func (updater *volumeBackupUpdater) patchBackupStatus(
	oldBackup,
	newBackup *kahuapi.VolumeBackupContent) (*kahuapi.VolumeBackupContent, error) {
	origBytes, err := json.Marshal(oldBackup)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original backup")
	}

	updatedBytes, err := json.Marshal(newBackup)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated backup")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for backup")
	}

	updatedBackup, err := updater.volumeBackupClient.Patch(context.TODO(),
		oldBackup.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
		"status")
	if err != nil {
		return nil, errors.Wrap(err, "error patching backup")
	}

	_, err = utils.StoreRevisionUpdate(updater.store, updatedBackup, "VolumeBackupContent")
	if err != nil {
		return updatedBackup, errors.Wrap(err, "Failed to updated processed VBC cache")
	}

	return updatedBackup, nil
}
