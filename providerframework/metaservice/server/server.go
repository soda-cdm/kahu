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
	"bufio"
	"bytes"
	"context"
	"io"
	"os"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/soda-cdm/kahu/providerframework/metaservice/app/options"
	"github.com/soda-cdm/kahu/providerframework/metaservice/archiver"
	"github.com/soda-cdm/kahu/providerframework/metaservice/archiver/manager"
	backuprepo "github.com/soda-cdm/kahu/providerframework/metaservice/backuprespository"
	pb "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"github.com/soda-cdm/kahu/utils"
)

const (
	archiveFileFormat = ".tar"
)

type metaServer struct {
	ctx            context.Context
	options        options.MetaServiceOptions
	archiveManager archiver.ArchivalManager
	backupRepo     backuprepo.BackupRepository
}

func NewMetaServiceServer(ctx context.Context,
	serviceOptions options.MetaServiceOptions,
	backupRepo backuprepo.BackupRepository) pb.MetaServiceServer {
	archiveManager := manager.NewArchiveManager(serviceOptions.ArchivalYard)
	return &metaServer{
		ctx:            ctx,
		options:        serviceOptions,
		archiveManager: archiveManager,
		backupRepo:     backupRepo,
	}
}

func (server *metaServer) Backup(service pb.MetaService_BackupServer) error {
	log.Info("Backup Called .... ")

	backupHandle, err := getBackupHandle(service)
	if err != nil {
		return status.Errorf(codes.Unknown, "failed to get backup handle during backup")
	}

	archiveFileName := backupHandle + archiveFileFormat

	archiveHandler, archiveFile, err := server.archiveManager.
		GetArchiver(archiver.CompressionType(server.options.CompressionFormat),
			archiveFileName)
	if archiveHandler == nil || err != nil {
		log.Errorf("failed to create archiver %s", err)
		return status.Errorf(codes.Internal, "failed to create archiver %s", err)
	}
	// delete the created /tmp file, incase any issues occured
	defer deleteFile(archiveFile)
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
		log.Infof("Resource Info %+v", resource)
		resourceData := backupRequest.GetBackupResource().GetData()
		filePath := utils.ResourceToFile(backupHandle, resource)
		err = archiveHandler.WriteFile(filePath, resourceData)
		if err != nil {
			log.Errorf("failed to write file. %s", err)
			return status.Errorf(codes.Internal, "failed to write file. %s", err)
		}
	}

	err = archiveHandler.Close()
	if err != nil {
		return status.Errorf(codes.Internal, "failed to close and flush file. %s", err)
	}

	// upload backup to backup-location
	err = server.backupRepo.Upload(archiveFile)
	if err != nil {
		log.Errorf("failed to upload backup. %s", err)
		return status.Errorf(codes.Internal, "failed to upload backup. %s", err)
	}

	err = service.SendAndClose(&pb.Empty{})
	if err != nil {
		return status.Errorf(codes.Unknown, "failed to close and flush file. %s", err)
	}

	return nil
}

func (server *metaServer) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.Empty, error) {
	empty := &pb.Empty{}
	log.Info("Delete backup Called .... ")
	backupHandle := req.GetId().BackupHandle
	parameters := req.GetId().Parameters
	err := server.backupRepo.Delete(backupHandle+archiveFileFormat, parameters)
	if err != nil {
		log.Errorf("failed to delete backup. %s", err)
		return empty, status.Errorf(codes.Internal, "failed to delete backup. %s", err)
	}

	return empty, err
}

func getBackupHandle(service pb.MetaService_BackupServer) (string, error) {
	backupRequest, err := service.Recv()
	if err != nil {
		return "", status.Errorf(codes.Unknown, "failed with error %s", err)
	}

	identifier := backupRequest.GetIdentifier()
	if identifier == nil {
		return "", status.Errorf(codes.InvalidArgument, "first request is not backup identifier")
	}

	// use backup handle name for file
	backupHandle := identifier.GetBackupHandle()
	return backupHandle, nil
}

func deleteFile(filePath string) {
	err := os.Remove(filePath)
	if err != nil {
		log.Warningf("Failed to delete file. %s", err)
	}
}

func (server *metaServer) Restore(req *pb.RestoreRequest,
	service pb.MetaService_RestoreServer) error {
	log.Infof("Restore Called with request %+v", req)

	archiveFileName := req.GetId().GetBackupHandle() + archiveFileFormat

	// download backup file
	filePath, err := server.backupRepo.Download(archiveFileName, req.GetId().GetParameters())
	if err != nil {
		log.Errorf("failed to upload backup. %s", err)
		return status.Errorf(codes.Internal, "failed to upload backup. %s", err)
	}
	defer deleteFile(filePath)

	archiveReader, err := server.archiveManager.
		GetArchiveReader(archiver.CompressionType(server.options.CompressionFormat),
			filePath)
	if archiveReader == nil || err != nil {
		log.Errorf("failed to create archive reader %s", err)
		return status.Errorf(codes.Internal, "failed to create archive reader. %s", err)
	}

	var buffer bytes.Buffer
	for {
		header, reader, err := archiveReader.ReadNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "failed to reader from archived file. %s", err)
		}

		writer := bufio.NewWriter(&buffer)
		_, err = io.CopyN(writer, reader, header.Size)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to form archived file. %s", err)
		}

		err = service.Send(&pb.GetResponse{
			Restore: &pb.GetResponse_BackupResource{
				BackupResource: &pb.BackupResource{
					Resource: utils.FileToResource(header.Name),
					Data:     buffer.Bytes(),
				},
			},
		})
		if err != nil {
			return status.Errorf(codes.Internal, "failed to send back info from archived file. %s", err)
		}
		buffer.Reset()
	}

	return nil
}
