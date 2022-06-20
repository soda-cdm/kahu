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
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
type Backup struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec BackupSpec `json:"spec,omitempty"`

	// +optional
	Status BackupStatus `json:"status,omitempty"`
}

type BackupSpec struct {
	// MetadataLocation is location where backup is going to be stored
	// +nullable
	// +optional
	MetadataLocation *BackupLocation `json:"metadataLocation"`

	// ReclaimPolicy tells about reclamation of the backup. It can be either delete or retain
	// +optional
	ReclaimPolicy ReclaimPolicyType `json:"reclaimPolicy,omitempty"`

	// PreExecHook is a hook which should be executed before backup starts
	// +optional
	PreExecHook ResourceHookSpec `json:"preExecHook,omitempty"`

	// PostExecHook is a hook which should be executed after backup finished
	// +optional
	PostExecHook ResourceHookSpec `json:"postExecHook,omitempty"`

	// IncludedProviders is a list of all provideres included for backup. If empty, all provideres
	// are included
	// +optional
	IncludedProviders []string `json:"includedProviders,omitempty"`

	// ExcludedProviders is a list of all provideres excluded for backup
	// +optional
	ExcludeProviders []string `json:"excludedProviders,omitempty"`

	// EnableMetadataBackup tells whether metadata backup should be taken or not
	// +optional
	EnableMetadataBackup bool `json:"enableMetadataBackup,omitempty"`

	// EnableMetadataBackup tells whether volumes(PV) backup should be taken or not
	// +optional
	EnableVolumeBackup bool `json:"enableVolumeBackup,omitempty"`

	// IncludedNamespaces is a list of all namespaces included for backup. If empty, all namespaces
	// are included
	// +optional
	IncludedNamespaces []string `json:"includedNamespaces,omitempty"`

	// ExcludedNamespaces is a list of all namespaces excluded for backup
	// +optional
	ExcludedNamespaces []string `json:"excludedNamespaces,omitempty"`

	// IncludedResources is a list of all resources included for backup. If empty, all resources
	// are included
	// +optional
	IncludedResources []string `json:"includedResources,omitempty"`

	// ExcludedResources is a list of all resources excluded for backup
	// +optional
	ExcludedResources []string `json:"excludedResources,omitempty"`

	// Label is used to filter the resources
	// +optional
	Label *metav1.LabelSelector `json:"label,omitempty"`
}

// ResourceHookSpec is hook which should be executed
// at different phase of backup
type ResourceHookSpec struct {
	// +optional
	Name string `json:"name"`

	// IncludedNamespaces is a list of all namespaces included for hook. If empty, all namespaces
	// are included
	// +optional
	IncludedNamespaces []string `json:"includedNamespaces,omitempty"`

	// ExcludedNamespaces is a list of all namespaces excluded for hook
	// +optional
	ExcludedNamespaces []string `json:"excludedNamespaces,omitempty"`

	// IncludedResources is a list of all resources included for hook. If empty, all resources
	// are included
	// +optional
	IncludedResources []string `json:"includedResources,omitempty"`

	// ExcludedResources is a list of all resources excluded for backup
	// +optional
	ExcludedResources []string `json:"excludedResources,omitempty"`

	// Label is used to filter the resources
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// ReclaimPolicy tells about reclamation of the backup. It can be either delete or retain
type ReclaimPolicyType struct {
	// +optional
	ReclaimPolicyDelete string `json:"reclaimPolicyDelete,omitempty"`

	// +optional
	ReclaimPolicyRetain string `json:"reclaimPolicyRetain,omitempty"`
}

// BackupCondition indicates the current state of a resource that is backing up
type BackupCondition struct {
	metav1.TypeMeta `json:",inline"`

	// ResourceName is a one of the item of backup that is backing up
	// +optional
	ResourceName string `json:"resourceName,omitempty"`

	// Status is a state of the resource
	// +optional
	Status string `json:"status,omitempty"`
}

// BackupPhase is a state of backup
// +kubebuilder:validation:Enum=New;FailedValidation;InProgress;Completed;PartiallyFailed;Failed;Deleting
type BackupPhase string

const (
	// BackupPhaseInit indicates that current backup object is New
	BackupPhaseInit BackupPhase = "New"

	// BackupPhaseFailedValidation indicates that backup object has validation issues
	BackupPhaseFailedValidation BackupPhase = "FailedValidation"

	// BackupPhaseInProgress indicates that backup is executing by controller's
	BackupPhaseInProgress BackupPhase = "InProgress"

	// BackupPhaseCompleted indicates that backup is successfully completed
	BackupPhaseCompleted BackupPhase = "Completed"

	// BackupPhasePartiallyFailed indicates that some of the backup items are not backuped successfully
	BackupPhasePartiallyFailed BackupPhase = "PartiallyFailed"

	// BackupPhaseFailed indicates that backup is failed due to some errors
	BackupPhaseFailed BackupPhase = "Failed"

	// BackupPhaseDeleting indicates that backup and all its associated data are being deleted
	BackupPhaseDeleting BackupPhase = "Deleting"
)

type BackupStatus struct {
	// Phase is the current state of the backup
	// +optional
	Phase BackupPhase `json:"phase,omitempty"`

	// LastBackup defines the last backup time
	// +optional
	LastBackup *metav1.Time `json:"lastBackup,omitempty"`

	// ValidationErrors is a list of erros which are founded during validation of backup spec
	// +optional
	ValidationErrors []string `json:"validationErrors,omitempty"`

	// Conditions tells the current state of a resource that is backing up
	// +optional
	Conditions []BackupCondition `json:"conditions,omitempty"`

	// StartTimestamp is defines time when backup started
	// +optional
	// +nullable
	StartTimestamp *metav1.Time `json:"startTimestamp,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type BackupList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// +optional
	Items []Backup `json:"items"`
}
