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

// ContainsOwnerReference checks for owner reference existence.
func ContainsOwnerReference(meta metav1.Object) bool {
	return len(meta.GetOwnerReferences()) != 0
}

func AddOwnerReference(meta metav1.Object, ref metav1.OwnerReference) {
	refs := meta.GetOwnerReferences()
	refs = append(refs, ref)
	meta.SetOwnerReferences(refs)
}

// GetOwnerReference checks and return for OwnerReference value.
func GetOwnerReference(meta metav1.Object, UID string) (metav1.OwnerReference, bool) {
	if meta == nil {
		return metav1.OwnerReference{}, false
	}
	for _, ref := range meta.GetOwnerReferences() {
		if string(ref.UID) == UID {
			return ref, true
		}
	}
	return metav1.OwnerReference{}, false
}

func GetOwnerReferences(meta metav1.Object) []metav1.OwnerReference {
	return meta.GetOwnerReferences()
}

func GetOwnerRefByKind(meta metav1.Object, kind string) []metav1.OwnerReference {
	if meta == nil {
		return nil
	}

	refs := make([]metav1.OwnerReference, 0)
	for _, ref := range meta.GetOwnerReferences() {
		if ref.Kind == kind {
			refs = append(refs, ref)
		}
	}

	return refs
}

// RemoveOwnerReference removes OwnerReference if it exists.
func RemoveOwnerReference(meta metav1.Object, UID string) {
	if meta == nil {
		return
	}

	refs := meta.GetOwnerReferences()
	newRefs := make([]metav1.OwnerReference, 0)
	for _, ref := range refs {
		if string(ref.UID) == UID {
			continue
		}
	}

	meta.SetOwnerReferences(newRefs)
}
