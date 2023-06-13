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

	"github.com/google/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/volume/group"
)

type Factory interface {
	ByVolumes(backupName string, vg group.Interface, location string) (*kahuapi.VolumeBackupContent, error)
	BySnapshots(backupName string, snapshots group.Interface, location string) (*kahuapi.VolumeBackupContent, error)
	Delete(vbcName string, location string) error
}

const (
	labelBackupName           = "kahu.io/backup-name"
	labelVolumeBackupLocation = "kahu.io/backup-backup-location"
)

type factory struct {
	kubeClient    kubernetes.Interface
	dynamicClient dynamic.Interface
	kahuClient    clientset.Interface
	framework     framework.Interface
	logger        log.FieldLogger
}

func NewFactory(clientFactory client.Factory) (Factory, error) {
	kubeClient, err := clientFactory.KubeClient()
	if err != nil {
		return nil, err
	}

	dynamicClient, err := clientFactory.DynamicClient()
	if err != nil {
		return nil, err
	}

	kahuClient, err := clientFactory.KahuClient()
	if err != nil {
		return nil, err
	}

	return &factory{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
		kahuClient:    kahuClient,
		logger:        log.WithField("module", "backupper"),
	}, nil
}

func getVBCName() string {
	return fmt.Sprintf("vbc-%s", uuid.New().String())
}

func vbcLabels(backupName, location string) *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{
			labelBackupName:           backupName,
			labelVolumeBackupLocation: location,
		},
	}
}

func (f *factory) ByVolumes(backupName string,
	vg group.Interface,
	location string) (*kahuapi.VolumeBackupContent, error) {
	vbc, err := f.ensureVBCByVolumes(backupName, vg, location)
	if err != nil {
		return nil, err
	}

	return vbc, f.backup(vbc, location)
}

func (f *factory) BySnapshots(backupName string,
	vg group.Interface,
	location string) (*kahuapi.VolumeBackupContent, error) {
	vbc, err := f.ensureVBCBySnapshots(backupName, vg, location)
	if err != nil {
		return nil, err
	}
	return vbc, f.backup(vbc, location)
}

func (f *factory) Delete(vbcName string, location string) error {
	volService, err := f.framework.Executors().VolumeBackupService(location)
	if err != nil {
		return err
	}

	service, err := volService.Start(context.Background())
	if err != nil {
		return err
	}
	defer volService.Done()

	vbc, err := f.kahuClient.
		KahuV1beta1().
		VolumeBackupContents().
		Get(context.TODO(), vbcName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	err = service.DeleteBackup(context.Background(), vbc)
	if err != nil {
		return err
	}
	defer service.Close()

	return nil
}

func (f *factory) backup(vbc *kahuapi.VolumeBackupContent,
	location string) error {
	// check status
	if vbc.Status.Phase == kahuapi.VolumeBackupContentPhaseCompleted {
		return nil
	}

	volService, err := f.framework.Executors().VolumeBackupService(location)
	if err != nil {
		return err
	}

	service, err := volService.Start(context.Background())
	if err != nil {
		return err
	}
	defer volService.Done()

	err = service.Backup(context.Background(), vbc)
	if err != nil {
		return err
	}
	defer service.Close()

	return nil
}

func (f *factory) ensureVBCByVolumes(backupName string,
	vg group.Interface,
	location string) (*kahuapi.VolumeBackupContent, error) {
	f.logger.Infof("Ensuring volume backup content for %s/%s", vg.GetGroupName())
	labels := vbcLabels(backupName, location)
	provisionerName := vg.GetProvisionerName()
	selector, err := metav1.LabelSelectorAsSelector(labels)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("invalid label selector %s", labels.String()))
	}

	vbcs, err := f.kahuClient.
		KahuV1beta1().
		VolumeBackupContents().
		List(context.TODO(), metav1.ListOptions{
			LabelSelector: selector.String()})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, errors.Wrap(err, "unable to get volume backup content")
	}
	if len(vbcs.Items) > 0 {
		return &vbcs.Items[0], nil
	}

	time := metav1.Now()
	volumeBackupContent := &kahuapi.VolumeBackupContent{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels.MatchLabels,
			Name:   getVBCName(),
		},
		Spec: kahuapi.VolumeBackupContentSpec{
			BackupName:     backupName,
			VolumeProvider: &provisionerName,
			Parameters:     make(map[string]string),
			BackupSource:   kahuapi.BackupSource{
				// BackupSourceRef: &snapshotRef,
			},
		},
		Status: kahuapi.VolumeBackupContentStatus{
			Phase:          kahuapi.VolumeBackupContentPhaseInit,
			StartTimestamp: &time,
		},
	}

	vbc, err := f.kahuClient.KahuV1beta1().
		VolumeBackupContents().
		Create(context.TODO(), volumeBackupContent, metav1.CreateOptions{})
	if err != nil {
		f.logger.Errorf("unable to create volume backup content "+
			"for provider %s", vg.GetGroupName())
		return nil, errors.Wrap(err, "unable to create volume backup content")
	}

	return vbc, nil
}

func (f *factory) ensureVBCBySnapshots(backupName string,
	vg group.Interface,
	location string) (*kahuapi.VolumeBackupContent, error) {
	f.logger.Infof("Ensuring volume backup content for %s/%s", vg.GetGroupName())
	labels := vbcLabels(backupName, location)
	provisionerName := vg.GetProvisionerName()
	selector, err := metav1.LabelSelectorAsSelector(labels)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("invalid label selector %s", labels.String()))
	}

	vbcs, err := f.kahuClient.
		KahuV1beta1().
		VolumeBackupContents().
		List(context.TODO(), metav1.ListOptions{
			LabelSelector: selector.String()})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, errors.Wrap(err, "unable to get volume backup content")
	}
	if len(vbcs.Items) > 0 {
		return &vbcs.Items[0], nil
	}

	time := metav1.Now()
	volumeBackupContent := &kahuapi.VolumeBackupContent{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels.MatchLabels,
			Name:   getVBCName(),
		},
		Spec: kahuapi.VolumeBackupContentSpec{
			BackupName:     backupName,
			VolumeProvider: &provisionerName,
			Parameters:     make(map[string]string),
			BackupSource:   kahuapi.BackupSource{
				// BackupSourceRef: &snapshotRef,
			},
		},
		Status: kahuapi.VolumeBackupContentStatus{
			Phase:          kahuapi.VolumeBackupContentPhaseInit,
			StartTimestamp: &time,
		},
	}

	vbc, err := f.kahuClient.KahuV1beta1().
		VolumeBackupContents().
		Create(context.TODO(), volumeBackupContent, metav1.CreateOptions{})
	if err != nil {
		f.logger.Errorf("unable to create volume backup content "+
			"for provider %s", vg.GetGroupName())
		return nil, errors.Wrap(err, "unable to create volume backup content")
	}

	return vbc, nil
}
