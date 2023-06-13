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
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/providerframework/volumeservice/app/config"
	volumeservice "github.com/soda-cdm/kahu/providerframework/volumeservice/lib/go"
	providerSvc "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/utils"
	"github.com/soda-cdm/kahu/utils/k8sresource"
	"github.com/soda-cdm/kahu/utils/operation"
)

const (
	controllerName        = "volume-content-backup"
	volumeBackupFinalizer = "kahu.io/volume-backup-protection"
	defaultContextTimeout = 30 * time.Second
	defaultPollTime       = 5 * time.Second

	EventVolumeBackupFailed       = "VolumeBackupFailed"
	EventVolumeBackupStarted      = "VolumeBackupStarted"
	EventVolumeBackupCompleted    = "VolumeBackupCompleted"
	EventVolumeBackupDeleteFailed = "VolumeBackupDeleteFailed"
	EventVolumeBackupCancelFailed = "VolumeBackupCancelFailed"
)

type VolumeBackupService struct {
	logger           log.FieldLogger
	providerName     string
	driver           providerSvc.VolumeBackupClient
	operationManager operation.Manager
	kubeClient       kubernetes.Interface
	kahuClient       versioned.Interface
	manager          operation.Manager
	// csiSnapshot      snapshotv1.SnapshotV1Interface
}

func NewVolumeBackupService(
	config *config.CompletedConfig,
	driverClient providerSvc.VolumeBackupClient,
) *VolumeBackupService {
	logger := log.WithField("module", controllerName)
	service := &VolumeBackupService{
		logger:       logger,
		providerName: config.Provider,
		driver:       driverClient,
		kubeClient:   config.KubeClient,
		kahuClient:   config.KahuClient,
		manager:      operation.NewOperationManager(),
	}

	return service
}

func (ctrl *VolumeBackupService) getVolumeBackup(ctx context.Context,
	index string) (*kahuapi.VolumeBackupContent, error) {
	_, name, err := cache.SplitMetaNamespaceKey(index)
	if err != nil {
		return nil, err
	}

	return ctrl.kahuClient.KahuV1beta1().VolumeBackupContents().Get(ctx, name, metav1.GetOptions{})
}

func (ctrl *VolumeBackupService) Backup(ctx context.Context,
	backup *kahuapi.VolumeBackupContent,
	stream volumeservice.VolumeService_BackupServer) error {
	logger := ctrl.logger.WithField("backup", backup.Name)

	// check and lock backup handling
	if ctrl.manager.Exist(backup.Name) {
		return status.Errorf(codes.OutOfRange, "Volume backup[%s] already in progress", backup.Name)
	}
	ctrl.manager.Add(backup.Name)
	defer ctrl.manager.Delete(backup.Name)

	switch backup.Status.Phase {
	case "", kahuapi.VolumeBackupContentPhaseInit:
		err := ctrl.handleBackupStart(ctx, backup, stream)
		if err != nil {
			if utils.CheckServerUnavailable(err) {
				ctrl.logger.Errorf("Driver unavailable: Continue to retry for volume backup for %s", backup.Name)
				return status.Errorf(codes.FailedPrecondition,
					"Driver unavailable: Continue to retry for volume backup for %s", backup.Name)
			}
			return err
		}

		ctrl.sendBackupEvent(backup, stream,
			v1.EventTypeNormal,
			EventVolumeBackupStarted,
			"Started volume backup")
		fallthrough
	case kahuapi.VolumeBackupContentPhaseInProgress:
		ctrl.logger.Infof("Backup operation in progress for %s", backup.Name)
		return ctrl.handleBackupProgress(backup, stream)
	case kahuapi.VolumeBackupContentPhaseFailed:
		logger.Infof("Volume backup[%s] failed", backup.Name)
		return status.Errorf(codes.OutOfRange, "Volume backup[%s] failed", backup.Name)
	case kahuapi.VolumeBackupContentPhaseCompleted:
		logger.Infof("Volume backup successful")
		return nil
	case kahuapi.VolumeBackupContentPhaseDeleting:
		return status.Errorf(codes.OutOfRange, "Volume backup[%s] getting deleted already", backup.Name)
	default:
		logger.Infof("Ignoring volume backup state (%s).", backup.Status.Phase)
		return status.Errorf(codes.OutOfRange, "Volume backup[%s] is in invalid phase[%s]", backup.Name,
			backup.Status.Phase)
	}
}

func (ctrl *VolumeBackupService) handleBackupProgress(backup *kahuapi.VolumeBackupContent,
	stream volumeservice.VolumeService_BackupServer) error {
	var t clock.Timer
	backoff := wait.NewJitteredBackoffManager(defaultPollTime, 0.0, &clock.RealClock{})
	for {
		select {
		case <-stream.Context().Done():
			ctrl.logger.Info("Context completed")
			return nil
		default:
		}

		// check backup progress
		err := ctrl.processBackupProgress(backup.Name, stream)
		if err != nil {
			return err
		}

		//reset timer
		t = backoff.Backoff()
		select {
		case <-stream.Context().Done():
			ctrl.logger.Info("Context completed")
			return nil
		case <-t.C():
		}
	}
}

//func (ctrl *VolumeBackupService) isValidDeleteWithBackup(ownerReference metav1.OwnerReference,
//	volBackup *kahuapi.VolumeBackupContent) (bool, error) {
//	backup, err := ctrl.kahuClient.KahuV1beta1().Backups().Get(context.TODO(), ownerReference.Name, metav1.GetOptions{})
//	if err != nil && !apierrors.IsNotFound(err) {
//		return false, err
//	} else if apierrors.IsNotFound(err) {
//		return true, nil
//	}
//
//	if backup.UID != ownerReference.UID { // if owner with same name but different UID exist
//		return true, nil
//	}
//	if backup.DeletionTimestamp == nil {
//		//ctrl.updater.Event(volBackup, v1.EventTypeWarning, utils.EventOwnerNotDeleted,
//		//	"Owner backup not deleted")
//		ctrl.logger.Errorf("Backup %s not deleted. Ignoring VolumeBackupContent(%s) delete",
//			ownerReference.Name, volBackup.Name)
//		return false, nil
//	}
//
//	return true, nil
//}
//
//func (ctrl *VolumeBackupService) isValidDelete(volBackup *kahuapi.VolumeBackupContent) (bool, error) {
//	// process VolumeBackupContent delete if respective restore object is getting deleted
//	for _, ownerReference := range volBackup.OwnerReferences {
//		switch ownerReference.Kind {
//		case utils.GVKBackup.Kind:
//			valid, err := ctrl.isValidDeleteWithBackup(ownerReference, volBackup)
//			if err != nil {
//				return false, err
//			}
//			if !valid {
//				return false, nil
//			}
//		default:
//			continue
//		}
//	}
//
//	return true, nil
//}

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

func (ctrl *VolumeBackupService) cancelVolBackup(ctx context.Context,
	volBackup *kahuapi.VolumeBackupContent,
	backupIdentifiers []*providerSvc.BackupIdentity,
	backupParams map[string]string) error {
	logger := ctrl.logger.WithField("volume-backup", volBackup.Name)
	ctx, cancel := context.WithTimeout(ctx, defaultContextTimeout)
	defer cancel()
	_, err := ctrl.driver.CancelBackup(ctx, &providerSvc.CancelBackupRequest{
		BackupInfo: backupIdentifiers, Parameters: backupParams,
	})
	if err != nil {
		if utils.CheckServerUnavailable(err) {
			ctrl.logger.Errorf("Driver unavailable: Continue to retry for volume backup cancel for %s",
				volBackup.Name)
			return status.Errorf(codes.FailedPrecondition,
				"Driver unavailable: Continue to retry for volume backup for %s", volBackup.Name)
		}
		//ctrl.updater.Event(volBackup, v1.EventTypeWarning, EventVolumeBackupCancelFailed,
		//	fmt.Sprintf("Unable to Cancel backup. %s", err.Error()))
		logger.Errorf("Unable to Cancel backup. %s", err)
		return status.Errorf(codes.Aborted, "Unable to Cancel backup. %s", err)
	}

	return nil
}

func (ctrl *VolumeBackupService) DeleteBackup(ctx context.Context, volBackup *kahuapi.VolumeBackupContent) error {
	logger := ctrl.logger.WithField("volume-backup", volBackup.Name)
	logger.Infof("Initializing deletion for volume backup content(%s)", volBackup.Name)
	//valid, err := ctrl.isValidDelete(volBackup)
	//if err != nil {
	//	return err
	//} else if !valid {
	//	return nil
	//}

	//volBackup, err = ctrl.updater.updatePhase(volBackup, kahuapi.VolumeBackupContentPhaseDeleting)
	//if err != nil {
	//	if apierrors.IsNotFound(err) {
	//		return nil
	//	}
	//	logger.Errorf("Unable to update deletion status with err %s", err)
	//	return err
	//}

	backupParams := volBackup.Spec.Parameters
	backupIdentifiers := getVolIdentifiers(&volBackup.Status)
	if volBackup.Status.Phase == kahuapi.VolumeBackupContentPhaseInProgress {
		if err := ctrl.cancelVolBackup(ctx, volBackup, backupIdentifiers, backupParams); err != nil {
			return err
		}
	}

	logger.Infof("Initiating volume delete driver call for %s ", volBackup.Name)
	ctx, cancel := context.WithTimeout(ctx, defaultContextTimeout)
	defer cancel()
	_, err := ctrl.driver.DeleteBackup(ctx, &providerSvc.DeleteBackupRequest{BackupContentName: volBackup.Name,
		BackupInfo: backupIdentifiers, Parameters: backupParams,
	})
	if err != nil {
		if utils.CheckServerUnavailable(err) {
			ctrl.logger.Errorf("Driver unavailable: Continue to retry for volume backup delete for %s",
				volBackup.Name)
			return status.Errorf(codes.FailedPrecondition,
				"Driver unavailable: Continue to retry for volume backup for %s", volBackup.Name)
		}
		//ctrl.updater.Event(volBackup, v1.EventTypeWarning, EventVolumeBackupDeleteFailed,
		//	fmt.Sprintf("Unable to delete backup. %s", err.Error()))
		logger.Errorf("Unable to delete backup. %s", err)
		return status.Errorf(codes.Aborted, "Unable to delete backup. %s", err)
	}

	//if err := ctrl.ensureFinalizerRemove(volBackup); err != nil {
	//	return err
	//}

	return nil
}

func (ctrl *VolumeBackupService) ensureFinalizerRemove(volBackup *kahuapi.VolumeBackupContent) error {
	if utils.ContainsFinalizer(volBackup, volumeBackupFinalizer) {
		backupClone := volBackup.DeepCopy()
		utils.RemoveFinalizer(backupClone, volumeBackupFinalizer)
		//if _, err := ctrl.updater.patchBackup(volBackup, backupClone); err != nil {
		//	ctrl.logger.Errorf("removing finalizer failed for %s", volBackup.Name)
		//	return err
		//}
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

func (ctrl *VolumeBackupService) constructBackupRequest(
	volBackup *kahuapi.VolumeBackupContent) (*providerSvc.StartBackupRequest, sets.String, error) {
	if len(volBackup.Spec.VolumeMount) != 0 {
		// handle backup volume mount here
		return nil, sets.NewString(), status.Error(codes.Unauthenticated,
			"Volume mount handling not implemented")
	}

	return ctrl.getVolBackupReqByRef(volBackup)
}

func (ctrl *VolumeBackupService) getVolBackupReqByRef(
	volBackup *kahuapi.VolumeBackupContent) (*providerSvc.StartBackupRequest, sets.String, error) {
	volBackupInfos := make([]*providerSvc.VolBackup, 0)
	volNames := sets.NewString()
	for _, ref := range volBackup.Spec.VolumeRef {
		volBackupInfo := new(providerSvc.VolBackup)
		pv, err := ctrl.getVolume(ref.Volume)
		if err != nil {
			return nil, sets.NewString(), err
		}
		volBackupInfo.Pv = pv
		volNames.Insert(pv.Name)
		if ref.Snapshot != nil {
			snapshot := &providerSvc.Snapshot{
				SnapshotAttributes: ref.Snapshot.Attribute,
				SnapshotHandle:     ref.Snapshot.Handle,
			}
			volBackupInfo.Snapshot = snapshot
		}
		volBackupInfos = append(volBackupInfos, volBackupInfo)
	}

	return &providerSvc.StartBackupRequest{
		BackupContentName: volBackup.Name,
		BackupInfo:        volBackupInfos,
		Parameters:        volBackup.Spec.Parameters}, volNames, nil
}

//func (ctrl *VolumeBackupService) getVolBackupReqBySnapshot(backupName string,
//	backupParameter map[string]string,
//	volBackupRef *kahuapi.ResourceReference) (*providerSvc.StartBackupRequest, sets.String, error) {
//	// only snapshot objects
//	kahuVolSnapshot, err := ctrl.kahuClient.
//		KahuV1beta1().
//		VolumeSnapshots().
//		Get(context.TODO(), volBackupRef.Name, metav1.GetOptions{})
//	if err != nil {
//		return nil, nil, err
//	}
//
//	volBackupInfo := make([]*providerSvc.VolBackup, 0)
//	volNames := sets.NewString()
//	for _, state := range kahuVolSnapshot.Status.SnapshotStates {
//		snapshot, err := ctrl.getSnapshotBySnapshotState(state)
//		if err != nil {
//			return nil, nil, err
//		}
//		pv, err := ctrl.getPVBySnapshotState(state)
//		if err != nil {
//			return nil, nil, err
//		}
//		volNames.Insert(pv.Name)
//		volBackupInfo = append(volBackupInfo, &providerSvc.VolBackup{
//			Snapshot: snapshot,
//			Pv:       pv,
//		})
//	}
//
//	return &providerSvc.StartBackupRequest{
//		BackupContentName: backupName,
//		BackupInfo:        volBackupInfo,
//		Parameters:        backupParameter}, volNames, nil
//}
//
//func (ctrl *VolumeBackupService) getVolBackupReqByVolumeGroup(backupName string,
//	backupParameter map[string]string,
//	volBackupRef *kahuapi.ResourceReference) (*providerSvc.StartBackupRequest, sets.String, error) {
//	// only PVC objects
//	kahuVolGroup, err := ctrl.kahuClient.
//		KahuV1beta1().
//		VolumeGroups().
//		Get(context.TODO(), volBackupRef.Name, metav1.GetOptions{})
//	if err != nil {
//		return nil, nil, err
//	}
//
//	volBackupInfo := make([]*providerSvc.VolBackup, 0)
//	volNames := sets.NewString()
//	for _, vol := range kahuVolGroup.Status.Volumes {
//		pv, err := ctrl.getVolume(vol)
//		if err != nil {
//			return nil, nil, err
//		}
//		volNames.Insert(pv.Name)
//		volBackupInfo = append(volBackupInfo, &providerSvc.VolBackup{
//			Pv: pv,
//		})
//	}
//
//	return &providerSvc.StartBackupRequest{
//		BackupContentName: backupName,
//		BackupInfo:        volBackupInfo,
//		Parameters:        backupParameter}, volNames, nil
//}

//func (ctrl *VolumeBackupService) getSnapshotBySnapshotState(
//	state kahuapi.VolumeSnapshotState) (*providerSvc.Snapshot, error) {
//	//csiSnapshotRef := state.CSISnapshotRef
//	//
//	//if csiSnapshotRef == nil {
//	//	return nil, fmt.Errorf("csi Snapshot info not populated")
//	//}
//	//
//	//csiVolSnapshot, err := ctrl.csiSnapshot.VolumeSnapshots(csiSnapshotRef.Namespace).
//	//	Get(context.TODO(), csiSnapshotRef.Name, metav1.GetOptions{})
//	//if err != nil {
//	//	return nil, err
//	//}
//	//
//	//csiVolSnapshotContent, err := ctrl.csiSnapshot.VolumeSnapshotContents().
//	//	Get(context.TODO(), *csiVolSnapshot.Status.BoundVolumeSnapshotContentName, metav1.GetOptions{})
//	//if err != nil {
//	//	return nil, err
//	//}
//	//
//	//return &providerSvc.Snapshot{
//	//	SnapshotHandle: *csiVolSnapshotContent.Status.SnapshotHandle,
//	//}, nil
//
//	return &providerSvc.Snapshot{
//		SnapshotHandle: nil,
//	}, nil
//}

func (ctrl *VolumeBackupService) getVolume(
	volume kahuapi.ResourceReference) (*v1.PersistentVolume, error) {
	volName := volume.Name
	if volume.Kind == k8sresource.PersistentVolumeClaimGVK.Kind {
		pvc, err := ctrl.kubeClient.CoreV1().PersistentVolumeClaims(volume.Namespace).
			Get(context.TODO(), volume.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		volName = pvc.Spec.VolumeName
	}

	return ctrl.kubeClient.CoreV1().PersistentVolumes().
		Get(context.TODO(), volName, metav1.GetOptions{})
}

//func (ctrl *VolumeBackupService) getPVBySnapshotState(
//	state kahuapi.VolumeSnapshotState) (*v1.PersistentVolume, error) {
//	pvc, err := ctrl.kubeClient.CoreV1().PersistentVolumeClaims(state.PVC.Namespace).
//		Get(context.TODO(), state.PVC.Name, metav1.GetOptions{})
//	if err != nil {
//		return nil, err
//	}
//
//	return ctrl.kubeClient.CoreV1().PersistentVolumes().
//		Get(context.TODO(), pvc.Spec.VolumeName, metav1.GetOptions{})
//}

//func (ctrl *VolumeBackupService) getPersistentVols(
//	vbcVols []v1.PersistentVolume) ([]*v1.PersistentVolume, sets.String, error) {
//	volNames := sets.NewString()
//	volumes := make([]*v1.PersistentVolume, 0)
//	for _, vbcVol := range vbcVols {
//		ctrl.logger.Infof("Getting volume details for pv  (%s) ", vbcVol.Name)
//		pv, err := ctrl.kubeClient.CoreV1().PersistentVolumes().Get(context.TODO(), vbcVol.Name, metav1.GetOptions{})
//		if apierrors.IsNotFound(err) {
//			ctrl.logger.Errorf("request PV (%s) backup not found", vbcVol.Name)
//			return volumes, volNames, fmt.Errorf("request PV (%s) backup not found", vbcVol.Name)
//		}
//		if err != nil {
//			ctrl.logger.Errorf("Failed to fetch volume details for the pv (%s) err (%s) ", vbcVol.Name, err)
//			return volumes, volNames, err
//		}
//
//		volNames.Insert(pv.Name)
//		// ensure APIVersion and Kind for PV
//		pv.APIVersion = vbcVol.APIVersion
//		pv.Kind = vbcVol.Kind
//		volumes = append(volumes, pv)
//	}
//
//	return volumes, volNames, nil
//}

func (ctrl *VolumeBackupService) handleBackupStart(ctx context.Context,
	volBackup *kahuapi.VolumeBackupContent,
	stream volumeservice.VolumeService_BackupServer) error {
	volNames := sets.NewString()

	backupReq, volNames, err := ctrl.constructBackupRequest(volBackup)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, defaultContextTimeout)
	defer cancel()
	response, err := ctrl.driver.StartBackup(ctx, backupReq)
	if err != nil {
		ctrl.logger.Errorf("Unable to start backup. %s", err)
		return err
	}

	if response != nil && len(response.Errors) > 0 {
		msg := fmt.Sprintf("Unable to start backup. err: (%s)", strings.Join(response.Errors, " ,"))
		ctrl.logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}

	if err = validateBackupResponse(volNames, response.GetBackupInfo()); err != nil {
		ctrl.logger.Warningf("Invalid backup response from driver. %s", err)
		return err
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

	return ctrl.sendBackupState(volBackup, stream, backupState)
}

func toBackupProgress(state kahuapi.VolumeBackupState) *volumeservice.BackupProgress {
	return &volumeservice.BackupProgress{
		Volume:          state.VolumeName,
		BackupHandle:    state.BackupHandle,
		BackupAttribute: state.BackupAttributes,
		Progress:        state.Progress,
	}
}

func (ctrl *VolumeBackupService) sendBackupState(backup *kahuapi.VolumeBackupContent,
	stream volumeservice.VolumeService_BackupServer,
	states []kahuapi.VolumeBackupState) error {
	backupProgress := make([]*volumeservice.BackupProgress, 0)
	for _, state := range states {
		backupProgress = append(backupProgress, toBackupProgress(state))
	}

	err := stream.Send(&volumeservice.BackupResponse{
		Name: backup.Name,
		Data: &volumeservice.BackupResponse_State{
			State: &volumeservice.BackupState{
				Progress: backupProgress,
			},
		},
	})
	if err != nil {
		ctrl.logger.Warningf("Unable to send backup state. %s", err)
	}
	return nil
}

func toBackupEvent(etype, event, msg string) *volumeservice.BackupResponse_Event {
	return &volumeservice.BackupResponse_Event{
		Event: &volumeservice.Event{
			Type:    etype,
			Name:    event,
			Message: msg,
		},
	}
}

func (ctrl *VolumeBackupService) sendBackupEvent(backup *kahuapi.VolumeBackupContent,
	stream volumeservice.VolumeService_BackupServer,
	etype, event, msg string) {
	err := stream.Send(&volumeservice.BackupResponse{
		Name: backup.Name,
		Data: toBackupEvent(etype, event, msg),
	})
	if err != nil {
		ctrl.logger.Warningf("Unable to send backup event. %s", err)
	}
}

func (ctrl *VolumeBackupService) skipBackupProgressProcessing(volBackup *kahuapi.VolumeBackupContent) bool {
	if volBackup.DeletionTimestamp != nil {
		ctrl.logger.Warningf("Volume backup[%] is getting deleted. Exiting volume backup progress",
			volBackup.Name)
		return true
	} else if volBackup.Status.Phase != kahuapi.VolumeBackupContentPhaseInProgress {
		ctrl.logger.Infof("Volume backup is not in-progress. Skipping %s "+
			"backup progress check", volBackup.Name)
		return true
	}

	return false
}

func (ctrl *VolumeBackupService) processBackupProgress(index string,
	stream volumeservice.VolumeService_BackupServer) error {
	ctrl.logger.Infof("Checking backup progress for %s", index)
	volBackup, err := ctrl.getVolumeBackup(stream.Context(), index)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Errorf("volume backup (%s) already deleted", index)
			return status.Errorf(codes.NotFound, "volume backup[%s] already deleted", index)
		}
		ctrl.logger.Errorf("Unable to get volume backup. %s", err)
		return nil
	}

	if ctrl.skipBackupProgressProcessing(volBackup) {
		return status.Errorf(codes.OutOfRange, "volume backup[%s] not in progress", index)
	}

	err = ctrl.syncBackupStat(volBackup, stream)
	if err != nil {
		if utils.CheckServerUnavailable(err) {
			ctrl.logger.Errorf("Driver unavailable: Continue to retry for volume backup stat for %s",
				volBackup.Name)
			return nil
		}
		ctrl.logger.Errorf("Failed to sync volume backup stat. %s", err)
		return nil
	}

	return nil
}

func (ctrl *VolumeBackupService) syncBackupStat(volBackup *kahuapi.VolumeBackupContent,
	stream volumeservice.VolumeService_BackupServer) error {
	backupInfo := make([]*providerSvc.BackupIdentity, 0)
	for _, state := range volBackup.Status.BackupState {
		backupInfo = append(backupInfo, &providerSvc.BackupIdentity{
			BackupHandle:     state.BackupHandle,
			BackupAttributes: state.BackupAttributes,
		})
	}

	driverCtx, cancel := context.WithTimeout(context.TODO(), defaultContextTimeout)
	defer cancel()
	stats, err := ctrl.driver.GetBackupStat(driverCtx, &providerSvc.GetBackupStatRequest{
		BackupInfo: backupInfo,
		Parameters: volBackup.Spec.Parameters,
	})
	if err != nil {
		ctrl.logger.Errorf("Unable to get volume backup state for %s. %s", volBackup.Name, err)
		return err
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

	return ctrl.sendBackupState(volBackup, stream, volBackup.Status.BackupState)
}
