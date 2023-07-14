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

package resourcebackup

import (
	"context"
	"io"
	"net"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

const (
	DefaultMetaServiceServicePort   = 443
	defaultMetaServiceContainerPort = 8181
	defaultMetaServiceContainerName = "meta-service"
	defaultMetaServiceCommand       = "/usr/local/bin/meta-service"
	metaServicePortArg              = "-p"
	defaultServicePortName          = "grpc"
	defaultUnixSocketVolName        = "socket"
	defaultUnixSocketMountPath      = "/tmp"
)

type service struct {
	conn       *grpc.ClientConn
	client     metaservice.MetaServiceClient
	parameters map[string]string
	logger     log.FieldLogger
}

func newService(parameters map[string]string, grpcConn *grpc.ClientConn) Interface {
	return &service{
		conn:       grpcConn,
		client:     metaservice.NewMetaServiceClient(grpcConn),
		parameters: parameters,
		logger:     log.WithField("module", "meta-service-client"),
	}
}

func serviceTarget(svcResource k8sresource.ResourceReference, port string) string {
	target := svcResource.Name + "." + svcResource.Namespace
	if net.ParseIP(target) == nil {
		// if not IP, try dns
		target = "dns:///" + target
	}

	return target + ":" + port
}

func newGrpcConnection(target string) (*grpc.ClientConn, error) {
	return newLoadBalanceDial(target, grpc.WithInsecure())
}

func newLoadBalanceDial(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts = append(opts, grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`))
	return grpc.Dial(target, opts...)
}

func (svc *service) Close() error {
	return svc.conn.Close()
}

func (svc *service) Probe(ctx context.Context) error {
	_, err := svc.client.Probe(ctx, &metaservice.ProbeRequest{})
	if err != nil {
		return err
	}
	return nil
}

func (svc *service) UploadK8SResource(ctx context.Context, backupID string, resources []k8sresource.Resource) error {
	backupper, err := svc.client.Upload(ctx)
	if err != nil {
		return err
	}

	// send backup identity
	err = backupper.Send(&metaservice.UploadRequest{
		Request: &metaservice.UploadRequest_Backup{
			Backup: &metaservice.Backup{
				Handle:     backupID,
				Parameters: svc.parameters,
			}}})
	if err != nil {
		return err
	}

	// send resources
	for _, resource := range resources {
		data, err := resource.MarshalJSON()
		if err != nil {
			return err
		}
		gvk := resource.GroupVersionKind()
		err = backupper.Send(&metaservice.UploadRequest{
			Request: &metaservice.UploadRequest_Resource{
				Resource: &metaservice.Resource{
					Identity: &metaservice.Resource_K8SResource{
						K8SResource: &metaservice.K8SResource{
							Name:      resource.GetName(),
							Namespace: resource.GetNamespace(),
							Kind:      gvk.Kind,
							Group:     gvk.Group,
							Version:   gvk.Version,
						},
					},
					Data: data}}})
		if err != nil {
			return err
		}
	}

	// send the confirmation
	_, err = backupper.CloseAndRecv()
	return err
}

func (svc *service) UploadObject(ctx context.Context, backupID string, key string, data []byte) error {
	backupper, err := svc.client.Upload(ctx)
	if err != nil {
		return err
	}

	// send backup identity
	err = backupper.Send(&metaservice.UploadRequest{
		Request: &metaservice.UploadRequest_Backup{
			Backup: &metaservice.Backup{
				Handle:     backupID,
				Parameters: svc.parameters,
			}}})
	if err != nil {
		return err
	}

	// send object
	err = backupper.Send(&metaservice.UploadRequest{
		Request: &metaservice.UploadRequest_Resource{
			Resource: &metaservice.Resource{
				Identity: &metaservice.Resource_Key{Key: key},
				Data:     data}}})
	if err != nil {
		return err
	}

	// send the confirmation
	_, err = backupper.CloseAndRecv()
	return err
}

func (svc *service) Download(ctx context.Context, backupID string, fnDownload FnDownload) error {
	downloadCli, err := svc.client.Download(ctx, &metaservice.DownloadRequest{
		Backup: &metaservice.Backup{
			Handle:     backupID,
			Parameters: svc.parameters,
		}})
	if err != nil {
		return err
	}
	defer func() {
		if err := downloadCli.CloseSend(); err != nil {
			svc.logger.Errorf("Failed to close download call. %s", err)
		}
	}()

	for {
		res, err := downloadCli.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			svc.logger.Errorf("Failed fetching data. %s", err)
			return errors.Wrap(err, "unable to receive backup resource meta")
		}

		for _, resource := range res.GetResources() {
			if err := fnDownload(resource); err != nil {
				return err
			}
		}
	}
	return nil
}

func (svc *service) ResourceExists(ctx context.Context, backupID string) (bool, error) {
	res, err := svc.client.ObjectExists(ctx, &metaservice.ObjectExistsRequest{
		Backup: &metaservice.Backup{
			Handle:     backupID,
			Parameters: svc.parameters,
		}})
	if err != nil {
		return false, err
	}
	return res.GetExists(), nil
}

func (svc *service) Delete(ctx context.Context, backupID string) error {
	_, err := svc.client.Delete(ctx, &metaservice.DeleteRequest{
		Backup: &metaservice.Backup{
			Handle:     backupID,
			Parameters: svc.parameters,
		}})
	return err
}
