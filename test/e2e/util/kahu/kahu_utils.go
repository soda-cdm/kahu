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

package kahu

import (
	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
)

func NewBackup(name string, ns string, objType string) *kahuapi.Backup {
	backup := &kahuapi.Backup{}
	backup.APIVersion = "kahu.io/v1beta1"
	backup.Kind = "backup"
	backup.Name = name
	backup.Spec.IncludeNamespaces = []string{ns}
	resourceSpec := kahuapi.ResourceSpec{
		Kind:    objType,
		IsRegex: true,
	}
	backup.Spec.IncludeResources = []kahuapi.ResourceSpec{resourceSpec}
	backup.Spec.MetadataLocation = "nfs"
	return backup
}

func NewRestore(name, nsRestore, nsBackup, backupName string) *kahuapi.Restore {
	restore := &kahuapi.Restore{}
	restore.APIVersion = "kahu.io/v1beta1"
	restore.Kind = "Restore"
	restore.ObjectMeta.Name = name
	restore.Spec.BackupName = backupName
	restore.Spec.NamespaceMapping = map[string]string{nsBackup: nsRestore}
	return restore
}
