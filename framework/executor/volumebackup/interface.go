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

package volumebackup

import (
	"context"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	volumeservice "github.com/soda-cdm/kahu/providerframework/volumeservice/lib/go"
)

type Service interface {
	Probe(ctx context.Context) error
	Backup(ctx context.Context, vbc *kahuapi.VolumeBackupContent) error
	DeleteBackup(ctx context.Context, vbc *kahuapi.VolumeBackupContent) error
	Restore(ctx context.Context, vrc *kahuapi.VolumeRestoreContent) error

	Cleanup(context.Context) error
	Sync()
}

type podTemplateCallback func(*corev1.PodTemplateSpec)

type volumeServiceProvisioner interface {
	Start(ctx context.Context, callback podTemplateCallback) (volumeService, error)
	Done()
	Remove() error
	Sync()
}

type volumeService interface {
	volumeservice.VolumeServiceClient
	close() error
}

type printResource struct {
	logger *log.Entry
}

func NewEmptyVolumeBackupService() Service {
	return &printResource{
		logger: log.WithField("module", "print-store"),
	}
}

func (p *printResource) Cleanup(_ context.Context) error {
	return nil
}

func (p *printResource) Probe(_ context.Context) error {
	return nil
}

func (p *printResource) Backup(ctx context.Context, vbc *kahuapi.VolumeBackupContent) error {
	p.logger.Infof("backup resource %s", vbc.Name)

	return nil
}

func (p *printResource) DeleteBackup(ctx context.Context, vbc *kahuapi.VolumeBackupContent) error {
	p.logger.Infof("Delete backup resource %s", vbc.Name)
	return nil
}

func (p *printResource) Restore(ctx context.Context, vrc *kahuapi.VolumeRestoreContent) error {
	p.logger.Infof("Restore resource %s", vrc.Name)
	return nil
}

func (_ *printResource) close() error {
	return nil
}

func (_ *printResource) start(ctx context.Context) error {
	return nil
}

func (_ *printResource) done() {
	return
}

func (_ *printResource) remove() error {
	return nil
}

func (p *printResource) Sync() {
	p.logger.Infof("Resource synced")
}
