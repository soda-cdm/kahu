package backup

import (
	"context"
	"encoding/json"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
)

type Updater interface {
	PatchBackup(oldBackup, newBackup *kahuapi.Backup) (*kahuapi.Backup, error)
	updateBackupStatus(backup *kahuapi.Backup, status kahuapi.BackupStatus) (*kahuapi.Backup, error)
	updateBackupStatusWithEvent(
		backup *kahuapi.Backup,
		status kahuapi.BackupStatus,
		eventType, reason, message string) (*kahuapi.Backup, error)
}

var _ Updater = new(controller)

func (ctrl *controller) PatchBackup(oldBackup, newBackup *kahuapi.Backup) (*kahuapi.Backup, error) {
	return ctrl.patchBackup(oldBackup, newBackup)
}

func (ctrl *controller) patchBackup(oldBackup, newBackup *kahuapi.Backup) (*kahuapi.Backup, error) {
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

	updatedBackup, err := ctrl.backupClient.Patch(context.TODO(),
		oldBackup.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "error patching backup")
	}

	_, err = utils.StoreRevisionUpdate(ctrl.processedBackup, updatedBackup, "Backup")
	if err != nil {
		return updatedBackup, errors.Wrap(err, "Failed to updated processed backup cache")
	}

	return updatedBackup, nil
}

func (ctrl *controller) updateBackupStatus(
	backup *kahuapi.Backup,
	status kahuapi.BackupStatus) (*kahuapi.Backup, error) {
	var err error

	backupClone := backup.DeepCopy()
	dirty := false
	// update Phase
	if status.Stage != "" && toOrdinal(backup.Status.Stage) < toOrdinal(status.Stage) {
		backupClone.Status.Stage = status.Stage
		dirty = true
	}

	if status.State != "" && backup.Status.State != status.State {
		backupClone.Status.State = status.State
		dirty = true
	}

	// update Validation error
	if len(status.ValidationErrors) > 0 {
		backupClone.Status.ValidationErrors = status.ValidationErrors
		dirty = true
	}

	// update Start time
	if backup.Status.StartTimestamp == nil &&
		status.StartTimestamp != nil {
		backupClone.Status.StartTimestamp = status.StartTimestamp
		dirty = true
	}

	// update Start time
	if backup.Status.CompletionTimestamp == nil &&
		status.CompletionTimestamp != nil {
		backupClone.Status.CompletionTimestamp = status.CompletionTimestamp
		dirty = true
	}

	if backup.Status.LastBackup == nil &&
		status.LastBackup != nil {
		backupClone.Status.LastBackup = status.LastBackup
		dirty = true
	}

	if len(status.Resources) > 0 {
		mergeStatusResources(backupClone, status)
		dirty = true
	}

	if dirty {
		backupClone, err = ctrl.backupClient.UpdateStatus(context.TODO(), backupClone, metav1.UpdateOptions{})
		if err != nil {
			ctrl.logger.Errorf("Updating backup(%s) status: update status failed %s", backup.Name, err)
		}
		_, err = utils.StoreRevisionUpdate(ctrl.processedBackup, backupClone, "Backup")
		if err != nil {
			return backupClone, errors.Wrap(err, "Failed to updated processed backup cache")
		}

	}

	return backupClone, err
}

func mergeStatusResources(backup *kahuapi.Backup,
	status kahuapi.BackupStatus) {
	newResources := make([]kahuapi.BackupResource, 0)
	for _, resource := range status.Resources {
		found := false
		for _, backupResource := range backup.Status.Resources {
			if backupResource.Namespace == resource.Namespace &&
				backupResource.ResourceName == resource.ResourceName {
				found = true
				break
			}
		}
		if !found {
			newResources = append(newResources, resource)
		}
	}
	backup.Status.Resources = append(backup.Status.Resources, newResources...)
}

func (ctrl *controller) updateBackupStatusWithEvent(
	backup *kahuapi.Backup,
	status kahuapi.BackupStatus,
	eventType, reason, message string) (*kahuapi.Backup, error) {

	newBackup, err := ctrl.updateBackupStatus(backup, status)
	if err != nil {
		return newBackup, err
	}

	if newBackup.ResourceVersion != backup.ResourceVersion {
		ctrl.logger.Infof("Backup %s changed phase to %q: %s", backup.Name, status.Stage, message)
		ctrl.eventRecorder.Event(newBackup, eventType, reason, message)
	}
	return newBackup, err
}
