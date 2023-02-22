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

package volumebackup

import (
	"context"
	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	"io"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"k8s.io/client-go/tools/record"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	volumeservice "github.com/soda-cdm/kahu/providerframework/volumeservice/lib/go"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

const (
	DefaultServicePort         = 443
	defaultContainerPort       = 8181
	defaultContainerName       = "volume-service"
	defaultCommand             = "/usr/local/bin/volume-service"
	metaServicePortArg         = "-p"
	defaultServicePortName     = "grpc"
	defaultUnixSocketVolName   = "socket"
	defaultUnixSocketMountPath = "/tmp"
	DefaultVolumeServiceImage  = "sodacdm/kahu-volume-service:v1.0.0"
)

type service struct {
	conn          *grpc.ClientConn
	client        volumeservice.VolumeServiceClient
	parameters    map[string]string
	logger        log.FieldLogger
	kahuClient    kahuv1client.KahuV1beta1Interface
	eventRecorder record.EventRecorder
}

func newService(parameters map[string]string,
	grpcConn *grpc.ClientConn,
	kahuClient kahuv1client.KahuV1beta1Interface,
	eventRecorder record.EventRecorder) Interface {
	return &service{
		conn:          grpcConn,
		client:        volumeservice.NewVolumeServiceClient(grpcConn),
		parameters:    parameters,
		kahuClient:    kahuClient,
		eventRecorder: eventRecorder,
		logger:        log.WithField("module", "volume-service-client"),
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
	_, err := svc.client.Probe(ctx, &volumeservice.ProbeRequest{})
	if err != nil {
		return err
	}
	return nil
}

func (svc *service) Backup(ctx context.Context, vbc *kahuapi.VolumeBackupContent) error {
	backupClient, err := svc.client.Backup(ctx, &volumeservice.BackupRequest{
		Name:       vbc.Name,
		Parameters: svc.parameters,
	})
	if err != nil {
		return err
	}

	completed := false
	for {
		backupRes, err := backupClient.Recv()
		if err == io.EOF {
			svc.logger.Info("Volume backup[%s] completed ....", vbc.Name)
			break
		}
		if err != nil {
			svc.logger.Errorf("Unable to do Volume backup[%s]. %s", vbc.Name, err)
			return err
		}

		if vbc.Name != backupRes.Name {
			svc.logger.Warningf("Out of context response for Volume backup[%s]", vbc.Name)
			continue
		}

		if event := backupRes.GetEvent(); event != nil {
			svc.processEvent(vbc, event)
		}

		if state := backupRes.GetState(); state != nil {
			vbc, completed, err = svc.updateVBCState(vbc, state)
			if err != nil {
				return err
			}
			if completed {
				return nil
			}
		}
	}

	return nil
}

func (svc *service) processEvent(vbc *kahuapi.VolumeBackupContent, event *volumeservice.Event) {
	svc.eventRecorder.Event(vbc, event.Type, event.Name, event.Message)
}

func (svc *service) updateVBCState(vbc *kahuapi.VolumeBackupContent,
	state *volumeservice.BackupState) (*kahuapi.VolumeBackupContent, bool, error) {
	var err error
update:
	// syncing vbc with state
	progressMap := make(map[string]*volumeservice.BackupProgress)
	for _, progress := range state.Progress {
		progressMap[progress.Volume] = progress
	}

	newStates := make([]kahuapi.VolumeBackupState, 0)
	for i, backupState := range vbc.Status.BackupState {
		backupProgress, ok := progressMap[backupState.VolumeName]
		if !ok {
			newStates = append(newStates, backupState)
			continue
		}

		vbc.Status.BackupState[i].Progress = backupProgress.Progress
	}

	for _, newState := range newStates {
		vbc.Status.BackupState = append(vbc.Status.BackupState, kahuapi.VolumeBackupState{
			VolumeName:       newState.VolumeName,
			BackupHandle:     newState.BackupHandle,
			BackupAttributes: newState.BackupAttributes,
			Progress:         newState.Progress,
		})
	}

	vbc, err = svc.kahuClient.VolumeBackupContents().UpdateStatus(context.TODO(), vbc, metav1.UpdateOptions{})
	if err != nil && apierrors.IsConflict(err) {
		vbc, err = svc.kahuClient.VolumeBackupContents().Get(context.TODO(), vbc.Name, metav1.GetOptions{})
		if err != nil {
			goto update
		}
	}
	if err != nil {
		return vbc, false, err
	}

	for _, state := range vbc.Status.BackupState {
		if state.Progress != 100 {
			return vbc, false, nil
		}
	}

	return vbc, true, nil
}

func (svc *service) DeleteBackup(ctx context.Context, vbc *kahuapi.VolumeBackupContent) error {
	_, err := svc.client.DeleteBackup(ctx, &volumeservice.DeleteBackupRequest{
		Name:       vbc.Name,
		Parameters: svc.parameters,
	})
	if err != nil {
		return err
	}
	return nil
}

func (svc *service) Restore(ctx context.Context, vrc *kahuapi.VolumeRestoreContent) error {
	return nil
}
