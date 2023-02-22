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

package k8sresource

import (
	"github.com/soda-cdm/kahu/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Resource struct {
	unstructured.Unstructured
}

var _ metav1.Object = &Resource{}
var _ runtime.Unstructured = &Resource{}
var _ metav1.ListInterface = &Resource{}
var _ schema.ObjectKind = &Resource{}

// FromResource converts an object from Resource representation into a concrete type.
func FromResource(resource Resource, obj interface{}) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, obj)
}

// ToUnstructured converts an object into Resource representation.
func ToResource(obj interface{}) (Resource, error) {
	unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	return Resource{
		Unstructured: unstructured.Unstructured{
			Object: unstructuredMap,
		},
	}, err
}

func (resource *Resource) ResourceID() string {
	return utils.ToResourceID(resource.GetAPIVersion(),
		resource.GetKind(),
		resource.GetNamespace(),
		resource.GetName())
}

func (resource *Resource) DeepCopy() Resource {
	clone := resource.Unstructured.DeepCopy()
	return Resource{
		Unstructured: unstructured.Unstructured{
			Object: clone.Object,
		},
	}
}

type ResourceReference struct {
	schema.GroupVersionKind `json:",inline"`
	Namespace               string `json:"namespace,omitempty"`
	Name                    string `json:"name,omitempty"`
}
