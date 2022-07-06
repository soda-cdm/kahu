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

package backup

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	kahuv1beta1 "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
)

var ResultantNamespace = []string{}

var ResultantResource = []string{}

type itemKey struct {
	resource  string
	namespace string
	name      string
}

type PrepareBackup struct {
	*kahuv1beta1.Backup

	StorageLocation *kahuv1beta1.BackupLocation
	BackedUpItems   map[itemKey]struct{}
}

type KubernetesResource struct {
	groupResource         schema.GroupResource
	preferredGVR          schema.GroupVersionResource
	namespace, name, path string
}

type GroupResouceVersion struct {
	resourceName string
	version      string
	group        string
}

const (
	deployments = iota
	pod
	pvc
	pv
	other
)

func CoreGroupResourcePriority(resource string) int {
	switch strings.ToLower(resource) {
	case "deployments":
		return deployments
	case "pods":
		return pod
	case "persistentvolumeclaims":
		return pvc
	case "persistentvolumes":
		return pv
	}

	return other
}
