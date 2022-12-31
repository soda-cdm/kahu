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
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"github.com/soda-cdm/kahu/utils"
)

const (
	waitForSnapshotTimeout = 10 * time.Minute
)

func getVBCName() string {
	return fmt.Sprintf("vbc-%s", uuid.New().String())
}

func (ctrl *controller) processVolumeBackup(backup *kahuapi.Backup, resources Resources) (*kahuapi.Backup, error) {
	ctrl.logger.Infof("Processing Volume backup(%s)", backup.Name)

	pvcs, err := ctrl.getVolumes(backup, resources)
	if err != nil {
		ctrl.logger.Errorf("Volume backup validation failed. %s", err)

		if _, err := ctrl.updateBackupStatusWithEvent(backup, kahuapi.BackupStatus{
			State: kahuapi.BackupStateFailed,
		}, corev1.EventTypeWarning, EventVolumeBackupFailed,
			fmt.Sprintf("Volume backup validation failed. %s", err)); err != nil {
			return backup, errors.Wrap(err, "Volume backup validation failed")
		}
		return backup, err
	}

	if len(pvcs) == 0 {
		ctrl.logger.Infof("No volume for backup. " +
			"Setting stage to volume backup completed")
		// set annotation for
		return ctrl.annotateBackup(annVolumeBackupCompleted, backup)
	}

	volumeGroup, err := ctrl.volumeHandler.Group().ByPVCs(backup.Name, pvcs)
	if err != nil {
		ctrl.logger.Errorf("Failed to ensure volume group. %s", err)
		return backup, err
	}

	snapshotter, err := ctrl.volumeHandler.Snapshot().ByVolumeGroup(volumeGroup)
	if err != nil {
		return backup, err
	}

	// initiate snapshotting
	err = snapshotter.Apply()
	if err != nil {
		return backup, err
	}

	// wait for external snapshotter to finish snapshotting
	if err = snapshotter.WaitForSnapshotToReady(backup.Name, waitForSnapshotTimeout); err != nil {
		return backup, err
	}

	backupper, err := ctrl.volumeHandler.Backup().BySnapshot(snapshotter)
	if err != nil {
		return backup, err
	}

	err = backupper.Apply(backup.Name)
	if err != nil {
		return backup, err
	}

	return backup, nil
}

func (ctrl *controller) ensureVolumeBackupParameters(backup *kahuapi.Backup) (map[string]string, []string) {
	validationErrors := make([]string, 0)
	volBackupParam := make(map[string]string, 0)

	backupSpec := backup.Spec

	if len(backupSpec.VolumeBackupLocations) == 0 {
		validationErrors = append(validationErrors, "volume backup location can not be empty "+
			"for backups with volumes")
		return volBackupParam, validationErrors
	}

	for _, volumeBackupLocation := range backupSpec.VolumeBackupLocations {
		volBackupLoc, err := ctrl.backupLocationLister.Get(volumeBackupLocation)
		if apierrors.IsNotFound(err) {
			validationErrors = append(validationErrors,
				fmt.Sprintf("volume backup location(%s) not found", volumeBackupLocation))
			return volBackupParam, validationErrors
		}

		if err == nil && volBackupLoc != nil {
			_, err := ctrl.providerLister.Get(volBackupLoc.Spec.ProviderName)
			if apierrors.IsNotFound(err) {
				validationErrors = append(validationErrors,
					fmt.Sprintf("invalid provider name (%s) configured for volume backup location (%s)",
						volBackupLoc.Spec.ProviderName, volumeBackupLocation))
				return volBackupParam, validationErrors
			}
		}

		// TODO: Add Provider configurations
		return volBackupLoc.Spec.Config, nil
	}

	return volBackupParam, validationErrors
}

func (ctrl *controller) removeVolumeBackup(
	backup *kahuapi.Backup) error {

	vbcList, err := ctrl.volumeBackupClient.List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.Set{
			volumeContentBackupLabel: backup.Name,
		}.String(),
	})
	if err != nil {
		ctrl.logger.Errorf("Unable to get volume backup content list %s", err)
		return errors.Wrap(err, "unable to get volume backup content list")
	}

	for _, vbc := range vbcList.Items {
		if vbc.DeletionTimestamp != nil { // ignore deleting volume backup content
			continue
		}
		err := ctrl.volumeBackupClient.Delete(context.TODO(), vbc.Name, metav1.DeleteOptions{})
		if err != nil {
			ctrl.logger.Errorf("Failed to delete volume backup content %s", err)
			return errors.Wrap(err, "Unable to delete volume backup content")
		}
	}

	return nil
}

func (ctrl *controller) getVolumes(
	backup *kahuapi.Backup,
	resources Resources) ([]*corev1.PersistentVolumeClaim, error) {
	// retrieve all persistent volumes for backup
	ctrl.logger.Infof("Getting PersistentVolume for backup(%s)", backup.Name)

	unstructuredPVCs := resources.GetResourcesByKind(PVCKind)
	pvcs := make([]*corev1.PersistentVolumeClaim, 0)

	for _, unstructuredPVC := range unstructuredPVCs {
		pvc := new(corev1.PersistentVolumeClaim)
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredPVC.Object, pvc)
		if err != nil {
			ctrl.logger.Warningf("Failed to translate unstructured (%s) to "+
				"pvc. %s", unstructuredPVC.GetName(), err)
			return pvcs, err
		}

		pvcs = append(pvcs, pvc)

	}

	return pvcs, nil
}

func (ctrl *controller) ensureVolumeBackupContent(
	backupName string,
	volBackupParam map[string]string,
	pvProviderMap map[string][]corev1.PersistentVolume) error {
	// ensure volume backup content

	for provider, pvList := range pvProviderMap {
		// check if volume content already available
		// backup name and provider name is unique tuple for volume backup content
		vbcList, err := ctrl.volumeBackupClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.Set{
				volumeContentBackupLabel:    backupName,
				volumeContentVolumeProvider: provider,
			}.String(),
		})
		if err != nil {
			ctrl.logger.Errorf("Unable to get volume backup content list %s", err)
			return errors.Wrap(err, "Unable to get volume backup content list")
		}
		if len(vbcList.Items) > 0 {
			continue
		}

		pvObjectRefs := make([]corev1.PersistentVolume, 0)
		for _, pv := range pvList {
			ctrl.logger.Infof("Preparing volume backup content with pv name %s UID %s", pv.Name, pv.UID)
			pvObjectRefs = append(pvObjectRefs, corev1.PersistentVolume{
				TypeMeta: metav1.TypeMeta{
					Kind:       pv.Kind,
					APIVersion: pv.APIVersion,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: pv.Namespace,
					Name:      pv.Name,
					UID:       pv.UID,
				},
			})
		}

		time := metav1.Now()
		volumeBackupContent := &kahuapi.VolumeBackupContent{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					volumeContentBackupLabel:    backupName,
					volumeContentVolumeProvider: provider,
				},
				Name: getVBCName(),
			},
			Spec: kahuapi.VolumeBackupContentSpec{
				BackupName:     backupName,
				Volumes:        pvObjectRefs,
				VolumeProvider: &provider,
				Parameters:     volBackupParam,
			},
			Status: kahuapi.VolumeBackupContentStatus{
				Phase:          kahuapi.VolumeBackupContentPhaseInit,
				StartTimestamp: &time,
			},
		}

		_, err = ctrl.volumeBackupClient.Create(context.TODO(), volumeBackupContent, metav1.CreateOptions{})
		if err != nil {
			ctrl.logger.Errorf("unable to create volume backup content "+
				"for provider %s", provider)
			return errors.Wrap(err, "unable to create volume backup content")
		}
	}

	return nil
}

func (ctrl *controller) backupVolumeBackupContent(
	backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {
	ctrl.logger.Infoln("Starting collecting volume backup content")

	apiResource, _, err := ctrl.discoveryHelper.ByKind(utils.VBC)
	if err != nil {
		ctrl.logger.Errorf("Unable to get API resource info for Volume backup content: %s", err)
		return err
	}
	gv := schema.GroupVersion{
		Group:   apiResource.Group,
		Version: apiResource.Version,
	}.String()
	selectors := labels.Set(map[string]string{
		volumeContentBackupLabel: backup.Name,
	}).String()

	list, err := ctrl.volumeBackupClient.List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}
	for _, volumeBackupContent := range list.Items {
		// hack: API resource version and Kind are empty in response
		// populating with from API discovery module
		configData, err := json.Marshal(volumeBackupContent.Spec.Parameters)
		if err != nil {
			ctrl.logger.Errorf("Unable to get resource content: %s", err)
			return err
		}

		metav1.SetMetaDataAnnotation(&volumeBackupContent.ObjectMeta,
			utils.AnnBackupLocationParam, string(configData))
		volumeBackupContent.Kind = apiResource.Kind
		volumeBackupContent.APIVersion = gv
		resourceData, err := json.Marshal(volumeBackupContent)
		if err != nil {
			ctrl.logger.Errorf("Unable to get resource content: %s", err)
			return err
		}

		ctrl.logger.Infof("sending metadata for object %s/%s", volumeBackupContent.APIVersion,
			volumeBackupContent.Name)

		err = backupClient.Send(&metaservice.BackupRequest{
			Backup: &metaservice.BackupRequest_BackupResource{
				BackupResource: &metaservice.BackupResource{
					Resource: &metaservice.Resource{
						Name:    volumeBackupContent.Name,
						Group:   volumeBackupContent.APIVersion,
						Version: volumeBackupContent.APIVersion,
						Kind:    volumeBackupContent.Kind,
					},
					Data: resourceData,
				},
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (ctrl *controller) backupBackupLocation(
	backupLocationName string,
	backupClient metaservice.MetaService_BackupClient) error {
	ctrl.logger.Infoln("Starting collecting volume backup content")

	apiResource, _, err := ctrl.discoveryHelper.ByKind(utils.BackupLocation)
	if err != nil {
		ctrl.logger.Errorf("Unable to get API resource info for Volume backup content: %s", err)
		return err
	}
	gv := schema.GroupVersion{
		Group:   apiResource.Group,
		Version: apiResource.Version,
	}.String()

	location, err := ctrl.backupLocationLister.Get(backupLocationName)
	if err != nil {
		return err
	}

	location.Kind = apiResource.Kind
	location.APIVersion = gv
	resourceData, err := json.Marshal(location)
	if err != nil {
		ctrl.logger.Errorf("Unable to get resource content: %s", err)
		return err
	}

	ctrl.logger.Infof("sending metadata for object %s/%s", location.APIVersion,
		location.Name)

	err = backupClient.Send(&metaservice.BackupRequest{
		Backup: &metaservice.BackupRequest_BackupResource{
			BackupResource: &metaservice.BackupResource{
				Resource: &metaservice.Resource{
					Name:    location.Name,
					Group:   apiResource.Group,
					Version: apiResource.Version,
					Kind:    apiResource.Kind,
				},
				Data: resourceData,
			},
		},
	})

	return nil
}

func (ctrl *controller) annotateBackup(
	annotation string,
	backup *kahuapi.Backup) (*kahuapi.Backup, error) {
	backupName := backup.Name
	ctrl.logger.Infof("Annotating backup(%s) with %s", backupName, annotation)

	_, ok := backup.Annotations[annotation]
	if ok {
		ctrl.logger.Infof("Backup(%s) all-ready annotated with %s", backupName, annotation)
		return backup, nil
	}

	backupClone := backup.DeepCopy()
	metav1.SetMetaDataAnnotation(&backupClone.ObjectMeta, annotation, "true")

	origBytes, err := json.Marshal(backup)
	if err != nil {
		return backup, errors.Wrap(err, "error marshalling backup")
	}

	updatedBytes, err := json.Marshal(backupClone)
	if err != nil {
		return backup, errors.Wrap(err, "error marshalling updated backup")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return backup, errors.Wrap(err, "error creating json merge patch for backup")
	}

	backup, err = ctrl.backupClient.Patch(context.TODO(), backupName,
		types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		ctrl.logger.Error("Unable to update backup(%s) for volume completeness. %s",
			backupName, err)
		errors.Wrap(err, "error annotating volume backup completeness")
	}

	return backup, nil
}
