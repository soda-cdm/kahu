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

// BackupLocationSpec defines the desired state of BackupLocation
type BackupLocationSpec struct {
	// ProviderName is a 3rd party driver which inernally connect to respective storage
	// +kubebuilder:validation:Required
	// +required
	ProviderName string `json:"providerName"`

	// Config is a dictonary which may contains specific details, like secret key, password etc
	// +optional
	Config map[string]string `json:"config,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +genclient:skipVerbs=update,patch
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope=Cluster

// BackupLocation is the Schema for the backuplocations API
type BackupLocation struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec BackupLocationSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// BackupLocationList contains a list of BackupLocation
type BackupLocationList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// +optional
	Items []BackupLocation `json:"items"`
}
