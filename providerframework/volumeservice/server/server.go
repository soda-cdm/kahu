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

package server

import (
	"context"
	"time"

	"github.com/soda-cdm/kahu/providerframework/volumeservice/app/config"
	"github.com/soda-cdm/kahu/providerframework/volumeservice/backup"

	log "github.com/sirupsen/logrus"
	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/soda-cdm/kahu/client/clientset/versioned"
	volumeservice "github.com/soda-cdm/kahu/providerframework/volumeservice/lib/go"
	providerSvc "github.com/soda-cdm/kahu/providers/lib/go"
)

const (
	controllerName = "volume-backup-server"

	volumeRestoreContentFinalizer = "kahu.io/volume-restore-content-protection"

	defaultContextTimeout         = 30 * time.Minute
	defaultSyncTime               = 5 * time.Minute
	defaultRestoreProgressTimeout = 30 * time.Minute

	EventVolumeRestoreFailed       = "VolumeRestoreFailed"
	EventVolumeRestoreStarted      = "VolumeRestoreStarted"
	EventVolumeRestoreCompleted    = "VolumeRestoreCompleted"
	EventVolumeRestoreCancelFailed = "VolumeRestoreCancelFailed"

	annVolumeResourceCleanup = "kahu.io/volume-resource-cleanup"
)

type volServer struct {
	logger         log.FieldLogger
	providerName   string
	driver         providerSvc.VolumeBackupClient
	driverIdentity providerSvc.IdentityClient
	// operationManager operation.Manager
	kahuClient    versioned.Interface
	backupService *backup.VolumeBackupService
}

func NewServer(config *config.CompletedConfig,
	driver providerSvc.VolumeBackupClient,
	driverIdentity providerSvc.IdentityClient) volumeservice.VolumeServiceServer {
	logger := log.WithField("module", controllerName)

	server := &volServer{
		logger:         logger,
		providerName:   config.Provider,
		driver:         driver,
		driverIdentity: driverIdentity,
		kahuClient:     config.KahuClient,
		backupService:  backup.NewVolumeBackupService(config, driver),
	}

	return server
}

func (server *volServer) Backup(req *volumeservice.BackupRequest,
	stream volumeservice.VolumeService_BackupServer) error {
	// vbcName := req.GetName()
	// vbc, err := server.kahuClient.KahuV1beta1().
	// 	VolumeBackupContents().
	// 	Get(stream.Context(), vbcName, metav1.GetOptions{})
	// if err != nil && !apierrors.IsNotFound(err) {
	// 	server.logger.Errorf("Unable to get VolumeBackupContent[%s]", vbcName)
	// 	return status.Errorf(codes.Aborted, "Unable to get VolumeBackupContent[%s]", vbcName)
	// }

	// if apierrors.IsNotFound(err) {
	// 	server.logger.Errorf("VolumeBackupContent[%s] not available for backup", vbcName)
	// 	return status.Errorf(codes.NotFound, "VolumeBackupContent[%s] not available for backup", vbcName)
	// }

	// check volume backup driver
	// volBackupDriver, ok := checkVolumeBackupDriver(vbc)
	// if !ok {
	// 	server.logger.Infof("Volume backup driver not assigned for vbc[%s]", vbcName)
	// 	return status.Error(codes.Aborted, "Volume backup driver not assigned")
	// }

	// if volBackupDriver != server.providerName {
	// 	server.logger.Infof("Skipping volume backup processing for %s driver", volBackupDriver)
	// 	return status.Errorf(codes.Aborted, "Skipping volume backup processing for %s driver", volBackupDriver)
	// }

	return server.backupService.Backup(stream.Context(), req, stream)
}

func (server *volServer) DeleteBackup(ctx context.Context,
	req *volumeservice.DeleteBackupRequest) (*volumeservice.Empty, error) {
	empty := &volumeservice.Empty{}
	vbcName := req.GetName()
	vbc, err := server.kahuClient.KahuV1beta1().
		VolumeBackupContents().
		Get(ctx, vbcName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		server.logger.Errorf("Unable to get VolumeBackupContent[%s]", vbcName)
		return empty, status.Errorf(codes.Aborted, "Unable to get VolumeBackupContent[%s]", vbcName)
	}
	if apierrors.IsNotFound(err) {
		server.logger.Errorf("VolumeBackupContent[%s] not available for delete", vbcName)
		return empty, nil
	}

	return &volumeservice.Empty{}, server.backupService.DeleteBackup(ctx, vbc)
}

func (*volServer) Restore(*volumeservice.RestoreRequest,
	volumeservice.VolumeService_RestoreServer) error {
	return nil
}

func (*volServer) DeleteRestore(context.Context,
	*volumeservice.DeleteRestoreRequest) (*volumeservice.Empty, error) {
	return &volumeservice.Empty{}, nil
}

// Probe provider for availability check
func (server *volServer) Probe(ctx context.Context,
	_ *volumeservice.ProbeRequest) (*volumeservice.ProbeResponse, error) {
	rsp, err := server.driverIdentity.
		Probe(ctx, &providerSvc.ProbeRequest{})
	if err != nil {
		return &volumeservice.ProbeResponse{}, err
	}

	return &volumeservice.ProbeResponse{Ready: rsp.GetReady()}, nil
}

func checkVolumeBackupDriver(volBackup *kahuapi.VolumeBackupContent) (string, bool) {
	if volBackup.Status.VolumeBackupProvider == nil {
		return "", false
	}

	return *volBackup.Status.VolumeBackupProvider, true
}
