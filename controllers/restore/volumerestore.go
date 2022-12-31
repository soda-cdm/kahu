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
	"reflect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
)

const (
	volumeContentRestoreLabel = "kahu.io/restore-name"
)

func getVRCName() string {
	return fmt.Sprintf("vrc-%s", uuid.New().String())
}

func (ctx *restoreContext) volumeRestoreFinished(restore *kahuapi.Restore) bool {
	return restore.Status.State == kahuapi.RestoreStateCompleted
}

func (ctx *restoreContext) annotateRestore(annotation, value string,
	restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	if metav1.HasAnnotation(restore.ObjectMeta, annotation) {
		ctx.logger.Infof("Restore all-ready annotated with %s", annotation)
		return restore, nil
	}

	restoreClone := restore.DeepCopy()
	metav1.SetMetaDataAnnotation(&restoreClone.ObjectMeta, annotation, value)
	return ctx.patchRestore(restore, restoreClone)
}

func (ctx *restoreContext) annotateVolRestoreContent(annotation string,
	vrcData *kahuapi.VolumeRestoreContent) (*kahuapi.VolumeRestoreContent, error) {
	if metav1.HasAnnotation(vrcData.ObjectMeta, annotation) {
		ctx.logger.Infof("Volume restore content all-ready annotated with %s", annotation)
		return vrcData, nil
	}

	vrcClone := vrcData.DeepCopy()
	metav1.SetMetaDataAnnotation(&vrcClone.ObjectMeta, annotation, "true")
	return ctx.patchVolRestoreContent(vrcData, vrcClone)
}

func (ctx *restoreContext) checkPVCRestored(restore *kahuapi.Restore,
	indexer cache.Indexer) (bool, error) {
	// all contents are prefetched based on restore backup spec
	pvcs, err := ctx.parseRestorePVC(indexer)
	if err != nil {
		ctx.logger.Errorf("Unable to parse PVC. %s", err)
		// retry to parse PVC
		return false, err
	}

	// check all pvc restored
	for _, pvc := range pvcs {
		k8sPVC, err := ctx.kubeClient.CoreV1().
			PersistentVolumeClaims(pvc.Namespace).
			Get(context.TODO(), pvc.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("Restore PVC (%s) not available", pvc.Name)
			restore.Status.State = kahuapi.RestoreStateFailed
			restore.Status.FailureReason = fmt.Sprintf("Restore PVC (%s) not available", pvc.Name)
			_, err = ctx.updateRestoreStatus(restore)
			return false, err
		}
		if err != nil {
			ctx.logger.Errorf("Failed to get PVC status. %s", err)
			// retry to check PVC status from K8S
			return false, err
		}
		if k8sPVC.Spec.VolumeName == "" {
			ctx.logger.Errorf("Restored PVC (%s) is not bound. Considering PVC restore failed", pvc.Name)
			restore.Status.State = kahuapi.RestoreStateFailed
			restore.Status.FailureReason = fmt.Sprintf("Restored PVC (%s) is not bound. "+
				"Considering PVC restore failed", pvc.Name)
			_, err = ctx.updateRestoreStatus(restore)
			return false, err
		}
	}

	return true, nil
}

func (ctx *restoreContext) syncVolumeRestore(restore *kahuapi.Restore,
	indexer cache.Indexer) (*kahuapi.Restore, error) {
	if ctx.volumeRestoreFinished(restore) {
		return restore, nil
	}
	if metav1.HasAnnotation(restore.ObjectMeta, annVolumeRestoreCompleted) {
		// ensure all PVC are restored
		restored, err := ctx.checkPVCRestored(restore, ctx.resolveObjects)
		if err != nil {
			ctx.logger.Errorf("Failed to check Volume restore(%s). Retrying volume restore check", err)
			return restore, err
		}
		if !restored {
			return restore, nil
		}
		// add volume backup content in resource backup list
		restore.Status.State = kahuapi.RestoreStateCompleted
		return ctx.updateRestoreStatus(restore)
	}
	// all contents are prefetched based on restore backup spec
	// check if volume backup required
	pvcs, err := ctx.parseRestorePVC(indexer)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Unable to parse PVC")
		return ctx.updateRestoreStatus(restore)
	}
	if len(pvcs) == 0 { // if not PVC annotate and move to completion
		ctx.logger.Info("PVCs are not available for restore. " +
			"Continue with metadata restore")
		restore, err = ctx.annotateRestore(annVolumeRestoreCompleted, "true", restore)
		if err != nil {
			ctx.logger.Error("Unable to annotate for volume backup completion")
			return restore, err
		}
		restore.Status.State = kahuapi.RestoreStateCompleted
		return ctx.updateRestoreStatus(restore)
	}

	if restore.Status.State == kahuapi.RestoreStateProcessing {
		// check if volume backup completed. if not return
		return ctx.checkVolumeRestoreStatus(len(pvcs), restore)
	}

	vrcs, err := ctx.constructVolumeRestoreContent(restore.Name, indexer, pvcs)
	if err != nil {
		return restore, errors.Wrap(err, "unable to construct volume restore content list")
	}

	return ctx.volumeRestore(restore, pvcs, vrcs, indexer)
}

func (ctx *restoreContext) volumeRestoreCompleted(restoreState kahuapi.VolumeRestoreState) bool {
	return restoreState.Progress == 100
}

func (ctx *restoreContext) checkVolumeRestoreStatus(pvcCount int,
	restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	// check if restore has already scheduled for volume restore
	// if already scheduled for restore, continue to wait for completion
	restoreVolContents, err := ctx.restoreVolumeClient.List(context.TODO(),
		metav1.ListOptions{
			LabelSelector: labels.Set(map[string]string{
				volumeContentRestoreLabel: restore.Name,
			}).String(),
		})
	if err != nil || restoreVolContents == nil || len(restoreVolContents.Items) == 0 {
		ctx.logger.Errorf("Unable to get volume restore content. %s", err)
		return restore, err
	}

	// ensure all PVC are restored and Restore count are same as requested PVC
	restoredPVC := 0
	for _, restoreVolContent := range restoreVolContents.Items {
		for _, restoreState := range restoreVolContent.Status.RestoreState {
			if ctx.volumeRestoreCompleted(restoreState) {
				restoredPVC++
			}
		}
	}
	if restoredPVC != pvcCount {
		ctx.logger.Infof("Volume are still getting restored")
		return restore, nil
	}

	restore, err = ctx.annotateRestore(annVolumeRestoreCompleted, "true", restore)
	if err != nil {
		ctx.logger.Error("Unable to annotate for volume backup completion")
		return restore, err
	}
	restore.Status.State = kahuapi.RestoreStateCompleted
	return ctx.updateRestoreStatus(restore)
}

func (ctx *restoreContext) parseRestorePVC(indexer cache.Indexer) ([]*v1.PersistentVolumeClaim, error) {
	pvcs := make([]*v1.PersistentVolumeClaim, 0)

	if indexer == nil {
		ctx.logger.Error("Invalid cached resource type")
		return pvcs, nil
	}
	pvcObjects, err := indexer.ByIndex(backupObjectResourceIndex, utils.PVC)
	if err != nil {
		return pvcs, nil
	}

	for _, pvcObject := range pvcObjects {
		var unstructuredPVC *unstructured.Unstructured
		switch unstructuredResource := pvcObject.(type) {
		case *unstructured.Unstructured:
			unstructuredPVC = unstructuredResource
		case unstructured.Unstructured:
			unstructuredPVC = unstructuredResource.DeepCopy()
		default:
			ctx.logger.Warningf("Unknown cached resource type. %s", reflect.TypeOf(pvcObject))
			continue
		}

		pvc := new(v1.PersistentVolumeClaim)
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredPVC.Object, &pvc)
		if err != nil {
			ctx.logger.Errorf("Failed to translate unstructured (%s) to "+
				"pv. %s", unstructuredPVC.GetName(), err)
			return pvcs, errors.Wrap(err, "Failed to covert unstructured resource to PVC for resolution")
		}

		pvcs = append(pvcs, pvc)
	}

	return pvcs, nil
}

func (ctx *restoreContext) parseVBC(indexer cache.Indexer) ([]*kahuapi.VolumeBackupContent, error) {
	vbcs := make([]*kahuapi.VolumeBackupContent, 0)

	vbcObjects, err := indexer.ByIndex(backupObjectResourceIndex, utils.VBC)
	if err != nil {
		ctx.logger.Errorf("Backup location not available in backup cache. %s", err)
		return vbcs, err
	}

	for _, vbcObject := range vbcObjects {
		var unstructuredVBC *unstructured.Unstructured
		switch unstructuredResource := vbcObject.(type) {
		case *unstructured.Unstructured:
			unstructuredVBC = unstructuredResource
		case unstructured.Unstructured:
			unstructuredVBC = unstructuredResource.DeepCopy()
		default:
			ctx.logger.Warningf("Unknown cached resource type. %s", reflect.TypeOf(vbcObject))
			continue
		}
		vbc := new(kahuapi.VolumeBackupContent)
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredVBC.Object, &vbc)
		if err != nil {
			ctx.logger.Errorf("Failed to translate unstructured (%s) to "+
				"pv. %s", unstructuredVBC.GetName(), err)
			return vbcs, errors.Wrap(err, "Failed to covert unstructured resource to PVC for resolution")
		}

		ctx.logger.Infof("Volume backup content %+v", vbc)

		vbcs = append(vbcs, vbc)
	}

	return vbcs, nil
}

func (ctx *restoreContext) constructVolumeRestoreContent(
	restoreName string,
	indexer cache.Indexer,
	pvcs []*v1.PersistentVolumeClaim) ([]*kahuapi.VolumeRestoreContent, error) {
	vrcs := make([]*kahuapi.VolumeRestoreContent, 0)

	pvcMap := make(map[string]*v1.PersistentVolumeClaim)
	for _, pvc := range pvcs {
		if pvc.Spec.VolumeName == "" {
			continue
		}
		pvcMap[pvc.Spec.VolumeName] = pvc
	}

	ctx.logger.Infof("Collected PVC map %+v", pvcMap)
	vbcs, err := ctx.parseVBC(indexer)
	if err != nil {
		return vrcs, errors.Wrap(err, "Unable to parse Volume backup content")
	}

	for _, vbc := range vbcs {
		time := metav1.Now()
		volumes := make([]kahuapi.RestoreVolumeSpec, 0)
		for _, backState := range vbc.Status.BackupState {
			if pvc, ok := pvcMap[backState.VolumeName]; ok {
				// unset volume name so that new restored volume gets created
				pvc.Spec.VolumeName = ""
				volumes = append(volumes, kahuapi.RestoreVolumeSpec{
					BackupHandle:     backState.BackupHandle,
					BackupAttributes: backState.BackupAttributes,
					Claim:            pvc,
				})
			}
		}

		if len(volumes) == 0 {
			continue
		}

		param := make(map[string]string)
		blParam, ok := vbc.Annotations[utils.AnnBackupLocationParam]
		if ok {
			json.Unmarshal([]byte(blParam), &param)
		}

		volumeRestoreContent := &kahuapi.VolumeRestoreContent{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					volumeContentRestoreLabel: restoreName,
				},
				Name: getVRCName(),
			},
			Spec: kahuapi.VolumeRestoreContentSpec{
				BackupName:     vbc.Spec.BackupName,
				Volumes:        volumes,
				VolumeProvider: vbc.Spec.VolumeProvider,
				Parameters:     param,
				RestoreName:    restoreName,
			},
			Status: kahuapi.VolumeRestoreContentStatus{
				Phase:          kahuapi.VolumeRestoreContentPhaseInit,
				StartTimestamp: &time,
			},
		}

		vrcs = append(vrcs, volumeRestoreContent)
	}

	return vrcs, nil
}

func (ctx *restoreContext) volumeRestore(
	restore *kahuapi.Restore,
	pvcs []*v1.PersistentVolumeClaim,
	vrcs []*kahuapi.VolumeRestoreContent,
	indexer cache.Indexer) (*kahuapi.Restore, error) {
	err := ctx.ensurePVCNamespace(pvcs)
	if err != nil {
		return restore, err
	}

	err = ctx.ensureStorageClass(indexer, pvcs)
	if err != nil {
		return restore, err
	}

	err = ctx.ensureVolumeRestoreContent(vrcs)
	if err != nil {
		return restore, err
	}

	restore.Status.State = kahuapi.RestoreStateProcessing
	return ctx.updateRestoreStatus(restore)
}

func (ctx *restoreContext) removeVolumeRestore(
	restore *kahuapi.Restore) error {

	vrcList, err := ctx.restoreVolumeClient.List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.Set{
			volumeContentRestoreLabel: restore.Name,
		}.String(),
	})
	if err != nil {
		ctx.logger.Errorf("Unable to get volume restore content list %s", err)
		return errors.Wrap(err, "unable to get volume restore content list")
	}

	// If restore is not fully successful, trigger resource cleanup
	if !(restore.Status.State == kahuapi.RestoreStateCompleted &&
		restore.Status.Stage == kahuapi.RestoreStageFinished) {
		ctx.logger.Infof("Preparing to cleanup vrc for the restore %s", restore.Name)
		err := ctx.prepareVRCCleanup(vrcList)
		if err != nil {
			ctx.logger.Errorf("Unable to mark vrc cleanup for restore %s.", restore.Name)
		}
	}

	for _, vrc := range vrcList.Items {
		if vrc.DeletionTimestamp != nil { // ignore deleting volume backup content
			continue
		}
		err := ctx.restoreVolumeClient.Delete(context.TODO(), vrc.Name, metav1.DeleteOptions{})
		if err != nil {
			ctx.logger.Errorf("Failed to delete volume backup content %s", err)
			return errors.Wrap(err, "Unable to delete volume backup content")
		}
	}

	return nil
}

// prepareVRCCleanup will add annotations to vrc to trigger the resource cleanup
func (ctx *restoreContext) prepareVRCCleanup(vrcList *kahuapi.VolumeRestoreContentList) error {
	for _, vrc := range vrcList.Items {
		if vrc.DeletionTimestamp != nil { // ignore deleting volume restore content
			continue
		}
		_, err := ctx.annotateVolRestoreContent(annVolumeResourceCleanup, &vrc)
		if err != nil {
			ctx.logger.Errorf("Failed to annotate volume restore content %s", err)
			return errors.Wrap(err, "Unable to annotate volume restore content")
		}
	}
	return nil
}

func (ctx *restoreContext) cleanupRestoredMetadata(restore *kahuapi.Restore) (*kahuapi.Restore, error) {

	resCleanupFailed := false
	Resources := restore.Status.Resources

	for _, resource := range Resources {
		err := ctx.deleteRestoredResource(&resource, restore)
		if err != nil {
			ctx.logger.Errorf("failed to cleanup the resource %s", resource.ResourceName)
			resCleanupFailed = true
		}
	}

	if resCleanupFailed == false {
		newRestore := restore.DeepCopy()
		utils.SetAnnotation(newRestore, annRestoreCleanupDone, "true")
		ctx.logger.Infof("Cleanup completed for restore %s", restore.Name)
		return ctx.patchRestore(restore, newRestore)
	}
	return restore, nil
}

func (ctx *restoreContext) ensureVolumeRestoreContent(
	vrcs []*kahuapi.VolumeRestoreContent) error {
	for _, vrc := range vrcs {
		_, err := ctx.restoreVolumeClient.Create(context.TODO(), vrc, metav1.CreateOptions{})
		if err != nil {
			ctx.logger.Errorf("unable to create volume restore content "+
				"for provider %s", vrc.Name)
			return err
		}
	}

	return nil
}

func (ctx *restoreContext) ensureStorageClass(
	indexer cache.Indexer,
	pvcs []*v1.PersistentVolumeClaim) error {
	// all contents are prefetched based on restore backup spec
	ctx.logger.Infof("restoring storage class in %s phase", kahuapi.RestoreStageVolumes)

	scObjects, err := indexer.ByIndex(backupObjectResourceIndex, utils.SC)
	if err != nil {
		return errors.Wrap(err, "storage class not available in restore cache")
	}
	storageClasses := make([]*unstructured.Unstructured, 0)
	for _, scObject := range scObjects {
		var unstructuredSC *unstructured.Unstructured
		switch unstructuredResource := scObject.(type) {
		case *unstructured.Unstructured:
			unstructuredSC = unstructuredResource
		case unstructured.Unstructured:
			unstructuredSC = unstructuredResource.DeepCopy()
		default:
			ctx.logger.Warningf("Unknown cached resource type. %s", reflect.TypeOf(scObject))
			continue
		}
		storageClasses = append(storageClasses, unstructuredSC)
		sc := new(storagev1.StorageClass)
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredSC.Object, &sc)
		if err != nil {
			ctx.logger.Errorf("Failed to translate unstructured (%s) to "+
				"sc. %s", unstructuredSC.GetName(), err)
			return errors.Wrap(err, "Failed to covert unstructured resource to PVC for resolution")
		}

		storageClasses = append(storageClasses, unstructuredSC)
	}

	for _, pvc := range pvcs {
		found := false
		for _, storageclass := range storageClasses {
			if storageclass.GetName() == *pvc.Spec.StorageClassName {
				found = true
				err := ctx.applyResource(storageclass, nil)
				if err != nil {
					ctx.logger.Errorf("Unable to create storage class resource, %s", err)
					return errors.Wrap(err, "Unable to create storage class resource")
				}
			}
		}
		if !found {
			return fmt.Errorf("restore cache donot have storage class (%s).Check backup data",
				*pvc.Spec.StorageClassName)
		}
	}

	return nil
}

func (ctx *restoreContext) ensurePVCNamespace(
	pvcs []*v1.PersistentVolumeClaim) error {
	namespaces := make(map[string]struct{})
	for _, pvc := range pvcs {
		namespaces[pvc.Namespace] = struct{}{}
	}

	for namespace, _ := range namespaces {
		err := ctx.ensureNamespace(namespace)
		if err != nil {
			return err
		}
	}

	return nil
}
