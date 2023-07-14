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

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName="pr"
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.providerName`
// +kubebuilder:printcolumn:name="Active",type=string,JSONPath=`.status.active`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ProviderRegistration struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec RegistrationSpec `json:"spec,omitempty"`

	// +optional
	Status RegistrationStatus `json:"status,omitempty"`
}

type RegistrationStatus struct {
	// Active status indicate state of the Plugin.
	Active bool `json:"active,omitempty"`
}

// Plugin specifications
type RegistrationSpec struct {
	// +required
	ProviderName *string `json:"providerName,omitempty"`

	// +required
	// +kubebuilder:default=ResourceBackup
	ProviderType RegistrationType `json:"providerType,omitempty"`

	// +required
	Version *string `json:"version,omitempty"`

	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`

	// Capabilities is the optional set of provider capabilities
	// +optional
	Flags []Support `json:"flags,omitempty"`

	// +optional
	SupportedProvisioner *string `json:"supportedProvisioner,omitempty"`

	// +required
	Template *v1.PodTemplateSpec `json:"template,omitempty"`

	// +optional
	Lifecycle `json:"lifecycle,omitempty"`
}

// +kubebuilder:validation:Enum=VOLUME_BACKUP_NEED_SNAPSHOT;VOLUME_BACKUP_NEED_VOLUME
type Support string

const (
	VolumeBackupNeedSnapshotSupport Support = "VOLUME_BACKUP_NEED_SNAPSHOT"
	VolumeBackupNeedVolumeSupport   Support = "VOLUME_BACKUP_NEED_VOLUME"
)

// +kubebuilder:validation:Enum=ResourceBackup;VolumeBackup
type RegistrationType string

const (
	// Backup location
	ResourceBackup RegistrationType = "ResourceBackup"

	// Volume backup/restore
	VolumeBackup RegistrationType = "VolumeBackup"
)

type WorkloadKind string

const (
	PodWorkloadKind        WorkloadKind = "Pod"
	DeploymentWorkloadKind WorkloadKind = "Deployment"
)

type Lifecycle struct {
	// +kubebuilder:validation:Enum=Pod;Deployment
	// +kubebuilder:default=Deployment
	Kind WorkloadKind `json:"kind,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ProviderRegistrationList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// +optional
	Items []ProviderRegistration `json:"items"`
}
