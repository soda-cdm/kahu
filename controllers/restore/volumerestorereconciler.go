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
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahuclient "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
)

// Reconciler runs a periodic loop to reconcile the current state of volume back and update
// backup annotation
type Reconciler interface {
	Run(stopCh <-chan struct{})
}

// newReconciler returns a new instance of Reconciler that waits loopPeriod
// between successive executions.
func newReconciler(
	loopPeriod time.Duration,
	logger log.FieldLogger,
	volumeRestoreClient kahuclient.VolumeRestoreContentInterface,
	restoreClient kahuclient.RestoreInterface,
	restoreLister kahulister.RestoreLister) Reconciler {
	return &reconciler{
		loopPeriod:          loopPeriod,
		logger:              logger,
		volumeRestoreClient: volumeRestoreClient,
		restoreLister:       restoreLister,
		restoreClient:       restoreClient,
	}
}

type reconciler struct {
	loopPeriod          time.Duration
	logger              log.FieldLogger
	volumeRestoreClient kahuclient.VolumeRestoreContentInterface
	restoreClient       kahuclient.RestoreInterface
	restoreLister       kahulister.RestoreLister
}

func (rc *reconciler) Run(stopCh <-chan struct{}) {
	wait.Until(rc.reconciliationLoopFunc(), rc.loopPeriod, stopCh)
}

func (rc *reconciler) reconciliationLoopFunc() func() {
	return func() {
		rc.reconcile()
	}
}

// reconcile update restore with volume restore updates
func (rc *reconciler) reconcile() {
	restores, err := rc.restoreLister.List(labels.Everything())
	if err != nil {
		rc.logger.Errorf("Unable to list restore list to reconcile")
	}

	for _, restore := range restores {
		restoreName := restore.Name
		if restore.DeletionTimestamp != nil {
			deleted, err := rc.isVolumeRestoreContentDeleted(restore)
			if err != nil {
				rc.logger.Errorf("Failed to check deleted volume restore status. %s", err)
				continue
			}

			if deleted {
				err := rc.annotateRestore(annVolumeRestoreDeleteCompleted, restoreName)
				if err != nil {
					rc.logger.Errorf("Unable to add annotation(%s) for restore(%s)",
						annVolumeRestoreDeleteCompleted,
						restoreName)
				}
				continue
			}
		}

		if !rc.isReconcileRequired(restore) {
			rc.logger.Debugf("Skipping reconcile for restore %s", restoreName)
			continue
		}

		// annotate with volume completeness if no volume for restore
		vbContents, err := rc.volumeRestoreClient.List(context.TODO(), metav1.ListOptions{LabelSelector: labels.Set{
			volumeContentRestoreLabel: restoreName,
		}.AsSelector().String()})
		if err != nil {
			rc.logger.Errorf("Unable to list volume restore content for restore(%s). %s",
				restoreName, err)
			continue
		}

		// may be lister not populated with volume restore contents
		if len(vbContents.Items) == 0 {
			continue
		}

		volumesRestoreDone := true
		volumeRestoreFailed := false
		for _, vbc := range vbContents.Items {
			if vbc.Status.Phase == kahuapi.VolumeRestoreContentPhaseFailed {
				volumeRestoreFailed = true
				break
			}
			if vbc.Status.Phase != kahuapi.VolumeRestoreContentPhaseCompleted {
				volumesRestoreDone = false
				break
			}
		}

		if volumeRestoreFailed {
			// update restore status failure
			_, err = rc.updateRestoreFailure(restore)
			if err != nil {
				rc.logger.Errorf("Unable to update restore(%s) failure", restore.Name)
			}
			continue
		}

		if !volumesRestoreDone {
			continue
		}

		err = rc.annotateRestore(annVolumeRestoreCompleted, restoreName)
		if err != nil {
			rc.logger.Errorf("Unable to annotate restore(%s) with volume restore completeness", restoreName)
		}
	}
}

func (rc *reconciler) isVolumeRestoreContentDeleted(restore *kahuapi.Restore) (bool, error) {
	restoreName := restore.Name
	// annotate with volume restore deletion if no volume for restore
	vbContents, err := rc.volumeRestoreClient.List(context.TODO(), metav1.ListOptions{LabelSelector: labels.Set{
		volumeContentRestoreLabel: restoreName,
	}.AsSelector().String()})
	if err == nil && len(vbContents.Items) > 0 {
		rc.logger.Debug("VolumeRestoreContents still available in restore %s", restoreName)
		return false, nil
	}

	if apierrors.IsNotFound(err) ||
		len(vbContents.Items) == 0 {
		return true, nil
	}

	return false, err
}

func (rc *reconciler) isReconcileRequired(restore *kahuapi.Restore) bool {
	// skip reconcile if restore getting deleted
	// skip reconcile if restore.Status.Phase is not volume restore
	// skip reconcile if volume restore completed
	if restore.Status.Stage != kahuapi.RestoreStageVolumes ||
		metav1.HasAnnotation(restore.ObjectMeta, annVolumeRestoreCompleted) {
		return false
	}
	return true
}

func (rc *reconciler) annotateRestore(
	annotation string,
	restoreName string) error {
	rc.logger.Infof("Annotating restore(%s) with %s", restoreName, annotation)

	restore, err := rc.restoreLister.Get(restoreName)
	if err != nil {
		rc.logger.Errorf("Unable to get restore(%s) for %s. %s",
			restoreName, annotation, err)
		return errors.Wrap(err, "Unable to update restore")
	}

	_, ok := restore.Annotations[annotation]
	if ok {
		rc.logger.Debugf("Restore(%s) all-ready annotated with %s", restoreName, annotation)
		return nil
	}

	restoreClone := restore.DeepCopy()
	metav1.SetMetaDataAnnotation(&restoreClone.ObjectMeta, annotation, "true")

	origBytes, err := json.Marshal(restore)
	if err != nil {
		return errors.Wrap(err, "error marshalling restore")
	}

	updatedBytes, err := json.Marshal(restoreClone)
	if err != nil {
		return errors.Wrap(err, "error marshalling updated restore")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return errors.Wrap(err, "error creating json merge patch for restore")
	}

	_, err = rc.restoreClient.Patch(context.TODO(), restoreName,
		types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		rc.logger.Error("Unable to update restore(%s) for volume completeness. %s",
			restoreName, err)
		errors.Wrap(err, "error annotating volume restore completeness")
	}

	return nil
}

func (rc *reconciler) updateRestoreFailure(
	restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	var err error

	restoreClone := restore.DeepCopy()
	restoreClone.Status.State = kahuapi.RestoreStateFailed
	restoreClone, err = rc.restoreClient.UpdateStatus(context.TODO(), restoreClone, metav1.UpdateOptions{})
	if err != nil {
		rc.logger.Errorf("updating restore(%s) status: update status failed %s", restore.Name, err)
	}

	return restoreClone, err
}
