// Copyright 2022 The SODA Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var SchemeGroupVersion = schema.GroupVersion{
	Group:   "kahu.io",
	Version: "v1beta1",
}

var (
	SchemeBuilder runtime.SchemeBuilder
	AddToScheme   = SchemeBuilder.AddToScheme
)

func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

func init() {
	SchemeBuilder.Register(addKnownTypes)
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion, &Backup{}, &BackupList{})
	scheme.AddKnownTypes(SchemeGroupVersion, &Restore{}, &RestoreList{})
	scheme.AddKnownTypes(SchemeGroupVersion, &Provider{}, &ProviderList{})
	scheme.AddKnownTypes(SchemeGroupVersion, &BackupLocation{}, &BackupLocationList{})
	scheme.AddKnownTypes(SchemeGroupVersion, &VolumeBackupContent{}, &VolumeBackupContentList{})
	scheme.AddKnownTypes(SchemeGroupVersion, &VolumeRestoreContent{}, &VolumeRestoreContentList{})
	scheme.AddKnownTypes(SchemeGroupVersion, &VolumeGroup{}, &VolumeGroupList{})
	scheme.AddKnownTypes(SchemeGroupVersion, &VolumeSnapshot{}, &VolumeSnapshotList{})

	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
