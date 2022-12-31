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
	controllerName = "volume-restore-content"

	volumeRestoreContentFinalizer = "kahu.io/volume-restore-content-protection"

	defaultContextTimeout         = 30 * time.Minute
	defaultSyncTime               = 5 * time.Minute
	defaultRestoreProgressTimeout = 30 * time.Minute

	EventVolumeRestoreFailed       = "VolumeRestoreFailed"
	EventVolumeRestoreStarted      = "VolumeRestoreStarted"
	EventVolumeRestoreCompleted    = "VolumeRestoreCompleted"
	EventVolumeRestoreCancelFailed = "VolumeRestoreCancelFailed"

	annVolumeResourceCleanup = "kahu.io/volume-resource-cleanup"
)

type controller struct {
	logger              log.FieldLogger
	providerName        string
	genericController   controllers.Controller
	volumeRestoreClient kahuclient.VolumeRestoreContentInterface
	volumeRestoreLister kahulister.VolumeRestoreContentLister
	eventRecorder       record.EventRecorder
	driver              providerSvc.VolumeBackupClient
	operationManager    operation.Manager
	processedVRC        utils.Store
	updater             volumeRestoreUpdater
}

type volumeRestoreUpdater struct {
	volumeRestoreClient kahuclient.VolumeRestoreContentInterface
	eventRecorder       record.EventRecorder
	store               utils.Store
}

func NewController(ctx context.Context,
	providerName string,
	kahuClient versioned.Interface,
	informer externalversions.SharedInformerFactory,
	eventBroadcaster record.EventBroadcaster,
	driver providerSvc.VolumeBackupClient) (controllers.Controller, error) {
	logger := log.WithField("controller", controllerName)

	processedVRCCache := utils.NewStore(utils.DeletionHandlingMetaNamespaceKeyFunc)
	updater := volumeRestoreUpdater{
		volumeRestoreClient: kahuClient.KahuV1beta1().VolumeRestoreContents(),
		eventRecorder:       eventBroadcaster.NewRecorder(kahuscheme.Scheme, v1.EventSource{Component: controllerName}),
		store:               processedVRCCache,
	}
	restoreController := &controller{
		logger:              logger,
		providerName:        providerName,
		updater:             updater,
		volumeRestoreClient: kahuClient.KahuV1beta1().VolumeRestoreContents(),
		volumeRestoreLister: informer.Kahu().V1beta1().VolumeRestoreContents().Lister(),
		driver:              driver,
		operationManager:    operation.NewOperationManager(ctx, logger.WithField("context", "restore-operation")),
		processedVRC:        processedVRCCache,
	}

	// construct controller interface to process worker queue
	genericController, err := controllers.NewControllerBuilder(controllerName).
		SetLogger(logger).
		SetHandler(restoreController.processQueue).
		SetReSyncHandler(restoreController.reSync).
		SetReSyncPeriod(defaultSyncTime).
		Build()
	if err != nil {
		return nil, err
	}

	// register to informer to receive events and push events to worker queue
	informer.Kahu().
		V1beta1().
		VolumeRestoreContents().
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
	restoreController.genericController = genericController
	return genericController, err
}

func (ctrl *controller) reSync() {
	volumeRestoreList, err := ctrl.volumeRestoreLister.List(labels.Everything())
	if err != nil {
		ctrl.logger.Errorf("Unable to get volume restore list. %s", err)
		return
	}

	for _, volRestore := range volumeRestoreList {
		if volRestore.Status.Phase == kahuapi.VolumeRestoreContentPhaseFailed ||
			volRestore.Status.Phase == kahuapi.VolumeRestoreContentPhaseCompleted {
			continue
		}

		ctrl.genericController.Enqueue(volRestore)
	}
}

func (ctrl *controller) processQueue(index string) error {
	ctrl.logger.Infof("Received volume restore request for %s", index)
	_, name, err := cache.SplitMetaNamespaceKey(index)
	if err != nil {
		ctrl.logger.Errorf("splitting key into namespace and name, error %s", err)
		return err
	}

	volumeRestore, err := ctrl.volumeRestoreLister.Get(name)
	if err == nil {
		newObj, err := utils.StoreRevisionUpdate(ctrl.processedVRC, volumeRestore, "VolumeRestoreContent")
		if err != nil {
			ctrl.logger.Errorf("%s", err)
		}
		if !newObj {
			ctrl.logger.Infof("Ignoring outdated volume restore request for %s", index)
			return nil
		}
		ctrl.logger.Infof("Processing volume restore request for %s", index)

		volumeRestoreClone := volumeRestore.DeepCopy()

		// delete scenario
		if volumeRestoreClone.DeletionTimestamp != nil {
			return ctrl.processDeleteVolumeRestore(volumeRestoreClone)
		}

		if isInitNeeded(volumeRestoreClone) {
			volumeRestoreClone, err = ctrl.restoreInitialize(volumeRestoreClone)
			if err != nil {
				ctrl.logger.Errorf("failed to initialize finalizer restore(%s)", index)
				return err
			}
		}
		// process create and sync
		return ctrl.processVolumeRestore(volumeRestoreClone)
	}

	return err
}

func (ctrl *controller) hasCleanupAnnotation(restore *kahuapi.VolumeRestoreContent) bool {
	if metav1.HasAnnotation(restore.ObjectMeta, annVolumeResourceCleanup) {
		return true
	}
	return false
}

func (ctrl *controller) processDeleteVolumeRestore(restore *kahuapi.VolumeRestoreContent) error {
	ctrl.logger.Infof("Processing volume backup delete request for %v", restore)

	// Delete resources if restore stage/state is not Finished/Completed
	if restore.Status.Phase == kahuapi.VolumeRestoreContentPhaseInProgress ||
		ctrl.hasCleanupAnnotation(restore) {
		restoreHandles := make([]*providerSvc.RestoreVolumeIdentity, 0)
		for _, volumeState := range restore.Status.RestoreState {
			restoreHandles = append(restoreHandles, &providerSvc.RestoreVolumeIdentity{VolumeHandle: volumeState.VolumeHandle,
				VolumeAttributes: volumeState.VolumeAttributes,
			})
		}
		_, err := ctrl.driver.CancelRestore(context.Background(), &providerSvc.CancelRestoreRequest{
			RestoreVolumeIdentity: restoreHandles, Parameters: restore.Spec.Parameters,
		})
		if err != nil {
			if utils.CheckServerUnavailable(err) {
				ctrl.logger.Errorf("Driver unavailable: Continue to retry for volume restore delete for %s",
					restore.Name)
				return err
			}
			ctrl.updater.Event(restore, v1.EventTypeWarning, EventVolumeRestoreCancelFailed,
				fmt.Sprintf("Unable to Cancel restore. %s", err.Error()))
			ctrl.logger.Errorf("Unable to cancel restore. %s", err)
			return err
		}
	}

	restoreClone := restore.DeepCopy()
	utils.RemoveFinalizer(restoreClone, volumeRestoreContentFinalizer)
	_, err := ctrl.patchVolRestoreContent(restore, restoreClone)
	if err != nil {
		ctrl.logger.Errorf("removing finalizer failed for %s", restore.Name)
		return err
	}

	ctrl.logger.Infof("Volume restore (%s) delete successfully", restore.Name)

	err = utils.StoreClean(ctrl.processedVRC, restore, "VolumeRestoreContent")
	if err != nil {
		ctrl.logger.Warningf("Failed to clean processed cache. %s", err)
	}
	return nil
}

func resetPVC(pvc *v1.PersistentVolumeClaim) *v1.PersistentVolumeClaim {
	pvcClone := pvc.DeepCopy()
	pvcClone.APIVersion = utils.GVKPersistentVolumeClaim.GroupVersion().String()
	pvcClone.Kind = utils.GVKPersistentVolumeClaim.Kind
	pvcClone.Annotations = make(map[string]string)
	pvcClone.Finalizers = make([]string, 0)
	pvcClone.Status = v1.PersistentVolumeClaimStatus{}
	return pvcClone
}

func (ctrl *controller) processVolumeRestore(restore *kahuapi.VolumeRestoreContent) error {
	logger := ctrl.logger.WithField("restore", restore.Name)

	switch restore.Status.Phase {
	case "", kahuapi.VolumeRestoreContentPhaseInit:
		newRestore, err := ctrl.handleVolumeRestore(restore)
		if err != nil {
			if utils.CheckServerUnavailable(err) {
				ctrl.logger.Errorf("Driver unavailable: Continue to retry for volume restore for %s",
					restore.Name)
				return err
			}
			if _, err = ctrl.updater.updateStatusWithFailure(newRestore, err.Error()); err != nil {
				ctrl.logger.Errorf("Failed to update restore failure status. %s", err)
			}
			return err
		}
		// update restore success
		if restore, err = ctrl.updater.updateStatusWithEvent(restore, newRestore,
			v1.EventTypeNormal, EventVolumeRestoreStarted, "Started volume restore"); err != nil {
			ctrl.logger.Errorf("Unable to update volume restore start")
			return err
		}

		fallthrough
	case kahuapi.VolumeRestoreContentPhaseInProgress:
		ctx := ctrl.newVolumeRestoreProgressContext(ctrl.logger)
		err := ctrl.operationManager.Run(restore.Name, operation.Handler{
			Operation: ctx.processRestoreProgress,
			OnFailure: ctx.handleRestoreProgressFailure,
			OnSuccess: ctx.handleRestoreProgressSuccess})
		if operation.IsAlreadyExists(err) {
			return nil
		}

		ctrl.logger.Infof("Restore operation in progress for %s", restore.Name)
		return nil
	case kahuapi.VolumeRestoreContentPhaseFailed:
		logger.Infof("Volume backup failed already")
		return nil
	default:
		logger.Infof("Ignoring volume restore state %s", restore.Status.Phase)
	}

	return nil
}

func (ctrl *controller) handleVolumeRestore(
	volRestore *kahuapi.VolumeRestoreContent) (*kahuapi.VolumeRestoreContent, error) {
	identifiers := make([]*providerSvc.RestoreIdentifier, 0)
	for _, volume := range volRestore.Spec.Volumes {
		pvc := resetPVC(volume.Claim)
		identifiers = append(identifiers, &providerSvc.RestoreIdentifier{Pvc: pvc,
			BackupIdentity: &providerSvc.BackupIdentity{BackupHandle: volume.BackupHandle,
				BackupAttributes: volume.BackupAttributes}})
	}
	ctx, cancel := context.WithTimeout(context.TODO(), defaultContextTimeout)
	defer cancel()
	response, err := ctrl.driver.CreateVolumeFromBackup(ctx,
		&providerSvc.CreateVolumeFromBackupRequest{RestoreInfo: identifiers,
			RestoreContentName: volRestore.Name, Parameters: volRestore.Spec.Parameters,
		})
	if err != nil {
		ctrl.logger.Errorf("Unable to start restore. %s", err)
		return volRestore, err
	}
	if len(response.GetErrors()) > 0 {
		return volRestore, fmt.Errorf("unable to create volume. %s",
			strings.Join(response.GetErrors(), ", "))
	}

	restoreState := make([]kahuapi.VolumeRestoreState, 0)
	for _, restoreIdentifier := range response.GetVolumeIdentifiers() {
		restoreState = append(restoreState, kahuapi.VolumeRestoreState{VolumeName: restoreIdentifier.PvcName,
			VolumeHandle:       restoreIdentifier.GetVolumeIdentity().GetVolumeHandle(),
			VolumeAttributes:   restoreIdentifier.GetVolumeIdentity().GetVolumeAttributes(),
			LastProgressUpdate: metav1.Now().Unix(),
		})
	}

	// update restore status
	newRestore := volRestore.DeepCopy()
	newRestore.Status.Phase = kahuapi.VolumeRestoreContentPhaseInProgress
	newRestore.Status.RestoreState = restoreState
	return newRestore, err
}

type restoreProgressContext struct {
	updater          volumeRestoreUpdater
	volRestoreLister kahulister.VolumeRestoreContentLister
	driver           providerSvc.VolumeBackupClient
	logger           log.FieldLogger
}

func (ctrl *controller) newVolumeRestoreProgressContext(logger log.FieldLogger) *restoreProgressContext {
	return &restoreProgressContext{
		volRestoreLister: ctrl.volumeRestoreLister,
		updater:          ctrl.updater,
		driver:           ctrl.driver,
		logger:           logger.WithField("context", "volume-restore-progress"),
	}
}

func (ctx *restoreProgressContext) skipRestoreProgressProcessing(volRestore *kahuapi.VolumeRestoreContent) bool {
	if volRestore.DeletionTimestamp != nil {
		ctx.logger.Warningf("Volume restore % is getting deleted. Exiting volume restore progress",
			volRestore.Name)
		return true
	} else if volRestore.Status.Phase != kahuapi.VolumeRestoreContentPhaseInProgress {
		ctx.logger.Infof("Volume restore is not in-progress. Skipping %s "+
			"restore progress check", volRestore.Name)
		return true
	}

	return false
}

func getVolumeRestore(index string,
	volRestoreLister kahulister.VolumeRestoreContentLister) (*kahuapi.VolumeRestoreContent, error) {
	_, name, err := cache.SplitMetaNamespaceKey(index)
	if err != nil {
		return nil, err
	}

	return volRestoreLister.Get(name)
}

func (ctx *restoreProgressContext) processRestoreProgress(index string) (bool, error) {
	ctx.logger.Infof("Checking restore progress for %s", index)
	volRestore, err := getVolumeRestore(index, ctx.volRestoreLister)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("volume restore (%s) already deleted", index)
			return true, nil
		}
		ctx.logger.Errorf("Unable to get volume restore. %s", err)
		return false, nil
	}

	if ctx.skipRestoreProgressProcessing(volRestore) {
		return true, nil
	}

	newVolRestore := volRestore.DeepCopy()
	completed, err := ctx.syncRestoreStat(newVolRestore)
	if err != nil {
		if utils.CheckServerUnavailable(err) {
			ctx.logger.Errorf("Driver unavailable: Continue to retry for status for %s", volRestore.Name)
			return false, nil
		}
		ctx.logger.Errorf("Failed to sync volume restore stat. %s", err)
		return false, err
	}

	if completed {
		newVolRestore.Status.Phase = kahuapi.VolumeRestoreContentPhaseCompleted
	}

	ctx.logger.Infof("Updating restore progress for %s", index)
	volRestore, err = ctx.updater.updateStatus(volRestore, newVolRestore)
	if err != nil {
		ctx.logger.Errorf("Unable to update volume restore states for %s. %s", volRestore.Name, err)
		return false, nil
	}

	if completed {
		ctx.logger.Infof("Volume restore completed for %s", index)
		ctx.updater.Event(volRestore, v1.EventTypeNormal, EventVolumeRestoreCompleted, "Completed volume restore")
		return true, nil
	}

	return false, nil
}

func (ctx *restoreProgressContext) syncRestoreStat(
	volRestore *kahuapi.VolumeRestoreContent) (bool, error) {
	restoreInfo := make([]*providerSvc.RestoreVolumeIdentity, 0)
	for _, state := range volRestore.Status.RestoreState {
		restoreInfo = append(restoreInfo, &providerSvc.RestoreVolumeIdentity{
			VolumeHandle:     state.VolumeHandle,
			VolumeAttributes: state.VolumeAttributes,
		})
	}

	driverCtx, cancel := context.WithTimeout(context.TODO(), defaultContextTimeout)
	defer cancel()
	stats, err := ctx.driver.GetRestoreStat(driverCtx, &providerSvc.GetRestoreStatRequest{
		RestoreVolumeIdentity: restoreInfo,
		Parameters:            volRestore.Spec.Parameters,
	})
	if err != nil {
		ctx.logger.Errorf("Unable to get volume restore state for %s. %s", volRestore.Name, err)
		return false, err
	}

	// update restore status
	for _, stat := range stats.RestoreVolumeStat {
		for i, state := range volRestore.Status.RestoreState {
			if state.VolumeHandle == stat.RestoreVolumeHandle &&
				stat.Progress > state.Progress {
				volRestore.Status.RestoreState[i].LastProgressUpdate = metav1.Now().Unix()
				volRestore.Status.RestoreState[i].Progress = stat.Progress
			}
		}
	}

	progressCompleted := true
	for _, state := range volRestore.Status.RestoreState {
		if state.Progress < 100 {
			progressCompleted = false
			break
		}
	}

	progressTimeout := true
	for _, volStat := range volRestore.Status.RestoreState {
		if time.Since(time.Unix(volStat.LastProgressUpdate, 0)) < defaultRestoreProgressTimeout {
			progressTimeout = false
		}
	}

	if progressTimeout {
		volRestore.Status.FailureReason = fmt.Sprintf("Volume restore timout. No progress update for "+
			"max wait duration %s", defaultRestoreProgressTimeout.String())
		volRestore.Status.Phase = kahuapi.VolumeRestoreContentPhaseFailed
	}

	return progressCompleted, nil
}

func (ctx *restoreProgressContext) handleRestoreProgressFailure(index string, progressErr error) {
	volRestore, err := getVolumeRestore(index, ctx.volRestoreLister)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("volume restore (%s) already deleted", index)
			return
		}
		ctx.logger.Errorf("Unable to get volume restore. %s", err)
		return
	}

	if ctx.skipRestoreProgressProcessing(volRestore) {
		return
	}

	_, err = ctx.updater.updateStatusWithFailure(volRestore, progressErr.Error())
	if err != nil {
		ctx.logger.Errorf("Failed to update restore progress failure(%s). %s", progressErr, err)
	}
	return
}

func (ctx *restoreProgressContext) handleRestoreProgressSuccess(index string) {
	volRestore, err := getVolumeRestore(index, ctx.volRestoreLister)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("volume restore (%s) already deleted", index)
			return
		}
		ctx.logger.Errorf("Unable to get volume restore. %s", err)
		return
	}

	if ctx.skipRestoreProgressProcessing(volRestore) {
		return
	}

	_, err = ctx.updater.updatePhase(volRestore, kahuapi.VolumeRestoreContentPhaseCompleted)
	if err != nil {
		ctx.logger.Errorf("Failed to update restore progress success. %s", err)
	}
	return
}

func isInitNeeded(restore *kahuapi.VolumeRestoreContent) bool {
	if restore.Status.Phase == "" ||
		restore.Status.Phase == kahuapi.VolumeRestoreContentPhaseInit {
		return true
	}

	return false
}

func (ctrl *controller) restoreInitialize(
	restore *kahuapi.VolumeRestoreContent) (*kahuapi.VolumeRestoreContent, error) {
	restoreClone := restore.DeepCopy()

	utils.SetFinalizer(restoreClone, volumeRestoreContentFinalizer)
	restoreClone, err := ctrl.patchVolRestoreContent(restore, restoreClone)
	if err != nil {
		return restoreClone, err
	}

	status := kahuapi.VolumeRestoreContentStatus{}
	if restoreClone.Status.Phase == "" {
		status.Phase = kahuapi.VolumeRestoreContentPhaseInit
	}
	if restore.Status.StartTimestamp == nil {
		currentTime := metav1.Now()
		status.StartTimestamp = &currentTime
	}
	return ctrl.updateStatus(restoreClone, status)
}

func (ctrl *controller) patchVolRestoreContent(
	oldRestore,
	newRestore *kahuapi.VolumeRestoreContent) (*kahuapi.VolumeRestoreContent, error) {
	origBytes, err := json.Marshal(oldRestore)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original restore")
	}

	updatedBytes, err := json.Marshal(newRestore)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated restore")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for restore")
	}

	updatedRestore, err := ctrl.volumeRestoreClient.Patch(context.TODO(),
		oldRestore.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "error patching restore")
	}

	_, err = utils.StoreRevisionUpdate(ctrl.processedVRC, updatedRestore, "VolumeRestoreContent")
	if err != nil {
		return updatedRestore, errors.Wrap(err, "Failed to updated processed VRC cache")
	}

	return updatedRestore, nil
}

func (ctrl *controller) updateStatus(
	restore *kahuapi.VolumeRestoreContent,
	status kahuapi.VolumeRestoreContentStatus) (*kahuapi.VolumeRestoreContent, error) {
	ctrl.logger.Infof("Updating status: volume restore content %s", restore.Name)
	if status.Phase != "" &&
		status.Phase != restore.Status.Phase {
		restore.Status.Phase = status.Phase
	}

	if status.FailureReason != "" {
		restore.Status.FailureReason = status.FailureReason
	}

	if restore.Status.StartTimestamp == nil &&
		status.StartTimestamp != nil {
		restore.Status.StartTimestamp = status.StartTimestamp
	}

	if status.RestoreState != nil {
		mergeStatusResources(restore, status)
	}

	updatedRestore, err := ctrl.volumeRestoreClient.UpdateStatus(context.TODO(),
		restore,
		metav1.UpdateOptions{})
	if err != nil {
		return updatedRestore, errors.Wrap(err, "error patching restore")
	}

	_, err = utils.StoreRevisionUpdate(ctrl.processedVRC, updatedRestore, "VolumeRestoreContent")
	if err != nil {
		return updatedRestore, errors.Wrap(err, "Failed to updated processed VRC cache")
	}

	return updatedRestore, err
}

func mergeStatusResources(restore *kahuapi.VolumeRestoreContent,
	status kahuapi.VolumeRestoreContentStatus) {
	newStates := make([]kahuapi.VolumeRestoreState, 0)
	for _, state := range status.RestoreState {
		found := false
		for _, restoreState := range restore.Status.RestoreState {
			if state.VolumeName == restoreState.VolumeName {
				found = true
				break
			}
		}
		if !found {
			newStates = append(newStates, state)
		}
	}
	restore.Status.RestoreState = append(restore.Status.RestoreState, newStates...)
}

func (updater *volumeRestoreUpdater) Event(restore *kahuapi.VolumeRestoreContent,
	eventType, reason, message string) {
	updater.eventRecorder.Event(restore, eventType, reason, message)
}

func (updater *volumeRestoreUpdater) updateStatusWithEvent(restore, newRestore *kahuapi.VolumeRestoreContent,
	eventType, reason, message string) (*kahuapi.VolumeRestoreContent, error) {
	newRestore, err := updater.updateStatus(restore, newRestore)
	if err != nil {
		return newRestore, err
	}

	updater.Event(newRestore, eventType, reason, message)
	return newRestore, err
}

func (updater *volumeRestoreUpdater) updateStatusWithFailure(restore *kahuapi.VolumeRestoreContent,
	msg string) (*kahuapi.VolumeRestoreContent, error) {
	newRestore := restore.DeepCopy()
	newRestore.Status.Phase = kahuapi.VolumeRestoreContentPhaseFailed
	newRestore.Status.FailureReason = msg
	restore, err := updater.patchRestoreStatus(restore, newRestore)
	if err != nil {
		return restore, err
	}

	updater.Event(restore, v1.EventTypeWarning, EventVolumeRestoreFailed, msg)
	return restore, err
}

func (updater *volumeRestoreUpdater) updateStatus(restore,
	newRestore *kahuapi.VolumeRestoreContent) (*kahuapi.VolumeRestoreContent, error) {
	return updater.patchRestoreStatus(restore, newRestore)
}

func (updater *volumeRestoreUpdater) updatePhaseWithEvent(restore *kahuapi.VolumeRestoreContent,
	phase kahuapi.VolumeRestoreContentPhase,
	eventType, reason, message string) (*kahuapi.VolumeRestoreContent, error) {
	newRestore, err := updater.updatePhase(restore, phase)
	if err != nil {
		return newRestore, err
	}

	updater.Event(newRestore, eventType, reason, message)
	return newRestore, err
}

func (updater *volumeRestoreUpdater) updatePhase(restore *kahuapi.VolumeRestoreContent,
	phase kahuapi.VolumeRestoreContentPhase) (*kahuapi.VolumeRestoreContent, error) {
	restoreClone := restore.DeepCopy()
	restoreClone.Status.Phase = phase
	return updater.patchRestoreStatus(restore, restoreClone)
}

func (updater *volumeRestoreUpdater) patchRestore(
	oldRestore,
	newRestore *kahuapi.VolumeRestoreContent) (*kahuapi.VolumeRestoreContent, error) {
	origBytes, err := json.Marshal(oldRestore)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original restore")
	}

	updatedBytes, err := json.Marshal(newRestore)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated restore")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for restore")
	}

	updatedRestore, err := updater.volumeRestoreClient.Patch(context.TODO(),
		oldRestore.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "error patching restore")
	}

	_, err = utils.StoreRevisionUpdate(updater.store, updatedRestore, "VolumeRestoreContent")
	if err != nil {
		return updatedRestore, errors.Wrap(err, "Failed to updated processed VBC cache")
	}

	return updatedRestore, nil
}

func (updater *volumeRestoreUpdater) patchRestoreStatus(
	oldRestore,
	newRestore *kahuapi.VolumeRestoreContent) (*kahuapi.VolumeRestoreContent, error) {
	origBytes, err := json.Marshal(oldRestore)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original restore")
	}

	updatedBytes, err := json.Marshal(newRestore)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated restore")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for restore")
	}

	updatedRestore, err := updater.volumeRestoreClient.Patch(context.TODO(),
		oldRestore.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
		"status")
	if err != nil {
		return nil, errors.Wrap(err, "error patching restore")
	}

	_, err = utils.StoreRevisionUpdate(updater.store, updatedRestore, "VolumeRestoreContent")
	if err != nil {
		return updatedRestore, errors.Wrap(err, "Failed to updated processed VBC cache")
	}

	return updatedRestore, nil
}
