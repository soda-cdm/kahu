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

package volume

import (
	"context"

	"github.com/soda-cdm/kahu/client"
	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/volume/backup"
	"github.com/soda-cdm/kahu/volume/group"
	"github.com/soda-cdm/kahu/volume/location"
	"github.com/soda-cdm/kahu/volume/provider"
	"github.com/soda-cdm/kahu/volume/restore"
	"github.com/soda-cdm/kahu/volume/snapshot"
)

type Interface interface {
	GroupGetter
	SnapshotGetter
	BackupGetter
	RestoreGetter
	ProviderGetter
	LocationGetter
}

type GroupGetter interface {
	Group() group.Factory
}

type SnapshotGetter interface {
	Snapshot() snapshot.Snapshotter
}

type BackupGetter interface {
	Backup() backup.Factory
}

type RestoreGetter interface {
	Restore(group.Interface) restore.Factory
}

type ProviderGetter interface {
	Provider() provider.Interface
}

type LocationGetter interface {
	Location() location.Interface
}

type volume struct {
	client   client.Factory
	snapshot snapshot.Snapshotter
	backup   backup.Factory
	restore  restore.Factory
	group    group.Factory
	provider provider.Interface
	location location.Interface
}

func NewVolumeHandler(ctx context.Context, clientFactory client.Factory, frmWork framework.Interface) (Interface, error) {
	groupFactory, err := group.NewFactory(clientFactory)
	if err != nil {
		return nil, err
	}
	snapshotFactory, err := snapshot.NewSnapshotter(ctx, clientFactory)
	if err != nil {
		return nil, err
	}
	backupFactory, err := backup.NewFactory(clientFactory, frmWork)
	if err != nil {
		return nil, err
	}
	restoreFactory, err := restore.NewFactory(clientFactory, frmWork)
	if err != nil {
		return nil, err
	}
	providerFactory := provider.NewFactory()
	locationFactory := location.NewFactory()

	return &volume{
		client:   clientFactory,
		group:    groupFactory,
		snapshot: snapshotFactory,
		backup:   backupFactory,
		restore:  restoreFactory,
		provider: providerFactory,
		location: locationFactory,
	}, nil
}

func (vol *volume) Group() group.Factory {
	return vol.group
}

func (vol *volume) Snapshot() snapshot.Snapshotter {
	return vol.snapshot
}

func (vol *volume) Backup() backup.Factory {
	return vol.backup
}

func (vol *volume) Restore(group.Interface) restore.Factory {
	return vol.restore
}

func (vol *volume) Provider() provider.Interface {
	return vol.provider
}

func (vol *volume) Location() location.Interface {
	return vol.location
}
