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

package options

import (
	"github.com/soda-cdm/kahu/framework/executor"
	"github.com/soda-cdm/kahu/framework/executor/resourcebackup"
	"github.com/soda-cdm/kahu/framework/executor/volumebackup"
	"github.com/spf13/pflag"

	"github.com/soda-cdm/kahu/framework"
)

type FrameworkFlags struct {
	framework.Config
}

const (
	defaultMetaServiceImage = "sodacdm/kahu-meta-service:v1.0.0"
	defaultNamespace        = "kahu"
)

func NewFrameworkFlags() *FrameworkFlags {
	return &FrameworkFlags{
		framework.Config{
			Executor: executor.Config{
				ResourceBackup: resourcebackup.Config{
					MetaBackupServiceImage: defaultMetaServiceImage,
					MetaServicePort:        resourcebackup.DefaultMetaServiceServicePort,
				},
				Namespace: defaultNamespace,
				VolumeBackup: volumebackup.Config{
					VolumeBackupServiceImage: volumebackup.DefaultVolumeServiceImage,
					VolumeServicePort:        volumebackup.DefaultServicePort,
				},
			},
		},
	}
}

// AddFlags exposes available command line options
func (opt *FrameworkFlags) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&opt.Executor.ResourceBackup.MetaBackupServiceImage, "metaServiceImage",
		opt.Executor.ResourceBackup.MetaBackupServiceImage,
		"meta service side car image. Example: --metaServiceImage=sodacdm/kahu-meta-service:v1.0.0")
	fs.IntVar(&opt.Executor.ResourceBackup.MetaServicePort, "metaServicePort",
		opt.Executor.ResourceBackup.MetaServicePort,
		"meta service side car image. Example: --metaSidecarPort=443")
	fs.StringVarP(&opt.Executor.Namespace, "executorNamespace", "n",
		opt.Executor.Namespace,
		"namespace for resource and volume backup provisioners . Example: --executorNamespace=default")
	fs.StringVar(&opt.Executor.VolumeBackup.VolumeBackupServiceImage, "volumeServiceImage",
		opt.Executor.VolumeBackup.VolumeBackupServiceImage,
		"volume service side car image. Example: --volumeServiceImage=sodacdm/kahu-volume-service:v1.0.0")
	fs.IntVar(&opt.Executor.VolumeBackup.VolumeServicePort, "volumeServicePort",
		opt.Executor.VolumeBackup.VolumeServicePort,
		"volume service side car image. Example: --volumeServicePort=443")
}

// ApplyTo checks validity of available command line options
func (opt *FrameworkFlags) ApplyTo(cfg *framework.Config) error {
	cfg.Executor.ResourceBackup.MetaBackupServiceImage = opt.Executor.ResourceBackup.MetaBackupServiceImage
	cfg.Executor.ResourceBackup.MetaServicePort = opt.Executor.ResourceBackup.MetaServicePort
	cfg.Executor.Namespace = opt.Executor.Namespace
	cfg.Executor.VolumeBackup.VolumeBackupServiceImage = opt.Executor.VolumeBackup.VolumeBackupServiceImage
	cfg.Executor.VolumeBackup.VolumeServicePort = opt.Executor.VolumeBackup.VolumeServicePort
	return nil
}
