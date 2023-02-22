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

// Package server implements NFS provider service interfaces
package server

import (
	"context"
	"fmt"
	"github.com/soda-cdm/kahu/providers/nfs/archiver/tar"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/providers/nfs/server/options"
)

const (
	defaultProviderName    = "nfs-provider"
	defaultProviderVersion = "v1"
	fileExtension          = ".tar"
)

type nfsServer struct {
	ctx     context.Context
	options options.NFSProviderOptions
	logger  log.FieldLogger
}

// NewMetaBackupServer creates a new Meta backup service
func NewNFSServer(ctx context.Context,
	serviceOptions options.NFSProviderOptions) *nfsServer {
	return &nfsServer{
		ctx:     ctx,
		options: serviceOptions,
		logger:  log.WithField("module", "nfs-server"),
	}
}

// GetProviderInfo returns the basic information from provider side
func (server *nfsServer) GetProviderInfo(
	_ context.Context,
	_ *pb.GetProviderInfoRequest) (*pb.GetProviderInfoResponse, error) {
	log.Info("GetProviderInfo Called .... ")
	response := &pb.GetProviderInfoResponse{
		Provider: defaultProviderName,
		Version:  defaultProviderVersion}

	return response, nil
}

// GetProviderCapabilities returns the capabilities supported by provider
func (server *nfsServer) GetProviderCapabilities(
	ctx context.Context,
	GetProviderCapabilitiesRequest *pb.GetProviderCapabilitiesRequest) (*pb.GetProviderCapabilitiesResponse,
	error) {
	log.Info("GetProviderCapabilities Called .... ")
	return &pb.GetProviderCapabilitiesResponse{
		Capabilities: []*pb.ProviderCapability{
			{
				Type: &pb.ProviderCapability_Service_{
					Service: &pb.ProviderCapability_Service{
						Type: pb.ProviderCapability_Service_META_BACKUP_SERVICE,
					},
				},
			},
		},
	}, nil
}

// Probe checks the healthy/availability state of the provider
func (server *nfsServer) Probe(ctx context.Context, probeRequest *pb.ProbeRequest) (*pb.ProbeResponse, error) {
	log.Infof("Probe invoked of %v, request: %v", *server, probeRequest)
	return &pb.ProbeResponse{}, nil
}

func (server *nfsServer) filePath(fileName string) string {
	return filepath.Join(server.options.DataPath, fileName)
}

// Upload pushes the input data to the specified location at provider
func (server *nfsServer) Upload(service pb.MetaBackup_UploadServer) error {
	uploadRequest, err := service.Recv()
	if err != nil {
		return status.Error(codes.Unknown, "upload request failed")
	}

	bucket := uploadRequest.GetBucket()
	if bucket == nil {
		return status.Error(codes.Aborted, "first upload request doest not have bucket info")
	}

	server.logger.Infof("Upload request for %s", bucket)

	tarFilePath := tarFile(server.filePath(bucket.Handle))
	archiver, err := tar.NewArchiveWriter(tarFilePath)
	if err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("failed to initialize file archiver. %s", err))
	}
	defer func() {
		err := archiver.Close()
		if err != nil {
			server.logger.Errorf("failed to close archive file")
		}
	}()

	for {
		uploadRequest, err = service.Recv()
		// If there are no more requests
		if err == io.EOF {
			break
		}
		if err != nil {
			server.logger.Errorf("uploadRequest received error %s", err)
			return status.Error(codes.Unknown, "error receiving request")
		}
		object := uploadRequest.GetObject()
		if object == nil {
			return status.Error(codes.Aborted, "invalid request. Object request expected")
		}

		err = archiver.WriteFile(object.GetIdentifier(), object.GetData())
		if err != nil {
			return status.Error(codes.Unknown, "failed to write to file")
		}
	}

	err = service.SendAndClose(&pb.Empty{})
	if err != nil {
		return status.Error(codes.Unknown, "failed to close and flush file.")
	}

	return nil
}

// Download pulls the input file from the specified location at provider
func (server *nfsServer) Download(request *pb.DownloadRequest,
	service pb.MetaBackup_DownloadServer) error {

	tarFilePath := tarFile(server.filePath(request.GetBucket().Handle))
	server.logger.Infof("Download Called for %s ...", tarFilePath)

	archiver, err := tar.NewArchiveReader(tarFilePath)
	if err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("failed to initialize file archiver. %s", err))
	}
	defer func() {
		err := archiver.Close()
		if err != nil {
			server.logger.Errorf("failed to close archive file")
		}
	}()

	for {
		header, file, err := archiver.ReadNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			server.logger.Errorf("Error reading archive file. %s", err)
			return status.Error(codes.Unknown, "error reading archive file")
		}

		data, err := ioutil.ReadAll(file)
		if err != nil {
			server.logger.Errorf("Error reading archive file content. %s", err)
			return status.Error(codes.Internal, "error reading archive file content")
		}

		err = service.Send(&pb.DownloadResponse{
			Object: &pb.Object{
				Identifier: header.Name,
				Data:       data,
			},
		})
		if err != nil {
			server.logger.Errorf("Error sending download response. %s", err)
			return status.Errorf(codes.Unknown, "error sending download response %s", err)
		}
	}

	return nil
}

func tarFile(filePath string) string {
	return filePath + fileExtension
}

// Delete removes the input file from the specified location at provider
func (server *nfsServer) Delete(ctxt context.Context,
	request *pb.DeleteRequest) (*pb.Empty, error) {
	bucketHandle := request.GetBucket().Handle
	server.logger.Infof("Delete called for %s...", bucketHandle)
	empty := pb.Empty{}
	tarFilePath := tarFile(server.filePath(request.GetBucket().Handle))
	if _, err := os.Stat(tarFilePath); err != nil {
		if os.IsNotExist(err) {
			return &empty, nil
		}
		return &empty, status.Error(codes.Unknown, "error checking file status")
	}

	err := os.Remove(tarFilePath)
	if err != nil {
		log.Error("failed to delete file from NFS")
		return &empty, status.Error(codes.Internal, "remove, backup file not found")
	}

	return &empty, nil
}

// ObjectExists checks if input file exists at provider
func (server *nfsServer) ObjectExists(ctxt context.Context,
	request *pb.ObjectExistsRequest) (*pb.ObjectExistsResponse, error) {
	tarFilePath := tarFile(server.filePath(request.GetBucket().Handle))

	server.logger.Infof("ObjectExists Called for %s...", tarFilePath)
	if _, err := os.Stat(tarFilePath); err != nil {
		if os.IsNotExist(err) {
			return &pb.ObjectExistsResponse{Exists: true}, nil
		}
		return &pb.ObjectExistsResponse{Exists: false}, status.Error(codes.Unknown, "error checking file status")
	}

	return &pb.ObjectExistsResponse{Exists: true}, nil
}
