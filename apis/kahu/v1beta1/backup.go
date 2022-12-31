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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Stage",type=string,JSONPath=`.status.stage`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="MetadataLocation",type=string,JSONPath=`.spec.metadataLocation`
// +kubebuilder:printcolumn:name="VolumeBackupLocations",type=string,JSONPath=`.spec.volumeBackupLocations`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
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
	// +required
	MetadataLocation string `json:"metadataLocation"`

	// ReclaimPolicy tells about reclamation of the backup. It can be either delete or retain
	// +optional
	ReclaimPolicy ReclaimPolicyType `json:"reclaimPolicy,omitempty"`

	// Hook is pre or post operations which should be executed during backup
	// +optional
	Hook HookSpec `json:"hook,omitempty"`

	// VolumeBackupLocations is a list of all volume providers included for backup.
	// If empty, all providers are included
	// +optional
	VolumeBackupLocations []string `json:"volumeBackupLocations,omitempty"`

	// EnableMetadataBackup tells whether metadata backup should be taken or not
	// +optional
	EnableMetadataBackup bool `json:"enableMetadataBackup,omitempty"`

	// EnableMetadataBackup tells whether volumes(PV) backup should be taken or not
	// +optional
	EnableVolumeBackup bool `json:"enableVolumeBackup,omitempty"`

	// IncludeNamespaces is a list of all namespaces included for backup. If empty, all namespaces
	// are included
	// +optional
	IncludeNamespaces []string `json:"includeNamespaces,omitempty"`

	// ExcludeNamespaces is a list of all namespaces excluded for backup
	// +optional
	ExcludeNamespaces []string `json:"excludeNamespaces,omitempty"`

	// IncludeResources is a list of all resources included for backup. If empty, all resources
	// are included
	// +optional
	IncludeResources []ResourceSpec `json:"includeResources,omitempty"`

	// ExcludeResources is a list of all resources excluded for backup
	// +optional
	ExcludeResources []ResourceSpec `json:"excludeResources,omitempty"`

	// Label is used to filter the resources
	// +optional
	Label *metav1.LabelSelector `json:"label,omitempty"`
}

// HookSpec is hook which should be executed
// at different phase of backup
type HookSpec struct {
	// +optional
	Resources []ResourceHookSpec `json:"resources,omitempty"`
}

// ResourceHookSpec is hook which should be executed
// at different phase of backup
type ResourceHookSpec struct {
	// +required
	Name string `json:"name"`

	// IncludeNamespaces is a list of all namespaces included for hook. If empty, all namespaces
	// are included
	// +optional
	IncludeNamespaces []string `json:"includeNamespaces,omitempty"`

	// ExcludeNamespaces is a list of all namespaces excluded for hook
	// +optional
	ExcludeNamespaces []string `json:"excludeNamespaces,omitempty"`

	// IncludeResources is a list of all resources included for hook. If empty, all resources
	// are included
	// +optional
	IncludeResources []ResourceSpec `json:"includeResources,omitempty"`

	// ExcludeResources is a list of all resources excluded for backup
	// +optional
	ExcludeResources []ResourceSpec `json:"excludeResources,omitempty"`

	// Label is used to filter the resources
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// ContinueHookIfContainerNotFound is used to proceed flag continue hooks when user specified
	// container is not present in the Pod. If empty, container execution fail for missing container
	// +optional
	ContinueHookIfContainerNotFound bool `json:"continueHookIfContainerNotFound,omitempty"`

	// PreHooks is a list of ResourceHooks to execute prior to storing the item in the backup.
	// These are executed before any "additional items" from item actions are processed.
	// +optional
	PreHooks []ResourceHook `json:"pre,omitempty"`

	// PostHooks is a list of ResourceHooks to execute after storing the item in the backup.
	// These are executed after all "additional items" from item actions are processed.
	// +optional
	PostHooks []ResourceHook `json:"post,omitempty"`
}

// ResourceHook defines a hook for a resource.
type ResourceHook struct {
	// Exec defines an exec hook.
	Exec *ExecHook `json:"exec"`
}

// ExecHook is a hook that uses the pod exec API to execute a command in a container in a pod.
type ExecHook struct {
	// Container is the container in the pod where the command should be executed. If not specified,
	// the pod's first container is used.
	// +optional
	Container string `json:"container,omitempty"`

	// Command is the command and arguments to execute.
	// +kubebuilder:validation:MinItems=1
	Command []string `json:"command"`

	// OnError specifies how to behave if it encounters an error executing this hook.
	// +optional
	OnError HookErrorMode `json:"onError,omitempty"`

	// Timeout defines the maximum amount of time service should wait for the hook to complete before
	// considering the execution a failure.
	// +optional
	Timeout metav1.Duration `json:"timeout,omitempty"`
}

// HookErrorMode defines how service should treat an error from a hook.
// +kubebuilder:validation:Enum=Continue;Fail
type HookErrorMode string

const (
	// HookErrorModeContinue means that an error from a hook is acceptable, and the backup can
	// proceed.
	HookErrorModeContinue HookErrorMode = "Continue"

	// HookErrorModeFail means that an error from a hook is problematic, and the backup should be in
	// error.
	HookErrorModeFail HookErrorMode = "Fail"
)

// ReclaimPolicy tells about reclamation of the backup. It can be either delete or retain
type ReclaimPolicyType struct {
	// +optional
	ReclaimPolicyDelete string `json:"reclaimPolicyDelete,omitempty"`

	// +optional
	ReclaimPolicyRetain string `json:"reclaimPolicyRetain,omitempty"`
}

// +kubebuilder:validation:Enum=Pending;Processing;Completed
// ResourceStatus is a state a resource during backup
type ResourceStatus string

const (
	Pending    ResourceStatus = "Pending"
	Processing ResourceStatus = "Processing"
	Completed  ResourceStatus = "Completed"
)

// BackupResource indicates the current state of a resource that is backing up
type BackupResource struct {
	metav1.TypeMeta `json:",inline"`

	// ResourceName is a one of the item of backup that is backing up
	// +optional
	ResourceName string `json:"resourceName,omitempty"`

	// Namespace of the backup resource
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Status is a state of the resource
	// +optional
	Status ResourceStatus `json:"status,omitempty"`
}

// +kubebuilder:validation:Enum=Initial;PreHook;Resources;Volumes;PostHook;Finished
// BackupStage is a stage of backup
type BackupStage string

// +kubebuilder:validation:Enum=New;Validating;Processing;Completed;Deleting;Failed
// BackupState is a state in backup phase
type BackupState string

const (
	// BackupStageInitial indicates that current backup object is New
	BackupStageInitial BackupStage = "Initial"

	// BackupStagePreHook indicates that current backup object is pre hook
	BackupStagePreHook BackupStage = "PreHook"

	// BackupStageResources indicates that metadata are getting backup
	BackupStageResources BackupStage = "Resources"

	// BackupStageVolumes indicates that volume are getting backup
	BackupStageVolumes BackupStage = "Volumes"

	// BackupStagePostHook indicates that current backup object is post hook
	BackupStagePostHook BackupStage = "PostHook"

	// BackupStageFinished indicates that backup is successfully completed
	BackupStageFinished BackupStage = "Finished"

	// BackupStateNew indicates that backup object in initial state
	BackupStateNew BackupState = "New"

	// BackupStateValidating indicates that backup object is under validation
	BackupStateValidating BackupState = "Validating"

	// BackupStateFailed indicates that backup object has validation issues
	BackupStateFailed BackupState = "Failed"

	// BackupStateProcessing indicates that backup state is in progress
	BackupStateProcessing BackupState = "Processing"

	// BackupStateCompleted indicates that completed backup
	BackupStateCompleted BackupState = "Completed"

	// BackupStateDeleting indicates that backup and all its associated data are being deleted
	BackupStateDeleting BackupState = "Deleting"
)

type BackupStatus struct {
	// Stage is the current phase of the backup
	// +optional
	Stage BackupStage `json:"stage,omitempty"`

	// State is the current state in backup
	// +optional
	State BackupState `json:"state,omitempty"`

	// LastBackup defines the last backup time
	// +optional
	LastBackup *metav1.Time `json:"lastBackup,omitempty"`

	// ValidationErrors is a list of erros which are founded during validation of backup spec
	// +optional
	ValidationErrors []string `json:"validationErrors,omitempty"`

	// Conditions tells the current state of a resource that is backing up
	// +optional
	Resources []BackupResource `json:"resources,omitempty"`

	// StartTimestamp is defines time when backup started
	// +optional
	// +nullable
	StartTimestamp *metav1.Time `json:"startTimestamp,omitempty"`

	// CompletionTimestamp is defines time when backup completed
	// +optional
	// +nullable
	CompletionTimestamp *metav1.Time `json:"completionTimestamp,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type BackupList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// +optional
	Items []Backup `json:"items"`
}
