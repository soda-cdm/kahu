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
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/soda-cdm/kahu/providerframework/metaservice/app/options"
	"github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/utils"
)

type metaServer struct {
	ctx        context.Context
	options    options.MetaServiceOptions
	grpcConn   *grpc.ClientConn
	backupRepo providerservice.MetaBackupClient
	logger     log.FieldLogger
}

func NewMetaServiceServer(ctx context.Context,
	serviceOptions options.MetaServiceOptions,
	grpcConn *grpc.ClientConn) metaservice.MetaServiceServer {
	return &metaServer{
		ctx:        ctx,
		options:    serviceOptions,
		grpcConn:   grpcConn,
		backupRepo: providerservice.NewMetaBackupClient(grpcConn),
		logger:     log.WithField("module", "meta-service"),
	}
}

func (server *metaServer) Upload(service metaservice.MetaService_UploadServer) error {
	server.logger.Info("Upload Called .... ")

	uploadClient, err := server.backupRepo.Upload(service.Context())
	if err != nil {
		return status.Errorf(codes.Unknown, "failed to create upload client %s", err)
	}

	// TODO: Currently ignoring parameters
	backupHandle, err := getBackupHandle(service)
	if err != nil {
		return status.Errorf(codes.Unknown, "failed to get backup handle during backup")
	}

	// send backup handle request to driver
	err = uploadClient.Send(&providerservice.UploadRequest{
		Request: &providerservice.UploadRequest_Bucket{
			Bucket: &providerservice.Bucket{
				Handle: backupHandle,
			}}})
	if err != nil {
		return status.Errorf(codes.Internal, "failed to send backup handle")
	}

	for {
		uploadReq, err := service.Recv()
		// If there are no more requests
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Errorf("backupRequest received error %s", err)
			return status.Errorf(codes.Unknown, "error receiving request")
		}

		resource := uploadReq.GetResource()
		if resource == nil {
			return status.Errorf(codes.Aborted, "expecting resource request")
		}
		data := resource.GetData()
		objectKey := getObjectKey(backupHandle, resource)
		err = uploadClient.Send(&providerservice.UploadRequest{
			Request: &providerservice.UploadRequest_Object{
				Object: &providerservice.Object{
					Identifier: objectKey,
					Data:       data,
				}}})
		if err != nil {
			return status.Errorf(codes.Internal, "failed to send backup handle")
		}
	}

	_, err = uploadClient.CloseAndRecv()
	if err != nil {
		server.logger.Errorf("failed to upload backup. %s", err)
		return status.Errorf(codes.Internal, "failed to upload backup. %s", err)
	}

	err = service.SendAndClose(&metaservice.Empty{})
	if err != nil {
		return status.Errorf(codes.Unknown, "failed to close and flush file. %s", err)
	}

	return nil
}

func (server *metaServer) Download(req *metaservice.DownloadRequest,
	service metaservice.MetaService_DownloadServer) error {
	server.logger.Infof("Download backup resources ...")

	backup := req.GetBackup()
	if backup == nil {
		server.logger.Error("empty backup info in delete call")
		return status.Error(codes.Aborted, "empty backup info in Download call")
	}

	// TODO: Support individual file download

	downloadClient, err := server.backupRepo.Download(service.Context(), &providerservice.DownloadRequest{
		Bucket: &providerservice.Bucket{
			Handle:     backup.GetHandle(),
			Parameters: backup.GetParameters(),
		}})
	defer func() {
		err = downloadClient.CloseSend()
		if err != nil {
			server.logger.Errorf("failed to close client. %s", err)
		}
	}()
	if err != nil {
		server.logger.Errorf("Failed to connect with backup driver. %s", err)
		return status.Errorf(codes.Aborted, "Failed to connect with backup driver. %s", err)
	}

	for {
		res, err := downloadClient.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			server.logger.Errorf("Error in driver. %s", err)
			return status.Errorf(codes.Aborted, "Error in driver. %s", err)
		}

		object := res.GetObject()
		if object == nil {
			server.logger.Error("Empty response from driver")
			return status.Error(codes.Aborted, "Empty response from driver")
		}

		identifier := object.GetIdentifier()
		if k8sResource(backup.GetHandle(), object.GetIdentifier()) {
			err = service.Send(&metaservice.DownloadResponse{
				Resources: []*metaservice.Resource{
					{
						Identity: &metaservice.Resource_K8SResource{
							K8SResource: getK8SResource(identifier),
						},
						Data: object.GetData(),
					},
				},
			})
			if err != nil {
				server.logger.Errorf("Error in sending response. %s", err)
				return status.Errorf(codes.Aborted, "Error in sending response. %s", err)
			}
			continue
		}

		err = service.Send(&metaservice.DownloadResponse{
			Resources: []*metaservice.Resource{
				{
					Identity: &metaservice.Resource_Key{
						Key: getObjectResource(identifier),
					},
					Data: object.GetData(),
				},
			},
		})
		if err != nil {
			server.logger.Errorf("Error in sending response. %s", err)
			return status.Errorf(codes.Aborted, "Error in sending response. %s", err)
		}
	}

	return nil
}

func (server *metaServer) ObjectExists(context.Context, *metaservice.ObjectExistsRequest) (*metaservice.ObjectExistsResponse, error) {
	return nil, nil
}

// Probe provider for availability check
func (server *metaServer) Probe(ctx context.Context, req *metaservice.ProbeRequest) (*metaservice.ProbeResponse, error) {
	rsp, err := providerservice.
		NewIdentityClient(server.grpcConn).
		Probe(ctx, &providerservice.ProbeRequest{})
	if err != nil {
		return &metaservice.ProbeResponse{}, err
	}

	return &metaservice.ProbeResponse{Ready: rsp.GetReady()}, nil
}

func (server *metaServer) Delete(ctx context.Context, req *metaservice.DeleteRequest) (*metaservice.Empty, error) {
	empty := &metaservice.Empty{}
	server.logger.Info("Delete backup Called .... ")
	backup := req.GetBackup()
	if backup == nil {
		server.logger.Error("empty backup info in delete call")
		return empty, status.Error(codes.Aborted, "empty backup info in delete call")
	}

	_, err := server.backupRepo.Delete(ctx, &providerservice.DeleteRequest{
		Bucket: &providerservice.Bucket{
			Handle:     backup.GetHandle(),
			Parameters: backup.GetParameters(),
		}})
	if err != nil {
		server.logger.Errorf("failed to delete backup. %s", err)
		return empty, status.Errorf(codes.Aborted, "failed to delete backup. %s", err)
	}

	return empty, nil
}

func getBackupHandle(service metaservice.MetaService_UploadServer) (string, error) {
	backupRequest, err := service.Recv()
	if err != nil {
		return "", status.Errorf(codes.Unknown, "failed with error %s", err)
	}

	backup := backupRequest.GetBackup()
	if backup == nil {
		return "", status.Errorf(codes.Aborted, "first request is not backup identifier")
	}

	// use backup handle name for file
	backupHandle := backup.GetHandle()
	return backupHandle, nil
}

func getObjectKey(backupHandle string, service *metaservice.Resource) string {
	if resource := service.GetK8SResource(); resource != nil {
		return utils.ResourceToFile(backupHandle, resource)
	}

	return filepath.Join(backupHandle, service.GetKey())
}

func getK8SResource(path string) *metaservice.K8SResource {
	return utils.FileToResource(path)
}

func getObjectResource(path string) string {
	_, file := filepath.Split(path)
	return file
}

func k8sResource(backupHandle, path string) bool {
	return filepath.Dir(path) != backupHandle
}
