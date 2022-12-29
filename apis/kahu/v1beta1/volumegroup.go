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
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// VolumeGroup groups the volume based on volume providers
type VolumeGroup struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec VolumeGroupSpec `json:"spec,omitempty" protobuf:"bytes,3,opt,name=spec"`

	// +optional
	Status VolumeGroupStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// Volumes information source
// Only one source is allowed to set
type VolumeSource struct {
	List []ResourceReference `json:"list,omitempty" protobuf:"bytes,1,opt,name=list"`

	Selector *ResourceSelector `json:"selector,omitempty" protobuf:"bytes,1,opt,name=selector"`
}

// VolumeGroupSpec describe the volume information of the group.
type VolumeGroupSpec struct {
	Backup *ResourceReference `json:"backup,omitempty" protobuf:"bytes,1,opt,name=backup"`

	VolumeSource `json:",inline" protobuf:"bytes,2,opt,name=volumes"`
}

// VolumeGroupStatus describe the volume information of the group.
type VolumeGroupStatus struct {
	Volumes []ResourceReference `json:"volumes,omitempty" protobuf:"bytes,1,opt,name=volumes"`

	ReadyToUse *bool `json:"readyToUse,omitempty" protobuf:"bytes,1,opt,name=readyToUse"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type VolumeGroupList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// +optional
	Items []VolumeGroup `json:"items" protobuf:"bytes,1,opt,name=items"`
}
