package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackupLocationSpec defines the desired state of BackupLocation
type BackupLocationSpec struct {
	ProviderName string            `json:"providerName,omitempty"`
	Config       map[string]string `json:"config,omitempty"`
}

// BackupLocation is the Schema for the backuplocations API
type BackupLocation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec BackupLocationSpec `json:"spec,omitempty"`
}

// BackupLocationList contains a list of BackupLocation
type BackupLocationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// +optional
	Items []BackupLocation `json:"items"`
}
