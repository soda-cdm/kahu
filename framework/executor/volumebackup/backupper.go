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
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/clock"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	"github.com/soda-cdm/kahu/framework/executor/provisioner"
	volumeservice "github.com/soda-cdm/kahu/providerframework/volumeservice/lib/go"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

type Config struct {
	VolumeBackupServiceImage string
	VolumeServicePort        int
}

type backupper struct {
	counterLock sync.Mutex
	counter     uint32

	cfg           Config
	logger        log.FieldLogger
	bl            *kahuapi.BackupLocation
	provider      *kahuapi.Provider
	providerReg   *kahuapi.ProviderRegistration
	kubeClient    kubernetes.Interface
	kahuClient    kahuv1client.KahuV1beta1Interface
	provisioner   provisioner.Factory
	eventRecorder record.EventRecorder
}

type volumeServiceClient struct {
	volumeservice.VolumeServiceClient
	grpcConn *grpc.ClientConn
}

func newVolumeServiceProvisioner(
	ctx context.Context,
	cfg Config,
	backupLocation *kahuapi.BackupLocation,
	provider *kahuapi.Provider,
	providerReg *kahuapi.ProviderRegistration,
	kubeClient kubernetes.Interface,
	kahuClient kahuv1client.KahuV1beta1Interface,
	eventRecorder record.EventRecorder) volumeServiceProvisioner {
	// initialize event recorder

	return &backupper{
		cfg:           cfg,
		logger:        log.WithField("module", "volume-backup"),
		kubeClient:    kubeClient,
		kahuClient:    kahuClient,
		bl:            backupLocation,
		provider:      provider,
		providerReg:   providerReg,
		provisioner:   provisioner.NewProvisionerFactory(ctx, kubeClient),
		eventRecorder: eventRecorder,
	}
}

func newVolumeService(grpcConn *grpc.ClientConn) volumeService {
	return &volumeServiceClient{
		VolumeServiceClient: volumeservice.NewVolumeServiceClient(grpcConn),
		grpcConn:            grpcConn,
	}
}

func newGrpcConnection(target string) (*grpc.ClientConn, error) {
	return newLoadBalanceDial(target, grpc.WithInsecure())
}

func newLoadBalanceDial(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts = append(opts, grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`))
	return grpc.Dial(target, opts...)
}

func (cli *volumeServiceClient) close() error {
	return cli.grpcConn.Close()
}

func (s *backupper) incCount() {
	s.counterLock.Lock()
	defer s.counterLock.Unlock()
	s.counter += 1
}

func (s *backupper) decCount() {
	s.counterLock.Lock()
	defer s.counterLock.Unlock()
	s.counter -= 1
}

func (s *backupper) counterZero() bool {
	s.counterLock.Lock()
	defer s.counterLock.Unlock()
	return s.counter == 0
}

func (s *backupper) Start(ctx context.Context, callback podTemplateCallback) (volumeService, error) {
	s.logger.Infof("Starting service for backup location[%s]", s.bl.Name)
	if s.legacy() {
		return s.legacyService()
	}

	workloadType := getWorkloadType(s.providerReg)
	switch workloadType {
	case kahuapi.DeploymentWorkloadKind:
		service, err := s.provisionDeploymentWorkload(ctx, callback)
		if err != nil {
			return service, err
		}
		s.incCount()
		return service, nil
	}

	return nil, fmt.Errorf("invalid workload[%s] for backup location[%s]", workloadType, s.bl.Name)
}

func (s *backupper) Done() {
	if s.legacy() {
		s.logger.Infof("Ignore legacy resource backup deployment")
		// do not stop legacy workload
		return
	}

	s.decCount()
}

func (s *backupper) Sync() {
	if !s.counterZero() {
		return
	}
	s.logger.Infof("Stopping resource backupper for %s ", s.bl.Name)
	s.stop()
}

func (s *backupper) delayedStop() {
	realClock := clock.RealClock{}
	for {
		select {
		case <-realClock.After(60 * time.Second):
			s.counterLock.Lock()
			if s.counter == 0 {
				s.counterLock.Unlock()
				s.stop()
				return
			}
			s.counterLock.Lock()
		}
	}
}

func (s *backupper) stop() {
	workloadType := getWorkloadType(s.providerReg)
	switch workloadType {
	case kahuapi.DeploymentWorkloadKind:
		err := s.stopDeploymentWorkload()
		if err != nil {
			s.logger.Warnf("Failed to stop deployment workload[%s] for backup location[%s]",
				workloadType, s.bl.Name)
		}
		return
	}
}

func (s *backupper) Remove() error {
	if s.legacy() {
		// do not remove legacy workload
		return nil
	}

	workloadType := getWorkloadType(s.providerReg)
	switch workloadType {
	case kahuapi.DeploymentWorkloadKind:
		return s.removeDeploymentWorkload()
	}

	return fmt.Errorf("invalid workload[%s] for backup location[%s]", workloadType, s.bl.Name)
}

func getWorkloadType(providerReg *kahuapi.ProviderRegistration) kahuapi.WorkloadKind {
	return providerReg.Spec.Lifecycle.Kind
}

func serviceTarget(svcResource k8sresource.ResourceReference, port string) string {
	target := svcResource.Name + "." + svcResource.Namespace
	if net.ParseIP(target) == nil {
		// if not IP, try dns
		target = "dns:///" + target
	}

	return target + ":" + port
}

func (s *backupper) legacy() bool {
	return s.providerReg == nil
}

func (s *backupper) provisionDeploymentWorkload(_ context.Context, callback podTemplateCallback) (volumeService, error) {
	s.logger.Infof("Starting backup location[%s] service with deployment workload", s.bl.Name)
	podTemplate, err := s.podTemplateFunc()
	if err != nil {
		return nil, err
	}

	// callback
	callback(podTemplate)

	s.logger.Infof("Starting workload with pod template[%+v]", *podTemplate)

	// add service
	k8sService, err := s.provisioner.Deployment().AddService(podTemplate.Name, podTemplate.Namespace, []corev1.ServicePort{{
		Name:       defaultServicePortName,
		Port:       int32(s.cfg.VolumeServicePort),
		TargetPort: intstr.FromInt(defaultContainerPort),
	}})
	if err != nil {
		s.logger.Errorf("Failed to add service for backup location[%s]", s.bl.Name)
		return nil, err
	}

	// start workload
	err = s.provisioner.Deployment().Start(podTemplate.Name, podTemplate.Namespace, podTemplate)
	if err != nil {
		s.logger.Errorf("Failed starting backup location[%s] service with deployment workload", s.bl.Name)
		return nil, err
	}

	// grpc connection
	target := serviceTarget(k8sService, strconv.Itoa(s.cfg.VolumeServicePort))
	s.logger.Infof("Initiating grpc connection with %s", target)
	grpcConn, err := newGrpcConnection(target)
	if err != nil {
		return nil, err
	}

	return newVolumeService(grpcConn), nil
}

func (s *backupper) removeDeploymentWorkload() error {
	s.logger.Infof("Removing backup location[%s] service with deployment workload", s.bl.Name)

	// removing workload
	return s.provisioner.Deployment().Remove(s.bl.Name, s.bl.Namespace)
}

func (s *backupper) stopDeploymentWorkload() error {
	s.logger.Infof("Stopping backup location[%s] service with deployment workload", s.bl.Name)

	// stopping workload
	return s.provisioner.Deployment().Stop(s.bl.Name, s.bl.Namespace)
}

// func (s *backupper) parameters() map[string]string {
// 	param := make(map[string]string)

// 	for key, value := range s.bl.Spec.Config {
// 		param[key] = value
// 	}

// 	for key, value := range s.provider.Spec.Manifest {
// 		param[key] = value
// 	}

// 	return param
// }

func (s *backupper) legacyService() (volumeService, error) {
	return nil, nil
}

func (s *backupper) podTemplateFunc() (*corev1.PodTemplateSpec, error) {
	// get pod template
	podTemplate := s.podTemplate()

	// inject side car in pod template
	s.injectSidecar(podTemplate)

	s.injectUnixSocketVolume(podTemplate)

	s.injectBackupLocationVolume(s.bl, podTemplate)

	return podTemplate, nil
}

func (s *backupper) podTemplate() *corev1.PodTemplateSpec {
	return s.providerReg.Spec.Template.DeepCopy()
}

func (s *backupper) injectSidecar(template *corev1.PodTemplateSpec) {
	template.Spec.Containers = append(template.Spec.Containers, s.sidecar())
}

func (s *backupper) injectUnixSocketVolume(template *corev1.PodTemplateSpec) {
	// add volume
	template.Spec.Volumes = append(template.Spec.Volumes, corev1.Volume{
		Name: defaultUnixSocketVolName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	// add volume mount
	for i := range template.Spec.Containers {
		template.Spec.Containers[i].VolumeMounts = append(template.Spec.Containers[i].VolumeMounts,
			corev1.VolumeMount{
				Name:      defaultUnixSocketVolName,
				MountPath: defaultUnixSocketMountPath,
			})
	}
}

func (s *backupper) sidecar() corev1.Container {
	return corev1.Container{
		Name:    defaultContainerName,
		Command: []string{defaultCommand},
		Image:   s.cfg.VolumeBackupServiceImage,
		Args: []string{
			metaServicePortArg,
			strconv.Itoa(defaultContainerPort),
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          defaultServicePortName,
				ContainerPort: int32(defaultContainerPort),
			},
		},
	}
}

func (s *backupper) injectBackupLocationVolume(bl *kahuapi.BackupLocation,
	template *corev1.PodTemplateSpec) {
	if bl.Spec.Location == nil {
		return
	}

	switch bl.Spec.Location.SourceRef.Kind {
	case kahuapi.PVCLocationSupport:
		template.Spec.Volumes = append(template.Spec.Volumes, corev1.Volume{
			Name: *bl.Spec.Location.SourceRef.Name,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: *bl.Spec.Location.SourceRef.Name,
				},
			},
		})
	case kahuapi.SecretLocationSupport:
		template.Spec.Volumes = append(template.Spec.Volumes, corev1.Volume{
			Name: *bl.Spec.Location.SourceRef.Name,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: *bl.Spec.Location.SourceRef.Name,
				},
			},
		})
	}

	// add volume mount
	for i, container := range template.Spec.Containers {
		if container.Name == defaultContainerName {
			continue
		}
		template.Spec.Containers[i].VolumeMounts = append(template.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{
			Name:      *bl.Spec.Location.SourceRef.Name,
			MountPath: *bl.Spec.Location.Path,
		})
	}
}
