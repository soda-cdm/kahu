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

// Package server implements LVM provider service interfaces
package server

import (
	"context"
	"strings"

	snapshotapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	snapshotclientset "github.com/kubernetes-csi/external-snapshotter/client/v4/clientset/versioned"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/client"
	pb "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/providers/openebs-zfs/server/options"
)

type volBackupServer struct {
	ctx         context.Context
	options     options.LVMProviderOptions
	snapshotCli *snapshotclientset.Clientset
	kubeClient  kubernetes.Interface
}

// NewVolumeBackupServer creates a new volume backup service
func NewVolumeBackupServer(ctx context.Context,
	serviceOptions options.LVMProviderOptions) (pb.VolumeBackupServer, error) {
	clientFactory := client.NewFactory("VolumeBackup", serviceOptions.KubeClientOptions.Config)
	config, err := clientFactory.ClientConfig()
	if err != nil {
		return nil, err
	}
	snapshotCli, err := snapshotclientset.NewForConfig(config)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	kubeCli, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &volBackupServer{
		ctx:         ctx,
		options:     serviceOptions,
		snapshotCli: snapshotCli,
		kubeClient:  kubeCli,
	}, nil
}

// NewIdentityServer creates a new Identify service
func NewIdentityServer(ctx context.Context,
	serviceOptions options.LVMProviderOptions) pb.IdentityServer {
	return &volBackupServer{
		ctx:     ctx,
		options: serviceOptions,
	}
}

// GetProviderInfo returns the basic information from provider side
func (server *volBackupServer) GetProviderInfo(
	ctx context.Context,
	GetProviderInfoRequest *pb.GetProviderInfoRequest) (*pb.GetProviderInfoResponse, error) {
	log.Info("GetProviderInfo Called .... ")
	response := &pb.GetProviderInfoResponse{
		Provider: server.options.ProviderName,
		Version:  server.options.ProviderVersion}

	return response, nil
}

// GetProviderCapabilities returns the capabilities supported by provider
func (server *volBackupServer) GetProviderCapabilities(
	_ context.Context,
	_ *pb.GetProviderCapabilitiesRequest) (*pb.GetProviderCapabilitiesResponse,
	error) {
	log.Info("GetProviderCapabilities Called .... ")
	return &pb.GetProviderCapabilitiesResponse{
		Capabilities: []*pb.ProviderCapability{
			{
				Type: &pb.ProviderCapability_Service_{
					Service: &pb.ProviderCapability_Service{
						Type: pb.ProviderCapability_Service_VOLUME_BACKUP_SERVICE,
					},
				},
			},
		},
	}, nil
}

// Probe checks the healthy/availability state of the provider
func (server *volBackupServer) Probe(ctx context.Context, probeRequest *pb.ProbeRequest) (*pb.ProbeResponse, error) {
	return &pb.ProbeResponse{}, nil
}

// Create backup of the provided volumes
func (server *volBackupServer) StartBackup(ctx context.Context, req *pb.StartBackupRequest) (*pb.StartBackupResponse, error) {
	backupIdentifiers := make([]*pb.BackupIdentifier, 0)
	for _, backupInfo := range req.GetBackupInfo() {
		backupIdentifiers = append(backupIdentifiers, &pb.BackupIdentifier{
			PvName: backupInfo.Pv.Name,
			BackupIdentity: &pb.BackupIdentity{
				BackupHandle:     backupInfo.Snapshot.SnapshotHandle,
				BackupAttributes: backupInfo.Snapshot.SnapshotAttributes,
			},
		})
	}

	return &pb.StartBackupResponse{
		BackupInfo: backupIdentifiers,
	}, nil
}

// Delete given backup
func (server *volBackupServer) DeleteBackup(context.Context, *pb.DeleteBackupRequest) (*pb.DeleteBackupResponse, error) {
	return &pb.DeleteBackupResponse{}, nil
}

// Cancel given backup
func (server *volBackupServer) CancelBackup(ctx context.Context, req *pb.CancelBackupRequest) (*pb.CancelBackupResponse, error) {
	res, err := server.DeleteBackup(ctx, &pb.DeleteBackupRequest{
		BackupInfo: req.BackupInfo,
		Parameters: req.Parameters,
	})
	return &pb.CancelBackupResponse{
		Success: res.Success,
		Errors:  res.Errors,
	}, err
}

// Get backup statistics
func (server *volBackupServer) GetBackupStat(ctx context.Context,
	req *pb.GetBackupStatRequest) (*pb.GetBackupStatResponse, error) {
	stat := make([]*pb.BackupStat, 0)
	for _, info := range req.BackupInfo {
		stat = append(stat, &pb.BackupStat{
			BackupHandle: info.BackupHandle,
			Progress:     100,
		})
	}
	return &pb.GetBackupStatResponse{
		BackupStats: stat,
	}, nil
}

// Create volume from backup (for restore)
func (server *volBackupServer) CreateVolumeFromBackup(ctx context.Context,
	restoreReq *pb.CreateVolumeFromBackupRequest) (*pb.CreateVolumeFromBackupResponse, error) {

	restoreIDs := make([]*pb.RestoreVolumeIdentifier, 0)
	for _, restoreInfo := range restoreReq.RestoreInfo {
		backupHandle := restoreInfo.GetBackupIdentity().BackupHandle
		bacjupHandleSplit := strings.Split(backupHandle, "@")
		snapshotHandle := bacjupHandleSplit[1]
		snapshotContentName := strings.ReplaceAll(snapshotHandle, "snapshot", "snapcontent")

		snapshotContent, err := server.snapshotCli.SnapshotV1().
			VolumeSnapshotContents().
			Get(context.TODO(), snapshotContentName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		snapshotRef := snapshotContent.Spec.VolumeSnapshotRef

		pvc := v1.PersistentVolumeClaim{}
		pvc.Name = restoreInfo.Pvc.Name
		pvc.Namespace = restoreInfo.Pvc.Namespace
		pvc.Spec.DataSource = &v1.TypedLocalObjectReference{
			Name:     snapshotRef.Name,
			Kind:     "VolumeSnapshot",
			APIGroup: &snapshotapi.SchemeGroupVersion.Group,
		}
		pvc.Spec.StorageClassName = restoreInfo.Pvc.Spec.StorageClassName
		pvc.Spec.AccessModes = restoreInfo.Pvc.Spec.AccessModes
		pvc.Spec.Resources = restoreInfo.Pvc.Spec.Resources

		k8sPVC, err := server.kubeClient.
			CoreV1().
			PersistentVolumeClaims(snapshotRef.Namespace).
			Create(context.TODO(), &pvc, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}

		restoreIDs = append(restoreIDs, &pb.RestoreVolumeIdentifier{
			PvcName: k8sPVC.Name,
			VolumeIdentity: &pb.RestoreVolumeIdentity{
				VolumeHandle: k8sPVC.Name,
			},
		})
	}

	return &pb.CreateVolumeFromBackupResponse{
		VolumeIdentifiers: restoreIDs,
	}, nil
}

// Cancel given restore
func (server *volBackupServer) CancelRestore(context.Context, *pb.CancelRestoreRequest) (*pb.CancelRestoreResponse, error) {
	return &pb.CancelRestoreResponse{}, nil
}

// Get restore statistics
func (server *volBackupServer) GetRestoreStat(ctx context.Context, req *pb.GetRestoreStatRequest) (*pb.GetRestoreStatResponse, error) {
	restoreStats := make([]*pb.RestoreStat, 0)
	for _, volID := range req.RestoreVolumeIdentity {
		restoreStats = append(restoreStats, &pb.RestoreStat{
			RestoreVolumeHandle: volID.VolumeHandle,
			Progress:            100,
		})
	}

	return &pb.GetRestoreStatResponse{
		RestoreVolumeStat: restoreStats,
	}, nil
}
