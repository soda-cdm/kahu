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

package v1beta1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VolumeBackupContentSpec defines the desired state of VolumeBackupContent
type VolumeBackupContentSpec struct {
	// BackupName is backup CR name specified during backup
	// +required
	BackupName string `json:"backupName"`

	// Volume represents kubernetes volume to be backed up
	// +optional
	// +nullable
	Volumes []v1.PersistentVolume `json:"volumes,omitempty"`

	// Volume provider for set of volumes
	VolumeProvider *string `json:"volumeProvider,omitempty"`

	// Supported volume backup provider information
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`

	BackupSourceRef *ResourceReference `json:"backupSourceRef,omitempty"`
}

// +kubebuilder:validation:Enum=New;InProgress;Completed;Failed;Deleting

type VolumeBackupContentPhase string

const (
	VolumeBackupContentPhaseInit       VolumeBackupContentPhase = "New"
	VolumeBackupContentPhaseInProgress VolumeBackupContentPhase = "InProgress"
	VolumeBackupContentPhaseCompleted  VolumeBackupContentPhase = "Completed"
	VolumeBackupContentPhaseFailed     VolumeBackupContentPhase = "Failed"
	VolumeBackupContentPhaseDeleting   VolumeBackupContentPhase = "Deleting"
)

type VolumeBackupState struct {
	VolumeName string `json:"volumeName,omitempty"`

	BackupHandle string `json:"backupHandle,omitempty"`

	BackupAttributes map[string]string `json:"backupAttributes,omitempty"`

	Progress int64 `json:"progress,omitempty"`

	LastProgressUpdate int64 `json:"lastProgressUpdate,omitempty"`
}

// VolumeBackupContentStatus defines the observed state of VolumeBackupContent
type VolumeBackupContentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +optional
	Phase VolumeBackupContentPhase `json:"phase,omitempty"`

	// +optional
	VolumeBackupProvider *string `json:"volumeBackupProvider,omitempty"`

	// +optional
	// +nullable
	StartTimestamp *metav1.Time `json:"startTimestamp,omitempty"`

	// +optional
	// +nullable
	CompletionTimestamp *metav1.Time `json:"completionTimestamp,omitempty"`

	// +optional
	FailureReason string `json:"failureReason,omitempty"`

	// +optional
	BackupState []VolumeBackupState `json:"backupState,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +genclient:skipVerbs=update
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName="vbc"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Backup",type=string,JSONPath=`.spec.backupName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// VolumeBackupContent is the Schema for the VolumeBackupContents API
type VolumeBackupContent struct {
	metav1.TypeMeta `json:",inline"`

	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec VolumeBackupContentSpec `json:"spec,omitempty"`

	// +optional
	Status VolumeBackupContentStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VolumeBackupContentList contains a list of VolumeBackupContent
type VolumeBackupContentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VolumeBackupContent `json:"items"`
}
