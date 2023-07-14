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
	"fmt"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/utils/k8sresource"
	"github.com/soda-cdm/kahu/volume/group"
)

const (
	labelBackupName      = "kahu.io/backup-name"
	labelProvisionerName = "kahu.io/provisioner-name"
)

type Snapshotter interface {
	ByVolumeGroup(backupName string, volGroup group.Interface) (Wait, error)
	GetSnapshotsByBackup(backupName string) ([]kahuapi.VolumeSnapshot, error)
	Delete(volSnapshot string) error
	GetSnapshotsByProvisioner() ([]*kahuapi.VolumeSnapshot, error, error)
}

type snapshotter struct {
	ctx        context.Context
	kubeClient kubernetes.Interface
	kahuClient clientset.Interface
}

func NewSnapshotter(ctx context.Context, clientFactory client.Factory) (Snapshotter, error) {
	kubeClient, err := clientFactory.KubeClient()
	if err != nil {
		return nil, err
	}

	kahuClient, err := clientFactory.KahuClient()
	if err != nil {
		return nil, err
	}

	return &snapshotter{
		ctx:        ctx,
		kubeClient: kubeClient,
		kahuClient: kahuClient,
	}, nil
}

func (s *snapshotter) ByVolumeGroup(backupName string, volGroup group.Interface) (Wait, error) {
	volumes := volGroup.GetResources()
	provisioner := volGroup.GetProvisionerName()
	if provisioner == "" {
		return nil, fmt.Errorf("empty volume provisioner")
	}

	volRef := make([]kahuapi.ResourceReference, 0)
	for _, volume := range volumes {
		apiVersion, kind := k8sresource.PersistentVolumeClaimGVK.ToAPIVersionAndKind()
		volRef = append(volRef, kahuapi.ResourceReference{
			Kind:       kind,
			APIVersion: apiVersion,
			Name:       volume.GetName(),
			Namespace:  volume.GetNamespace(),
		})
	}

	snapshot, err := s.snapshot(backupName, provisioner, volRef)
	if err != nil {
		// delete older kahu snapshot objects
		return nil, err
	}

	return newSnapshotWait(s.ctx, s.kahuClient, snapshot.Name), nil
}

func (s *snapshotter) GetSnapshotsByBackup(backupName string) ([]kahuapi.VolumeSnapshot, error) {
	labels := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			labelBackupName: backupName,
		},
	}
	selector, err := metav1.LabelSelectorAsSelector(labels)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("invalid label selector %s", labels.String()))
	}

	volSnapshots, err := s.kahuClient.
		KahuV1beta1().
		VolumeSnapshots().
		List(context.TODO(), metav1.ListOptions{
			LabelSelector: selector.String()})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, errors.Wrap(err, "unable to get volume snapshots")
	}

	return volSnapshots.Items, nil
}
func (s *snapshotter) Delete(volSnapshot string) error {
	return nil
}
func (s *snapshotter) GetSnapshotsByProvisioner() ([]*kahuapi.VolumeSnapshot, error, error) {
	return nil, nil, nil
}

func (s *snapshotter) snapshot(backup string,
	provisioner string, volumes []kahuapi.ResourceReference) (*kahuapi.VolumeSnapshot, error) {
	kahuSnapshot := s.volGroupToSnapshot(backup, provisioner, volumes)

	return s.kahuClient.
		KahuV1beta1().
		VolumeSnapshots().
		Create(context.TODO(), kahuSnapshot, metav1.CreateOptions{})
}

func (s *snapshotter) volGroupToSnapshot(backup string,
	provisioner string,
	vols []kahuapi.ResourceReference) *kahuapi.VolumeSnapshot {
	return &kahuapi.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "snapshot-" + uuid.New().String(),
			Labels: s.snapshotLabel(backup, provisioner).MatchLabels,
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

func (s *snapshotter) snapshotLabel(backup, provisioner string) *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{
			labelBackupName:      backup,
			labelProvisionerName: provisioner,
		},
	}
}
