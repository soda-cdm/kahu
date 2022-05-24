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
	"os"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/providers/nfs_provider/server/options"
)

type nfsServer struct {
	ctx            context.Context
	options        options.NFSProviderOptions
}

func NewMetaBackupServer(ctx context.Context,
	serviceOptions options.NFSProviderOptions) pb.MetaBackupServer {
	return &nfsServer{
		ctx:            ctx,
		options:        serviceOptions,
	}
}

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

func (server *nfsServer) Download(*pb.DownloadRequest,
	pb.MetaBackup_DownloadServer) error {
	log.Info("Download Called")
	return nil
}

func (server *nfsServer) Delete(context.Context,
	*pb.DeleteRequest) (*pb.Empty, error) {
	log.Info("Delete Called")
	empty := pb.Empty{}
	return &empty, nil
}

func (server *nfsServer) ObjectExists(context.Context,
	*pb.ObjectExistsRequest) (*pb.ObjectExistsResponse, error) {
	log.Info("ObjectExists Called")
	return nil, nil
}
