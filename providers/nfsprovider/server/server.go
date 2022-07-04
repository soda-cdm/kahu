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
	"io"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/providers/nfsprovider/server/options"
)

const (
	defaultProviderName        = "kahu-nfs-provider"
	defaultProviderVersion     = "v1"
	READ_BUFFER_SIZE       int = 4096
)

type nfsServer struct {
	ctx     context.Context
	options options.NFSProviderOptions
}

func NewMetaBackupServer(ctx context.Context,
	serviceOptions options.NFSProviderOptions) pb.MetaBackupServer {
	return &nfsServer{
		ctx:     ctx,
		options: serviceOptions,
	}
}

func NewIdentityServer(ctx context.Context,
	serviceOptions options.NFSProviderOptions) pb.IdentityServer {
	return &nfsServer{
		ctx:     ctx,
		options: serviceOptions,
	}
}

// GetProviderInfo returns the basic information from provider side
func (server *nfsServer) GetProviderInfo(ctx context.Context, GetProviderInfoRequest *pb.GetProviderInfoRequest) (*pb.GetProviderInfoResponse, error) {
	log.Info("GetProviderInfo Called .... ")
	response := &pb.GetProviderInfoResponse{
		Provider: defaultProviderName,
		Version:  defaultProviderVersion}

	return response, nil
}

// GetProviderCapabilities returns the capabilities supported by provider
func (server *nfsServer) GetProviderCapabilities(ctx context.Context, GetProviderCapabilitiesRequest *pb.GetProviderCapabilitiesRequest) (*pb.GetProviderCapabilitiesResponse, error) {
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
	return nil, status.Errorf(codes.Unimplemented, "method Probe not implemented")
}

// Upload pushes the input data to the specified location at provider
func (server *nfsServer) Upload(service pb.MetaBackup_UploadServer) error {
	log.Info("Upload Called .... ")

	uploadRequest, err := service.Recv()
	if err != nil {
		return status.Errorf(codes.Unknown, "failed with error %s", err)
	}

	fileInfo := uploadRequest.GetInfo()
	if fileInfo == nil {
		return status.Errorf(codes.InvalidArgument, "first request is not upload file info")
	}

	// use backup handle name for file
	fileId := fileInfo.GetFileIdentifier()
	if fileId == "" {
		log.Errorf("failed to create archiver %s", err)
		return status.Errorf(codes.Internal, "invalid file identifier %s", err)
	}
	// TODO check if filename already exists
	fileName := server.options.DataPath + "/" + fileId
	file, err := os.Create(fileName)
	if err != nil {
		log.Errorf("failed to open file for upload to NFS: %s", err)
		return status.Errorf(codes.Internal, "invalid file identifier %s", err)
	}

	for {
		uploadRequest, err := service.Recv()
		// If there are no more requests
		if err == io.EOF {
			file.Close()
			break
		}
		if err != nil {
			log.Errorf("uploadRequest received error %s", err)
			file.Close()
			return status.Errorf(codes.Unknown, "error receiving request")
		}

		_, err = file.Write(uploadRequest.GetChunkData())
		if err != nil {
			file.Close()
			return status.Errorf(codes.Unknown, "failed to write to file %s", err)
		}
	}

	err = service.SendAndClose(&pb.Empty{})
	if err != nil {
		return status.Errorf(codes.Unknown, "failed to close and flush file. %s", err)
	}

	return nil
}

// Download pulls the input file from the specified location at provider
func (server *nfsServer) Download(request *pb.DownloadRequest,
	service pb.MetaBackup_DownloadServer) error {
	log.Info("Download Called ...")

	fileId := request.GetFileIdentifier()
	if fileId == "" {
		return status.Errorf(codes.InvalidArgument, "download file id is empty")
	}

	log.Printf("Download file id %v", fileId)
	fileName := filepath.Join(server.options.DataPath, fileId)
	file, err := os.Open(fileName)
	if err != nil {
		log.Errorf("failed to open file for download from NFS: %s", err)
		return status.Errorf(codes.Internal, "file not found %s", err)
	}
	defer file.Close()

	buffer := make([]byte, READ_BUFFER_SIZE)

	fi := pb.DownloadResponse_FileInfo{FileIdentifier: fileId}
	fid_data := pb.DownloadResponse{
		Data: &pb.DownloadResponse_Info{Info: &fi},
	}

	// First, send file identifier
	err = service.Send(&fid_data)
	if err != nil {
		log.Errorf("download response got error %s", err)
		return status.Errorf(codes.Unknown, "error sending response")
	}

	size := 0
	// Second, send backup content in loop till file end
	for {
		n, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Errorf("failed to read data from file %s", err)
			return status.Errorf(codes.Unknown, "error sending response")
		}

		size += n
		data := pb.DownloadResponse{Data: &pb.DownloadResponse_ChunkData{ChunkData: buffer[:n]}}
		err = service.Send(&data)
		if err != nil {
			log.Errorf("download response got error %s", err)
			return status.Errorf(codes.Unknown, "error sending response")
		}
	}

	log.Infof("Download success!. size %d", size)

	return nil
}

// Delete removes the input file from the specified location at provider
func (server *nfsServer) Delete(ctxt context.Context,
	request *pb.DeleteRequest) (*pb.Empty, error) {
	log.Info("Delete Called ...")

	fileId := request.GetFileIdentifier()
	empty := pb.Empty{}
	log.Printf("file to delete %v", fileId)
	fileName := server.options.DataPath + "/" + fileId

	// Try opening the file for Delete
	file, err := os.Open(fileName)
	if err != nil {
		log.Errorf("failed to open file for delete from NFS: %s", err)
		return &empty, status.Errorf(codes.Internal, "file not found %s", err)
	}
	file.Close()

	err = os.Remove(fileName)
	if err != nil {
		log.Errorf("failed to delete file from NFS: %s", err)
		return &empty, status.Errorf(codes.Internal, "file not found %s", err)
	}

	return &empty, nil
}

// ObjectExists checks if input file exists at provider
func (server *nfsServer) ObjectExists(ctxt context.Context,
	request *pb.ObjectExistsRequest) (*pb.ObjectExistsResponse, error) {
	log.Info("ObjectExists Called...")

	fileId := request.GetFileIdentifier()
	log.Printf("checking existence for file_id: %v", fileId)
	fileName := server.options.DataPath + "/" + fileId

	// Try opening the file for checking existence
	file, err := os.Open(fileName)
	if err != nil {
		log.Infof("failed to open file while checking existence: %s", err)
		response := pb.ObjectExistsResponse{Exists: false}
		return &response, nil
	}
	file.Close()

	response := pb.ObjectExistsResponse{Exists: true}
	return &response, nil
}
