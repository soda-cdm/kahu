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
	"sync"

	appv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1api "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
)

const (
	kindClusterRole              = "ClusterRole"
	kindClusterRoleBinding       = "ClusterRoleBinding"
	kindConfigmap                = "ConfigMap"
	kindCronJob                  = "CronJob"
	kindNamespace                = "Namespace"
	kindDaemonSet                = "DaemonSet"
	kindEndpoints                = "Endpoints"
	kindDeployment               = "Deployment"
	kindJob                      = "Job"
	kindPersistentVolume         = "PersistentVolume"
	kindPersistentVolumeClaim    = "PersistentVolumeClaim"
	kindPod                      = "Pod"
	KindReplicaSet               = "ReplicaSet"
	kindService                  = "Service"
	kindStatefulSet              = "StatefulSet"
	kindServiceAccount           = "ServiceAccount"
	kindSecret                   = "Secret"
	kindRoleBinding              = "RoleBinding"
	kindRole                     = "Role"
	kindBackup                   = "Backup"
	kindRestore                  = "Restore"
	kindKahuVolumeSnapshot       = "VolumeSnapshot"
	kindKahuVolumeGroup          = "VolumeGroup"
	kindKahuProviderRegistration = "ProviderRegistration"
)

var kindSync sync.Once

// Type meta constants
var ClusterRoleGVK schema.GroupVersionKind
var ClusterRoleBindingGVK schema.GroupVersionKind
var ConfigmapGVK schema.GroupVersionKind
var CronJobGVK schema.GroupVersionKind
var NamespaceGVK schema.GroupVersionKind
var DaemonSetGVK schema.GroupVersionKind
var DeploymentGVK schema.GroupVersionKind
var EndpointsGVK schema.GroupVersionKind
var JobGVK schema.GroupVersionKind
var PersistentVolumeGVK schema.GroupVersionKind
var PersistentVolumeClaimGVK schema.GroupVersionKind
var PodGVK schema.GroupVersionKind
var ReplicaSetGVK schema.GroupVersionKind
var ServiceGVK schema.GroupVersionKind
var StatefulSetGVK schema.GroupVersionKind
var ServiceAccountGVK schema.GroupVersionKind
var SecretGVK schema.GroupVersionKind
var RoleBindingGVK schema.GroupVersionKind
var RoleGVK schema.GroupVersionKind
var BackupGVK schema.GroupVersionKind
var RestoreGVK schema.GroupVersionKind
var KahuVolumeSnapshotGVK schema.GroupVersionKind
var KahuVolumeGroupGVK schema.GroupVersionKind
var KahuProviderRegistrationGVK schema.GroupVersionKind

func init() {
	kindSync.Do(func() {
		ClusterRoleGVK = rbacv1.SchemeGroupVersion.WithKind(kindClusterRole)
		ClusterRoleBindingGVK = rbacv1.SchemeGroupVersion.WithKind(kindClusterRoleBinding)
		ConfigmapGVK = corev1api.SchemeGroupVersion.WithKind(kindConfigmap)
		CronJobGVK = batchv1.SchemeGroupVersion.WithKind(kindCronJob)
		NamespaceGVK = corev1api.SchemeGroupVersion.WithKind(kindNamespace)
		DaemonSetGVK = appv1.SchemeGroupVersion.WithKind(kindDaemonSet)
		DeploymentGVK = appv1.SchemeGroupVersion.WithKind(kindDeployment)
		EndpointsGVK = corev1api.SchemeGroupVersion.WithKind(kindEndpoints)
		JobGVK = batchv1.SchemeGroupVersion.WithKind(kindJob)
		PersistentVolumeGVK = corev1api.SchemeGroupVersion.WithKind(kindPersistentVolume)
		PersistentVolumeClaimGVK = corev1api.SchemeGroupVersion.WithKind(kindPersistentVolumeClaim)
		PodGVK = corev1api.SchemeGroupVersion.WithKind(kindPod)
		ReplicaSetGVK = appv1.SchemeGroupVersion.WithKind(KindReplicaSet)
		StatefulSetGVK = appv1.SchemeGroupVersion.WithKind(kindStatefulSet)
		SecretGVK = corev1api.SchemeGroupVersion.WithKind(kindSecret)
		ServiceGVK = corev1api.SchemeGroupVersion.WithKind(kindService)
		ServiceAccountGVK = corev1api.SchemeGroupVersion.WithKind(kindServiceAccount)
		RoleBindingGVK = rbacv1.SchemeGroupVersion.WithKind(kindRoleBinding)
		RoleGVK = rbacv1.SchemeGroupVersion.WithKind(kindRole)
		BackupGVK = kahuapi.SchemeGroupVersion.WithKind(kindBackup)
		RestoreGVK = kahuapi.SchemeGroupVersion.WithKind(kindRestore)
		KahuVolumeSnapshotGVK = kahuapi.SchemeGroupVersion.WithKind(kindKahuVolumeSnapshot)
		KahuVolumeGroupGVK = kahuapi.SchemeGroupVersion.WithKind(kindKahuVolumeGroup)
		KahuProviderRegistrationGVK = kahuapi.SchemeGroupVersion.WithKind(kindKahuProviderRegistration)
	})
}
