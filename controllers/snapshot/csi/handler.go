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

package csi

import (
	"context"
	"fmt"
	"time"

	snapshotapiv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	snapshotclientset "github.com/kubernetes-csi/external-snapshotter/client/v4/clientset/versioned"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/clientset/versioned/typed/volumesnapshot/v1"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/controllers/snapshot/classsyncer"
)

const (
	csiSnapshotTimeout        = 5 * time.Minute
	defaultSnapshotNamePrefix = "snapshot"
)

type Snapshoter interface {
	Handle(snapshot *kahuapi.VolumeSnapshot) error
}

type snapshoter struct {
	ctx                    context.Context
	logger                 log.FieldLogger
	kubeClient             kubernetes.Interface
	kahuClient             clientset.Interface
	snapshotClient         snapshotv1.SnapshotV1Interface
	volSnapshotClassSyncer classsyncer.Interface
}

func NewSnapshotter(ctx context.Context,
	restConfig *rest.Config,
	kubeClient kubernetes.Interface,
	kahuClient clientset.Interface,
	volSnapshotClassSyncer classsyncer.Interface) (Snapshoter, error) {
	client, err := snapshotclientset.NewForConfig(restConfig)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return &snapshoter{
		ctx:                    ctx,
		kubeClient:             kubeClient,
		kahuClient:             kahuClient,
		volSnapshotClassSyncer: volSnapshotClassSyncer,
		snapshotClient:         client.SnapshotV1(),
		logger:                 log.WithField("module", "csi-snapshotter"),
	}, nil
}

func (s *snapshoter) Handle(snapshot *kahuapi.VolumeSnapshot) error {
	// create snapshot for each snapshot volumes
	csiSnapshotClass, err := s.volSnapshotClassSyncer.SnapshotClassByProvider(*snapshot.Spec.SnapshotProvider)
	if err != nil {
		return err
	}

	for _, snapshotState := range snapshot.Status.SnapshotStates {
		// ignore if already CSI object created
		if snapshotState.CSISnapshotRef != nil {
			continue
		}
		// create CSI object
		if err := s.applySnapshot(snapshot.Name, csiSnapshotClass.Name, snapshotState.PVC); err != nil {
			s.logger.Errorf("Error applying volume snapshot %s", err)
			return err
		}
	}

	err = s.waitForSnapshotToReady(snapshot.Name, csiSnapshotTimeout)
	readyToUse := true
	if err != nil {
		readyToUse = false
	}

	kahuVolSnapshot, err := s.kahuClient.KahuV1beta1().
		VolumeSnapshots().
		Get(context.TODO(), snapshot.Name, metav1.GetOptions{})
	if err != nil {
		s.logger.Errorf("Failed to update Volume Snapshot(%s) status, %s", snapshot.Name, err)
		return err
	}

	kahuVolSnapshot.Status.ReadyToUse = &readyToUse
	_, err = s.kahuClient.KahuV1beta1().
		VolumeSnapshots().
		UpdateStatus(context.TODO(), kahuVolSnapshot, metav1.UpdateOptions{})
	if err != nil {
		s.logger.Errorf("Failed to update Volume Snapshot(%s) status, %s", snapshot.Name, err)
		return err
	}

	return err
}

func (s *snapshoter) applySnapshot(kahuVolSnapshotName string,
	snapshotClassName string,
	pvc kahuapi.ResourceReference) error {
	csiSnapshot, err := s.snapshotClient.VolumeSnapshots(pvc.Namespace).Create(context.TODO(),
		&snapshotapiv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultSnapshotNamePrefix + "-" + string(uuid.NewUUID()),
				Namespace: pvc.Namespace,
			},
			Spec: snapshotapiv1.VolumeSnapshotSpec{
				Source: snapshotapiv1.VolumeSnapshotSource{
					PersistentVolumeClaimName: &pvc.Name,
				},
				VolumeSnapshotClassName: &snapshotClassName,
			}}, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	kahuVolSnapshot, err := s.kahuClient.
		KahuV1beta1().
		VolumeSnapshots().
		Get(context.TODO(), kahuVolSnapshotName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for i, snapshotState := range kahuVolSnapshot.Status.SnapshotStates {
		if snapshotState.PVC.Name == pvc.Name &&
			snapshotState.PVC.Namespace == pvc.Namespace {
			kahuVolSnapshot.Status.SnapshotStates[i].CSISnapshotRef = &kahuapi.ResourceReference{
				Namespace: csiSnapshot.Namespace,
				Name:      csiSnapshot.Name,
			}
		}
	}

	_, err = s.kahuClient.
		KahuV1beta1().
		VolumeSnapshots().
		UpdateStatus(context.TODO(), kahuVolSnapshot, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (s *snapshoter) waitForSnapshotToReady(snapshotID string, timeout time.Duration) error {
	s.logger.Infof("Probing for Snapshot Status [id=%v]", snapshotID)

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

		if snapshot.GetDeletionTimestamp() != nil {
			s.logger.Errorf("Snapshot [%s] has deletion timestamp, will not continue to wait for volume snapshot",
				snapshot.UID)
			return false, fmt.Errorf("snapshot(%s) is being deleted", snapshot.Name)
		}

		failureMsg := snapshot.Status.FailureReason
		if len(failureMsg) != 0 {
			s.logger.Errorf("Snapshot for %v failed: %v", snapshot.Name, failureMsg)
			return false, fmt.Errorf("snapshot(%s) failed already", snapshot.Name)
		}

		readyToUse, err := s.verifySnapshotStatus(snapshot)
		if err != nil {
			s.logger.Errorf("Failed to update Volume Snapshot(%s) status, %s", snapshot.Name, err)
			return false, nil
		}

		return readyToUse, nil
	}

	return s.waitForSnapshotStatus(snapshotID, timeout, verifyStatus, "Create")
}

func readyToUse(status *bool) bool {
	return status != nil && *status == true
}

func (s *snapshoter) verifySnapshotStatus(snapshot *kahuapi.VolumeSnapshot) (bool, error) {
	// attachment OK
	if readyToUse(snapshot.Status.ReadyToUse) {
		return true, nil
	}

	// check CSI snapshot, if all CSI Snapshot Ready to use return success
	for _, snapshotState := range snapshot.Status.SnapshotStates {
		if snapshotState.CSISnapshotRef == nil {
			return false, fmt.Errorf("waiting to populate csi info")
		}

		readyToUse, err := s.checkCSISnapshotStatus(snapshotState.CSISnapshotRef)
		if err != nil {
			return false, err
		}

		if !readyToUse {
			return false, nil
		}
	}

	return true, nil
}

func (s *snapshoter) checkCSISnapshotStatus(csiSnapshot *kahuapi.ResourceReference) (bool, error) {
	snapshot, err := s.snapshotClient.VolumeSnapshots(csiSnapshot.Namespace).Get(context.TODO(),
		csiSnapshot.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	if snapshot.Status.ReadyToUse == nil {
		return false, fmt.Errorf("CSI snapshot (%s) status not updated", snapshot.Name)
	}

	return *snapshot.Status.ReadyToUse, nil
}

func (s *snapshoter) waitForSnapshotStatus(snapshotID string,
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
			s.logger.Errorf("%s timeout for snapshot after %v [snapshotID=%s]",
				operation, timeout, snapshotID)
			return errors.New("timed out waiting for external-snapshotting")
		}
	}
}
