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

package utils

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ContainsAnnotation checks for Annotation existence.
func ContainsAnnotation(meta metav1.Object, annotation string) bool {
	if meta == nil {
		return false
	}
	for a, _ := range meta.GetAnnotations() {
		if a == annotation {
			return true
		}
	}
	return false
}

// GetAnnotation checks and return for annotation value.
func GetAnnotation(meta metav1.Object, annotation string) (string, bool) {
	if meta == nil {
		return "", false
	}
	for a, value := range meta.GetAnnotations() {
		if a == annotation {
			return value, true
		}
	}
	return "", false
}

// SetAnnotation append Annotation if not exists.
func SetAnnotation(meta metav1.Object, annotation, value string) {
	if meta == nil {
		return
	}

	if !ContainsAnnotation(meta, annotation) {
		annotations := meta.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string, 0)
		}
		annotations[annotation] = value
		meta.SetAnnotations(annotations)
	}
}

// RemoveAnnotation removes annotation if it exists.
func RemoveAnnotation(meta metav1.Object, annotation string) {
	if meta == nil {
		return
	}

	newAnnotations := meta.GetAnnotations()
	delete(newAnnotations, annotation)
	meta.SetAnnotations(newAnnotations)
}
