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

// ContainsFinalizer checks for finalizer existence.
func ContainsFinalizer(meta metav1.Object, finalizer string) bool {
	if meta == nil {
		return false
	}
	for _, f := range meta.GetFinalizers() {
		if f == finalizer {
			return true
		}
	}
	return false
}

// SetFinalizer append finalizer if not exists.
func SetFinalizer(meta metav1.Object, finalizer string) {
	if meta == nil {
		return
	}

	if !ContainsFinalizer(meta, finalizer) {
		meta.SetFinalizers(append(meta.GetFinalizers(), finalizer))
	}
}

// RemoveFinalizer removes a finalizer if it exists.
func RemoveFinalizer(meta metav1.Object, finalizer string) {
	if meta == nil {
		return
	}

	newFinalizers := make([]string, 0)
	for _, f := range meta.GetFinalizers() {
		if f != finalizer {
			newFinalizers = append(newFinalizers, f)
		}
	}
	meta.SetFinalizers(newFinalizers)
}
