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

package resourcebackup

import (
	"context"
	log "github.com/sirupsen/logrus"
	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

type FnDownload func(resource *metaservice.Resource) error

type Interface interface {
	Probe(ctx context.Context) error
	UploadK8SResource(ctx context.Context, backupID string, resource []k8sresource.Resource) error
	UploadObject(ctx context.Context, backupID string, key string, data []byte) error
	Download(ctx context.Context, backupID string, fnDownload FnDownload) error
	ResourceExists(ctx context.Context, backupID string) (bool, error)
	Delete(ctx context.Context, backupID string) error
	Close() error
}

type Service interface {
	Start(ctx context.Context) (Interface, error)
	Done()
	Remove() error
	Sync()
}

type printResource struct {
	logger *log.Entry
}

func NewEmptyRestoreStore() Interface {
	return &printResource{
		logger: log.WithField("module", "print-store"),
	}
}

func (p *printResource) Probe(_ context.Context) error {
	return nil
}

func (p *printResource) UploadK8SResource(_ context.Context, backupID string, resources []k8sresource.Resource) error {
	for _, resource := range resources {
		p.logger.Infof("Uploaded resource %s", resource.ResourceID())
	}
	return nil
}

func (p *printResource) UploadObject(ctx context.Context, backupID string, key string, data []byte) error {
	p.logger.Infof("Uploaded resource %s", key)
	return nil
}

func (p *printResource) Download(_ context.Context, backupID string, fnDownload FnDownload) error {
	p.logger.Infof("Download resource %s", backupID)
	return nil
}

func (_ *printResource) ResourceExists(ctx context.Context, backupID string) (bool, error) {
	return true, nil
}

func (_ *printResource) Delete(ctx context.Context, backupID string) error {
	return nil
}

func (_ *printResource) Close() error {
	return nil
}
