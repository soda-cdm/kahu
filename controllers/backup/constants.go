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
	"time"
)

const (
	controllerName           = "backup-controller"
	backupFinalizer          = "kahu.io/backup-protection"
	defaultReconcileTimeLoop = 5 * time.Second
	defaultReSyncTimeLoop    = 5 * time.Minute

	backupCacheNamespaceIndex             = "backup-cache-namespace-index"
	backupCacheResourceIndex              = "backup-cache-resource-index"
	backupCacheObjectClusterResourceIndex = "backup-cache-cluster-resource-index"

	volumeContentBackupLabel       = "kahu.io/backup-name"
	volumeContentVolumeProvider    = "kahu.io/backup-provider"
	annVolumeBackupDeleteCompleted = "kahu.io/volume-backup-delete-completed"
	annVolumeBackupCompleted       = "kahu.io/volume-backup-completed"
	annVolumeBackupFailHooks       = "kahu.io/volume-backup-fail-hooks-completed"
	annBackupContentSynced         = "kahu.io/backup-content-sync-completed"
	annBackupCleanupDone           = "kahu.io/backup-cleanup-done"
)

const (
	EventValidationSuccess     = "ValidationSuccessful"
	EventPreHookFailed         = "PreHookFailed"
	EventPostHookFailed        = "PostHookFailed"
	EventVolumeSnapshotSuccess = "VolumeSnapshotSuccess"
	EventVolumeSnapshotFailed  = "VolumeSnapshotFailed"
	EventVolumeBackupSuccess   = "VolumeBackupSuccess"
	EventVolumeBackupFailed    = "VolumeBackupFailed"
	EventVolumeBackupScheduled = "VolumeBackupScheduled"
	EventResourceBackupSuccess = "ResourceBackupSuccess"
	EventResourceBackupFailed  = "ResourceBackupFailed"
)

const (
	PVCKind   = "PersistentVolumeClaim"
	PVKind    = "PersistentVolume"
	NodeKind  = "Node"
	EventKind = "Event"
)
