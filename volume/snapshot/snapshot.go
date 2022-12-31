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

package snapshot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"github.com/soda-cdm/kahu/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/volume/volumegroup"
)

const (
	LabelBackupName      = "kahu.backup.name"
	LabelProvisionerName = "kahu.provisioner.name"
)

type Interface interface {
	Apply() error
	WaitForSnapshotToReady(refName string, timeout time.Duration) error
	Delete() (*kahuapi.VolumeSnapshot, error)
	GetSnapshots() ([]*kahuapi.VolumeSnapshot, error)
}

type snapshoter struct {
	ctx         context.Context
	kubeClient  kubernetes.Interface
	kahuClient  clientset.Interface
	volumeGroup volumegroup.Interface
	snapshotIDs sets.String
	logger      log.FieldLogger
}

func newSnapshot(ctx context.Context,
	kubeClient kubernetes.Interface,
	kahuClient clientset.Interface,
	volumeGroup volumegroup.Interface) (Interface, error) {
	return &snapshoter{
		ctx:         ctx,
		kubeClient:  kubeClient,
		kahuClient:  kahuClient,
		volumeGroup: volumeGroup,
		snapshotIDs: sets.NewString(),
		logger:      log.WithField("module", "snapshotter"),
	}, nil
}

func (s *snapshoter) Apply() error {
	volumes, readyToUse := s.volumeGroup.GetVolumes()
	if !readyToUse {
		return fmt.Errorf("volume group not ready to use %s", s.volumeGroup.GetGroupName())
	}

	// group volumes with provisioner
	byProvisioner := make(map[string][]kahuapi.ResourceReference)
	for _, volume := range volumes {
		pv, err := s.getPV(volume)
		if err != nil {
			return err
		}

		provisioner := utils.VolumeProvider(pv)
		if provisioner == "" {
			return fmt.Errorf("not able to identify provisioner for PV %s", pv.Name)
		}
		pvcs, ok := byProvisioner[provisioner]
		if !ok {
			byProvisioner[provisioner] = make([]kahuapi.ResourceReference, 0)
		}
		byProvisioner[provisioner] = append(pvcs, volume)
	}

	for provider, resources := range byProvisioner {
		snapshot, err := s.kahuSnapshot(s.volumeGroup.GetBackupName(), provider, resources)
		if err != nil {
			// delete older kahu snapshot objects
			return err
		}
		s.snapshotIDs.Insert(snapshot.Name)
	}

	return nil
}

func (s *snapshoter) getPV(volume kahuapi.ResourceReference) (*corev1.PersistentVolume, error) {
	switch volume.Kind {
	case utils.PVC:
		return s.snapshotClassByPVC(volume.Name, volume.Namespace)
	case utils.PV:
		return s.snapshotClassByPV(volume.Name)
	default:
		return nil, fmt.Errorf("invalid volume kind (%s)", volume.Kind)
	}
}

func (s *snapshoter) snapshotClassByPVC(name,
	namespace string) (*corev1.PersistentVolume, error) {
	pvc, err := s.kubeClient.CoreV1().
		PersistentVolumeClaims(namespace).
		Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if pvc.Spec.VolumeName == "" {
		return nil, fmt.Errorf("pvc(%s/%s) is not bound", namespace, name)
	}

	pv, err := s.kubeClient.CoreV1().
		PersistentVolumes().
		Get(context.TODO(), pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return pv, nil
}

func (s *snapshoter) snapshotClassByPV(name string) (*corev1.PersistentVolume, error) {
	pv, err := s.kubeClient.CoreV1().PersistentVolumes().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return pv, nil
}

func (s *snapshoter) kahuSnapshot(backup string,
	provisioner string, volumes []kahuapi.ResourceReference) (*kahuapi.VolumeSnapshot, error) {
	kahuSnapshot := s.volGroupToSnapshot(backup, provisioner, volumes)

	return s.kahuClient.KahuV1beta1().VolumeSnapshots().Create(context.TODO(), kahuSnapshot, metav1.CreateOptions{})
}

func (s *snapshoter) volGroupToSnapshot(backup string,
	provisioner string,
	vols []kahuapi.ResourceReference) *kahuapi.VolumeSnapshot {
	return &kahuapi.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: "snapshot-" + uuid.New().String(),
			Labels: map[string]string{
				LabelBackupName:      backup,
				LabelProvisionerName: provisioner,
			},
		},
		Spec: kahuapi.VolumeSnapshotSpec{
			BackupName:       &backup,
			SnapshotProvider: &provisioner,
			VolumeSource: kahuapi.VolumeSource{
				List: vols,
			},
		},
	}
}

func (s *snapshoter) Delete() (*kahuapi.VolumeSnapshot, error) {
	return nil, nil
}

func (s *snapshoter) WaitForSnapshotToReady(refName string, timeout time.Duration) error {
	for _, snapshotId := range s.snapshotIDs.List() {
		if err := s.waitForSnapshotToReady(refName, snapshotId, timeout); err != nil {
			return err
		}
	}
	return nil
}

func (s *snapshoter) waitForSnapshotToReady(refName string,
	snapshotID string, timeout time.Duration) error {
	s.logger.Infof("Probing Snapshot Status [id=%v]", snapshotID)

	verifyStatus := func() (bool, error) {
		snapshot, err := s.kahuClient.KahuV1beta1().VolumeSnapshots().Get(context.TODO(),
			snapshotID, metav1.GetOptions{})
		if err != nil {
			// Ignore "not found" errors in case the Snapshot was just created and hasn't yet made it into the lister.
			if !apierrors.IsNotFound(err) {
				s.logger.Errorf("unexpected error waiting for snapshot, %v", err)
				return false, err
			}

			// The Snapshot is not available yet and we will have to try again.
			return false, nil
		}

		successful, err := s.verifySnapshotStatus(snapshot)
		if err != nil {
			return false, err
		}
		return successful, nil
	}

	return s.waitForSnapshotStatus(refName, snapshotID, timeout, verifyStatus, "Create")
}

func (s *snapshoter) verifySnapshotStatus(snapshot *kahuapi.VolumeSnapshot) (bool, error) {
	// if being deleted, fail fast
	if snapshot.GetDeletionTimestamp() != nil {
		s.logger.Errorf("Snapshot [%s] has deletion timestamp, will not continue to wait for volume snapshot",
			snapshot.UID)
		return false, errors.New("snapshot is being deleted")
	}

	// attachment OK
	if snapshotReadyToUse(snapshot) {
		return true, nil
	}

	// driver reports attach error
	failureMsg := snapshot.Status.FailureReason
	if len(failureMsg) != 0 {
		s.logger.Errorf("Snapshot for %v failed: %v", snapshot.UID, failureMsg)
		return false, errors.New(failureMsg)
	}
	return false, nil
}

func snapshotReadyToUse(snapshot *kahuapi.VolumeSnapshot) bool {
	return snapshot.Status.ReadyToUse != nil && *snapshot.Status.ReadyToUse == true
}

func (s *snapshoter) waitForSnapshotStatus(backupName, snapshotID string,
	timeout time.Duration,
	verifyStatus func() (bool, error),
	operation string) error {
	var (
		initBackoff = 500 * time.Millisecond
		// This is approximately the duration between consecutive ticks after two minutes (CSI timeout).
		maxBackoff    = 7 * time.Second
		resetDuration = time.Minute
		backoffFactor = 1.05
		jitter        = 0.1
		clock         = &clock.RealClock{}
	)
	backoffMgr := wait.NewExponentialBackoffManager(initBackoff,
		maxBackoff,
		resetDuration,
		backoffFactor,
		jitter,
		clock)

	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()

	for {
		t := backoffMgr.Backoff()
		select {
		case <-t.C():
			successful, err := verifyStatus()
			if err != nil {
				return err
			}
			if successful {
				return nil
			}
		case <-ctx.Done():
			t.Stop()
			s.logger.Errorf("%s timeout for snapshot after %v [backup=%s snapshotID=%s]",
				operation, timeout, backupName, snapshotID)
			return fmt.Errorf("timed out waiting for external-snapshotting of %v backup ", backupName)
		}
	}
}

func (s *snapshoter) GetSnapshots() ([]*kahuapi.VolumeSnapshot, error) {
	kahuVolSnapshots := make([]*kahuapi.VolumeSnapshot, 0)
	s.logger.Infof("Getting snapshot info for %s", s.snapshotIDs)
	for _, snapshotID := range s.snapshotIDs.List() {
		kahuVolSnapshot, err := s.kahuClient.KahuV1beta1().
			VolumeSnapshots().
			Get(context.TODO(), snapshotID, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		kahuVolSnapshots = append(kahuVolSnapshots, kahuVolSnapshot)
	}

	return kahuVolSnapshots, nil
}
