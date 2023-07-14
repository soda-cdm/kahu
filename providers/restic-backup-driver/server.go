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

// Package server implements Restic provider service interfaces
package restic

import (
	"context"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"

	pb "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/providers/restic-backup-driver/config"
)

type volBackupServer struct {
	ctx        context.Context
	config     config.Config
	kubeClient kubernetes.Interface
}

// NewVolumeBackupServer creates a new volume backup service
func NewVolumeBackupServer(ctx context.Context,
	config config.Config) (pb.VolumeBackupServer, error) {
	return &volBackupServer{
		ctx:        ctx,
		config:     config,
		kubeClient: config.GetKubeClient(),
	}, nil
}

// NewIdentityServer creates a new Identify service
func NewIdentityServer(ctx context.Context,
	config config.Config) pb.IdentityServer {
	return &volBackupServer{
		ctx:    ctx,
		config: config,
	}
}

// GetProviderInfo returns the basic information from provider side
func (server *volBackupServer) GetProviderInfo(
	ctx context.Context,
	GetProviderInfoRequest *pb.GetProviderInfoRequest) (*pb.GetProviderInfoResponse, error) {
	log.Info("GetProviderInfo Called .... ")
	response := &pb.GetProviderInfoResponse{
		Provider: server.config.GetProviderName(),
		Version:  server.config.GetProviderVersion()}

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
	return &pb.CreateVolumeFromBackupResponse{
		// VolumeIdentifiers: restoreIDs,
	}, nil
}

// Cancel given restore
func (server *volBackupServer) CancelRestore(context.Context, *pb.CancelRestoreRequest) (*pb.CancelRestoreResponse, error) {
	return &pb.CancelRestoreResponse{}, nil
}

// Get restore statistics
func (server *volBackupServer) GetRestoreStat(ctx context.Context, req *pb.GetRestoreStatRequest) (*pb.GetRestoreStatResponse, error) {
	restoreStats := make([]*pb.RestoreStat, 0)
	// for _, volID := range req.RestoreVolumeIdentity {
	// 	restoreStats = append(restoreStats, &pb.RestoreStat{
	// 		RestoreVolumeHandle: volID.VolumeHandle,
	// 		Progress:            100,
	// 	})
	// }

	return &pb.GetRestoreStatResponse{
		RestoreVolumeStat: restoreStats,
	}, nil
}
