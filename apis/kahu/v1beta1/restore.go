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
	// BackupName is backup CR name specified during backup
	// +required
	BackupName string `json:"backupName"`

	// IncludeNamespaces are set of namespaces name considered for restore
	// +optional
	IncludeNamespaces []string `json:"includeNamespaces,omitempty"`

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

// +kubebuilder:validation:Enum=New;MetadataRestore;FailedValidation;InProgress;Completed;PartiallyFailed;Failed;Deleting

type RestorePhase string

const (
	RestorePhaseInit             RestorePhase = "New"
	RestorePhaseMeta             RestorePhase = "MetadataRestore"
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
	// +kubebuilder:default=New
	Phase RestorePhase `json:"phase,omitempty"`

	// +optional
	// +nullable
	StartTimestamp *metav1.Time `json:"startTimestamp,omitempty"`

	// +optional
	// +nullable
	CompletionTimestamp *metav1.Time `json:"completionTimestamp,omitempty"`

	// +optional
	Progress RestoreProgress `json:"progress,omitempty"`

	// +optional
	FailureReason string `json:"failureReason,omitempty"`

	// ValidationErrors is a slice of validation errors during restore
	// +optional
	// +nullable
	ValidationErrors []string `json:"validationErrors,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +genclient:skipVerbs=update,patch
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// Restore is the Schema for the restores API
type Restore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec RestoreSpec `json:"spec,omitempty"`
	// +optional
	Status RestoreStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RestoreList contains a list of Restore
type RestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Restore `json:"items"`
}
