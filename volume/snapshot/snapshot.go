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

	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
)

type Wait interface {
	WaitForSnapshotToReady(timeout time.Duration) error
}

type snapshotWait struct {
	ctx          context.Context
	kahuClient   clientset.Interface
	snapshotName string
	logger       log.FieldLogger
}

func newSnapshotWait(ctx context.Context,
	kahuClient clientset.Interface,
	snapshotName string) Wait {
	return &snapshotWait{ctx: ctx,
		kahuClient:   kahuClient,
		snapshotName: snapshotName,
		logger:       log.WithField("module", "snapshotter-wait"),
	}
}

func (s *snapshotWait) WaitForSnapshotToReady(timeout time.Duration) error {
	if err := s.waitForSnapshotToReady(s.snapshotName, timeout); err != nil {
		return err
	}
	return nil
}

func (s *snapshotWait) waitForSnapshotToReady(snapshotID string, timeout time.Duration) error {
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

	return s.waitForSnapshotStatus(snapshotID, timeout, verifyStatus, "Create")
}

func (s *snapshotWait) verifySnapshotStatus(snapshot *kahuapi.VolumeSnapshot) (bool, error) {
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
	return snapshot.Status.ReadyToUse != nil && *snapshot.Status.ReadyToUse
}

func (s *snapshotWait) waitForSnapshotStatus(snapshotID string,
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
		waitTick      = &clock.RealClock{}
	)
	backoffMgr := wait.NewExponentialBackoffManager(initBackoff,
		maxBackoff,
		resetDuration,
		backoffFactor,
		jitter,
		waitTick)

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
			return fmt.Errorf("timed out waiting for external-snapshotting for snapshot=%s", snapshotID)
		}
	}
}

func (s *snapshotWait) GetSnapshots() (*kahuapi.VolumeSnapshot, error) {
	s.logger.Infof("Getting snapshot info for %s", s.snapshotName)

	kahuVolSnapshot, err := s.kahuClient.KahuV1beta1().
		VolumeSnapshots().
		Get(context.TODO(), s.snapshotName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return kahuVolSnapshot, nil
}
