package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
type Backup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupSpec   `json:"spec,omitempty"`
	Status BackupStatus `json:"status,omitempty"`
}

type BackupSpec struct {
	// +optional
	ReclaimPolicy ReclaimPolicyType `json:"reclaimPolicy,omitempty"`

	// +nullable
	// +optional
	MetadataLocation *BackupLocation `json:"metadataLocation,omitempty"`

	// +optional
	PreExecHook ResourceHookSpec `json:"preExecHook,omitempty"`

	// +optional
	PostExecHook ResourceHookSpec `json:"postExecHook,omitempty"`

	// +optional
	IncludedProviders []string `json:"includedProviders,omitempty"`

	// +optional
	ExcludeProviders []string `json:"excludedProviders,omitempty"`

	// +optional
	EnableMetadataBackup bool `json:"enableMetadataBackup,omitempty"`

	// +optional
	EnableVolumeBackup bool `json:"enableVolumeBackup,omitempty"`

	// +optional
	IncludedNamespaces []string `json:"includedNamespaces,omitempty"`

	// +optional
	ExcludedNamespaces []string `json:"excludedNamespaces,omitempty"`

	// +optional
	IncludedResources []string `json:"includedResources,omitempty"`

	// +optional
	ExcludedResources []string `json:"excludedResources,omitempty"`

	// +optional
	Label *metav1.LabelSelector `json:"label,omitempty"`
}

type ResourceHookSpec struct {
	// +optional
	Name string `json:"name"`

	// +optional
	IncludedNamespaces []string `json:"includedNamespaces,omitempty"`

	// +optional
	ExcludedNamespaces []string `json:"excludedNamespaces,omitempty"`

	// +optional
	IncludedResources []string `json:"includedResources,omitempty"`

	// +optional
	ExcludedResources []string `json:"excludedResources,omitempty"`

	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

type ReclaimPolicyType struct {
	// +optional
	ReclaimPolicyDelete string `json:"reclaimPolicyDelete,omitempty"`

	// +optional
	ReclaimPolicyRetain string `json:"reclaimPolicyRetain,omitempty"`
}

type BackupCondition struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	ResourceName string `json:"resourceName,omitempty"`

	// +optional
	Status string `json:"status,omitempty"`
}

type BackupPhase string

const (
	// BackupPhaseNew means the backup has been created but not
	// yet processed by the BackupController.
	BackupPhaseInit BackupPhase = "New"

	// BackupPhaseFailedValidation means the backup has failed
	// the controller's validations and therefore will not run.
	BackupPhaseFailedValidation BackupPhase = "FailedValidation"

	// BackupPhaseInProgress means the backup is currently executing.
	BackupPhaseInProgress BackupPhase = "InProgress"

	// BackupPhaseUploading means the backups of Kubernetes resources
	// and creation of snapshots was successful and snapshot data
	// is currently uploading.  The backup is not usable yet.
	BackupPhaseUploading BackupPhase = "Uploading"

	// BackupPhaseUploadingPartialFailure means the backup of Kubernetes
	// resources and creation of snapshots partially failed (final phase
	// will be PartiallyFailed) and snapshot data is currently uploading.
	// The backup is not usable yet.
	BackupPhaseUploadingPartialFailure BackupPhase = "UploadingPartialFailure"

	// BackupPhaseCompleted means the backup has run successfully without
	// errors.
	BackupPhaseCompleted BackupPhase = "Completed"

	// BackupPhasePartiallyFailed means the backup has run to completion
	// but encountered 1+ errors backing up individual items.
	BackupPhasePartiallyFailed BackupPhase = "PartiallyFailed"

	// BackupPhaseFailed means the backup ran but encountered an error that
	// prevented it from completing successfully.
	BackupPhaseFailed BackupPhase = "Failed"

	// BackupPhaseDeleting means the backup and all its associated data are being deleted.
	BackupPhaseDeleting BackupPhase = "Deleting"
)

type BackupStatus struct {
	// +optional
	Phase BackupPhase `json:"phase,omitempty"`
	// +optional
	LastBackup *metav1.Time `json:"lastBackup,omitempty"`

	// +optional
	ValidationErrors []string `json:"validationErrors,omitempty"`

	// +optional
	Conditions []BackupCondition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type BackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// +optional
	Items []Backup `json:"items"`
}
