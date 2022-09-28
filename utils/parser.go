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

package utils

import (
	"fmt"
	"path/filepath"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	pb "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
)

func ResourceToFile(backupHandle string, resource *pb.Resource) string {
	if resource.Group == "" {
		return filepath.Join(backupHandle,
			resource.Kind,
			resource.Version,
			resource.Name)
	}
	return filepath.Join(backupHandle,
		resource.Kind,
		resource.Group+"."+resource.Version,
		resource.Name)
}

func FileToResource(filePath string) *pb.Resource {
	resource := &pb.Resource{}
	dir, file := filepath.Split(filePath)
	resource.Name = file
	dir, file = filepath.Split(dir)
	resource.Kind = file
	dir, file = filepath.Split(dir)
	resource.Group = file
	dir, file = filepath.Split(dir)
	resource.Version = file

	return resource
}

func GetBackupIdentifier(backup *kahuapi.Backup,
	backupLocation *kahuapi.BackupLocation,
	backupProvider *kahuapi.Provider) (*pb.BackupIdentifier, error) {
	if backup == nil || backupLocation == nil || backupProvider == nil {
		return nil, fmt.Errorf("empty backup information for backup identifier")
	}

	mergedParameter := make(map[string]string, 0)

	// construct merged parameter with Provider first
	// User can override Provider parameters with Backup location
	for key, val := range backupProvider.Spec.Manifest {
		mergedParameter[key] = val
	}

	// construct parameter from backup location
	for key, val := range backupLocation.Spec.Config {
		mergedParameter[key] = val
	}

	return &pb.BackupIdentifier{
		BackupHandle: backup.Name,
		Parameters:   mergedParameter,
	}, nil
}
