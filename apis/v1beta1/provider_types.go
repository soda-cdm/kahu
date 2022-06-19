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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ProviderState is the availability state of Provider.
// +kubebuilder:validation:Enum=Available;Unavailable
// +kubebuilder:default=Unavailable
type ProviderState string

const (
	// ProviderStateAvailable means the provider is available for use
	ProviderStateAvailable ProviderState = "Available"

	// ProviderStateUnavailable means the provider is unavailable for use
	ProviderStateUnavailable ProviderState = "Unavailable"
)

// ProviderType is the type of Provider.
// +kubebuilder:validation:Enum=MetadataProvider;VolumeProvider
// +kubebuilder:default=MetadataProvider
type ProviderType string

const (
	// ProviderTypeMetadata means the metadata provider
	ProviderTypeMetadata ProviderType = "MetadataProvider"

	// ProviderTypeVolume means the volume provider
	ProviderTypeVolume ProviderType = "VolumeProvider"
)

// +genclient

// ProviderSpec defines the specification of provider CRD
type ProviderSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Provider is name of the provider getting registered
	Provider string `json:"provider,omitempty"`

	// Type is type of the provider getting registered
	Type ProviderType `json:"type,omitempty"`

	// Version is version of the provider getting registered
	Version string `json:"version,omitempty"`

	// Manifest is the optional set of provider specific configurations
	// +optional
	Manifest map[string]string `json:"manifest,omitempty"`
}

// ProviderStatus defines the observed state of Provider
type ProviderStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	State ProviderState `json:"state,omitempty"`
}

//+genclient
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Provider is the Schema for the Provider
type Provider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderSpec   `json:"spec,omitempty"`
	Status ProviderStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ProviderList contains a list of Provider
type ProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Provider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Provider{}, &ProviderList{})
}
