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

	"github.com/soda-cdm/kahu/utils"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
)

type Interface interface {
	Apply(backupName string) error
	Delete() error
}

type backup struct {
	logger          log.FieldLogger
	kubeClient      kubernetes.Interface
	kahuClient      clientset.Interface
	dynamicClient   dynamic.Interface
	backupSourceRef []kahuapi.ResourceReference
}

const (
	volumeContentBackupLabel    = "kahu.io/backup-name"
	volumeContentVolumeProvider = "kahu.io/backup-provider"
)

func newBackup(kubeClient kubernetes.Interface,
	kahuClient clientset.Interface,
	dynamicClient dynamic.Interface,
	backupSourceRef ...kahuapi.ResourceReference) (Interface, error) {
	return &backup{
		kubeClient:      kubeClient,
		dynamicClient:   dynamicClient,
		kahuClient:      kahuClient,
		backupSourceRef: backupSourceRef,
		logger:          log.WithField("module", "backupper"),
	}, nil
}

func (b *backup) Apply(backupName string) error {
	b.logger.Infof("Starting volume backup for %s", backupName)
	for _, sourceRef := range b.backupSourceRef {
		b.logger.Infof("Ensuring volume backup content for %s/%s", sourceRef.Kind, sourceRef.Name)
		switch sourceRef.Kind {
		case utils.KahuSnapshot:
			if err := b.ensureVBCWithBackupSourceRef(backupName, sourceRef); err != nil {
				return err
			}
		}
	}

	return nil
}

func getVBCName() string {
	return fmt.Sprintf("vbc-%s", uuid.New().String())
}

func (b *backup) ensureVBCWithBackupSourceRef(backupName string,
	snapshotRef kahuapi.ResourceReference) error {
	kahuVolSnapshot, err := b.
		kahuClient.
		KahuV1beta1().
		VolumeSnapshots().
		Get(context.TODO(), snapshotRef.Name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to get volume snapshot")
	}

	snapshotProvider := *kahuVolSnapshot.Spec.SnapshotProvider
	time := metav1.Now()
	volumeBackupContent := &kahuapi.VolumeBackupContent{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				volumeContentBackupLabel:    backupName,
				volumeContentVolumeProvider: snapshotProvider,
			},
			Name: getVBCName(),
		},
		Spec: kahuapi.VolumeBackupContentSpec{
			BackupName:      backupName,
			VolumeProvider:  &snapshotProvider,
			Parameters:      make(map[string]string),
			BackupSourceRef: &snapshotRef,
		},
		Status: kahuapi.VolumeBackupContentStatus{
			Phase:          kahuapi.VolumeBackupContentPhaseInit,
			StartTimestamp: &time,
		},
	}

	_, err = b.kahuClient.KahuV1beta1().
		VolumeBackupContents().
		Create(context.TODO(), volumeBackupContent, metav1.CreateOptions{})
	if err != nil {
		b.logger.Errorf("unable to create volume backup content "+
			"for provider %s", snapshotProvider)
		return errors.Wrap(err, "unable to create volume backup content")
	}

	return err
}

func (*backup) Delete() error {
	return nil
}
