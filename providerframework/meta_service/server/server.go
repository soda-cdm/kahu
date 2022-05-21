// Copyright 2022 The SODA Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"context"
	"io"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/soda-cdm/kahu/providerframework/meta_service/archiver"
	"github.com/soda-cdm/kahu/providerframework/meta_service/archiver/manager"
	pb "github.com/soda-cdm/kahu/providerframework/meta_service/lib/go"
	"github.com/soda-cdm/kahu/providerframework/meta_service/server/options"
	"github.com/soda-cdm/kahu/utils"
)

type metaServer struct {
	ctx            context.Context
	options        options.MetaServiceOptions
	archiveManager archiver.ArchivalManager
}

func NewMetaServiceServer(ctx context.Context,
	serviceOptions options.MetaServiceOptions) pb.MetaServiceServer {
	archiveManager := manager.NewArchiveManager(serviceOptions.ArchivalYard)
	return &metaServer{
		ctx:            ctx,
		options:        serviceOptions,
		archiveManager: archiveManager,
	}
}

func (server *metaServer) Backup(service pb.MetaService_BackupServer) error {
	log.Info("Backup Called .... ")

	backupRequest, err := service.Recv()
	if err != nil {
		return status.Errorf(codes.Unknown, "failed with error %s", err)
	}

	identifier := backupRequest.GetIdentifier()
	if identifier == nil {
		return status.Errorf(codes.InvalidArgument, "first request is not backup identifier")
	}

	// use backup handle name for file
	backupHandle := identifier.GetBackupHandle()
	// TODO: check backup location info

	archiveHandler, err := server.archiveManager.
		GetArchiver(archiver.CompressionType(server.options.CompressionFormat),
			backupHandle)
	if archiveHandler == nil || err != nil {
		log.Errorf("failed to create archiver %s", err)
		return status.Errorf(codes.Internal, "failed to create archiver %s", err)
	}

	for {
		backupRequest, err := service.Recv()
		// If there are no more requests
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Errorf("backupRequest received error %s", err)
			return status.Errorf(codes.Unknown, "error receiving request")
		}

		resource := backupRequest.GetBackupResource().GetResource()
		resourceData := backupRequest.GetBackupResource().GetData()
		err = archiveHandler.WriteFile(utils.ResourceToFile(resource), resourceData)
		if err != nil {
			log.Errorf("failed to write file. %s", err)
			return status.Errorf(codes.Internal, "failed to write file. %s", err)
		}
	}

	err = archiveHandler.Close()
	if err != nil {
		return status.Errorf(codes.Internal, "failed to close and flush file. %s", err)
	}

	err = service.SendAndClose(&pb.Empty{})
	if err != nil {
		return status.Errorf(codes.Unknown, "failed to close and flush file. %s", err)
	}

	return nil
}

func (server *metaServer) Restore(*pb.RestoreRequest,
	pb.MetaService_RestoreServer) error {
	log.Info("Restore Called")
	return nil
}
