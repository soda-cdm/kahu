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

package restore

import (
	"time"

	"github.com/soda-cdm/kahu/utils"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	controllerName                   = "restore-controller"
	defaultReconcileTimeLoop         = 5 * time.Second
	backupObjectNamespaceIndex       = "backupObject-namespace-index"
	backupObjectResourceIndex        = "backupObject-resource-index"
	backupObjectClusterResourceIndex = "backupObject-cluster-resource-index"

	restoreFinalizer                = "kahu.io/restore-protection"
	annVolumeRestoreCompleted       = "kahu.io/volume-restore-completed"
	annVolumeRestoreDeleteCompleted = "kahu.io/volume-restore-delete-completed"
	annVolumeResourceCleanup        = "kahu.io/volume-resource-cleanup"
	annRestoreIdentifier            = "kahu.io/restore-name"
	annRestoreCleanupDone           = "kahu.io/restore-cleanup-done"
)

const (
	crdName = "CustomResourceDefinition"
)

var (
	excludeRestoreResources = sets.NewString(
		"CustomResourceDefinition",
		"VolumeBackupContent",
		utils.PV,  // Volume all ready restored
		utils.PVC, // Volume all ready restored
	)
)

var (
	excludeResources = sets.NewString(
		"Node",
		"Namespace",
		"Event",
	)
)
