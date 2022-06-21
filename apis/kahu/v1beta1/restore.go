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

// RestoreSpec defines the desired state of Restore
type RestoreSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// BackupName is backup CR name specified during backup
	BackupName string `json:"backupName,omitempty"`

	// IncludeNamespaces are set of namespaces name considered for restore
	// +optional
	IncludeNamespaces []string `json:"includeNamespace,omitempty"`

	// ExcludeNamespaces are set of namespace name should not get considered for restore
	// +optional
	ExcludeNamespaces []string `json:"excludeNamespaces,omitempty"`

	// IncludeResources are set of kubernetes resource name considered for restore
	// +optional
	IncludeResources []string `json:"includeResources,omitempty"`

	// ExcludeResources are set of kubernetes resource name should not get considered for restore
	// +optional
	ExcludeResources []string `json:"excludeResources,omitempty"`

	// LabelSelector are label get evaluated against resource selection
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// NamespaceMapping is mapping between backed up namespace name against restore namespace name
	// +optional
	NamespaceMapping map[string]string `json:"namespaceMapping,omitempty"`

	// IncludeClusterResource is a flag for considering cluster wider resource during restore
	// +optional
	IncludeClusterResource bool `json:"includeClusterResource,omitempty"`

	// ResourcePrefix gets prepended in each restored resource name
	// +optional
	ResourcePrefix string `json:"resourcePrefix,omitempty"`
}

// +kubebuilder:validation:Enum=New;FailedValidation;InProgress;Completed;PartiallyFailed;Failed;Deleting

type RestorePhase string

const (
	RestorePhaseInit             RestorePhase = "New"
	RestorePhaseFailedValidation RestorePhase = "FailedValidation"
	RestorePhaseInProgress       RestorePhase = "InProgress"
	RestorePhaseCompleted        RestorePhase = "Completed"
	RestorePhasePartiallyFailed  RestorePhase = "PartiallyFailed"
	RestorePhaseFailed           RestorePhase = "Failed"
	RestorePhaseDeleting         RestorePhase = "Deleting"
)

// RestoreProgress expresses overall progress of restore
type RestoreProgress struct {
	// TotalItems is count of resource to be process
	// +optional
	TotalItems int `json:"totalItems,omitempty"`

	// ItemsRestored is count of resource got processed
	// +optional
	ItemsRestored int `json:"itemsRestored,omitempty"`
}

// RestoreStatus defines the observed state of Restore
type RestoreStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +optional
	Phase RestorePhase `json:"phase,omitempty"`

	// +optional
	StartTimestamp metav1.Timestamp `json:"startTimestamp,omitempty"`

	// +optional
	CompletionTimestamp metav1.Timestamp `json:"completionTimestamp,omitempty"`

	// +optional
	Progress RestoreProgress `json:"progress,omitempty"`

	// +optional
	FailureReason []string `json:"failureReason,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Restore is the Schema for the restores API
type Restore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RestoreSpec   `json:"spec,omitempty"`
	Status RestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RestoreList contains a list of Restore
type RestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Restore `json:"items"`
}
