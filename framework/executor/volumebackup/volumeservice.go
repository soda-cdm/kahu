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
	"io"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
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

var (
	backupBackoff = wait.Backoff{
		Duration: time.Second * 5,
		Steps:    3,
	}
	csiVolSnapshotGroup = k8sresource.CSIVolumeSnapshotGVK.Group
	csiVolumeNodePath   = "/var/lib/kubelet/pods/%s/volumes/kubernetes.io~csi/%s/mount"
	csiHostPathType     = corev1.HostPathDirectory
)

type service struct {
	parameters         map[string]string
	logger             log.FieldLogger
	kahuClient         kahuv1client.KahuV1beta1Interface
	kubeClient         kubernetes.Interface
	eventRecorder      record.EventRecorder
	serviceProvisioner volumeServiceProvisioner
	deployNamespace    string
	backupLocation     *kahuapi.BackupLocation
	provider           *kahuapi.Provider
}

func NewService(
	ctx context.Context,
	cfg Config,
	deployNamespace string,
	backupLocation *kahuapi.BackupLocation,
	provider *kahuapi.Provider,
	providerReg *kahuapi.ProviderRegistration,
	kahuClient kahuv1client.KahuV1beta1Interface,
	kubeClient kubernetes.Interface,
	eventRecorder record.EventRecorder) Service {
	return &service{
		parameters:      backupLocation.Spec.Config,
		deployNamespace: deployNamespace,
		kahuClient:      kahuClient,
		kubeClient:      kubeClient,
		eventRecorder:   eventRecorder,
		backupLocation:  backupLocation,
		provider:        provider,
		logger:          log.WithField("module", "volume-service-client"),
		serviceProvisioner: newVolumeServiceProvisioner(ctx, cfg, backupLocation, provider,
			providerReg, kubeClient, kahuClient, eventRecorder),
	}
}

func (svc *service) Sync() {
	svc.serviceProvisioner.Sync()
}

func (svc *service) Probe(ctx context.Context) error {
	volumeService, err := svc.serviceProvisioner.Start(ctx, func(pts *corev1.PodTemplateSpec) {
		pts.Name = svc.backupLocation.Name
		pts.Namespace = svc.deployNamespace
	})
	if err != nil {
		return err
	}
	defer volumeService.close()

	_, err = volumeService.Probe(ctx, &volumeservice.ProbeRequest{})
	if err != nil {
		return err
	}
	return nil
}

func (svc *service) Cleanup(ctx context.Context) error {
	// TODO: implement cleanup funcationality
	return nil
}

func (svc *service) constructBackupRequest(vbc *kahuapi.VolumeBackupContent) (*volumeservice.BackupRequest, error) {
	backupReq := new(volumeservice.BackupRequest)
	backupReq.BackupContentName = vbc.Name
	backupReq.Parameters = vbc.Spec.Parameters

	backupInfo := make([]volumeservice.BackupVolume, 0)
	for _, volumeRef := range vbc.Spec.VolumeRef {
		pv, err := svc.getPersistanceVolumeFromRef(volumeRef.Volume)
		if err != nil {
			return nil, err
		}

		backupInfo = append(backupInfo, volumeservice.BackupVolume{
			Pv: pv,
			Snapshot: &volumeservice.Snapshot{
				SnapshotHandle:     volumeRef.Snapshot.Handle,
				SnapshotAttributes: volumeRef.Snapshot.Attribute,
			},
		})
	}

	return nil, nil
}

func (svc *service) getPersistanceVolumeFromRef(volume kahuapi.ResourceReference) (*corev1.PersistentVolume, error) {
	pvc, err := svc.kubeClient.CoreV1().PersistentVolumeClaims(volume.Namespace).Get(context.TODO(), volume.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return svc.kubeClient.CoreV1().PersistentVolumes().Get(context.TODO(), pvc.Spec.VolumeName, metav1.GetOptions{})
}

func (svc *service) providerNeedVolumeSupport() bool {
	for _, flag := range svc.provider.Spec.Flags {
		if flag == kahuapi.VolumeBackupNeedVolumeSupport {
			return true
		}
	}
	return false
}

func (svc *service) Backup(ctx context.Context, vbc *kahuapi.VolumeBackupContent) error {
	if svc.providerNeedVolumeSupport() {
		return svc.backupWithVolumeSupport(ctx, vbc)
	}

	volumeService, err := svc.serviceProvisioner.Start(ctx, func(pts *corev1.PodTemplateSpec) {
		pts.Name = svc.backupLocation.Name
		pts.Namespace = svc.deployNamespace
	})
	if err != nil {
		return err
	}
	defer volumeService.close()

	err = wait.ExponentialBackoffWithContext(ctx, backupBackoff, func() (done bool, err error) {
		backupReq, err := svc.constructBackupRequest(vbc)
		if err != nil {
			return false, err
		}

		backupClient, err := volumeService.Backup(ctx, backupReq)
		if err != nil {
			return false, err
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
				return false, err
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
					return false, err
				}
				if completed {
					return true, nil
				}
			}
		}

		return false, fmt.Errorf("retry volume backup for vbc[%s] again", vbc.Name)
	})

	return err
}

func (svc *service) backupWithVolumeSupport(ctx context.Context, vbc *kahuapi.VolumeBackupContent) error {
	// recreate volume from snapshot
	pvcs, err := svc.pvcFromSnapshot(ctx, vbc)
	if err != nil {
		return err
	}

	replicaSets, err := svc.ensurePausePodsForPVCs(ctx, pvcs)
	if err != nil {
		return err
	}

	// create a pod for each volume
	for _, replicaSet := range replicaSets {
		svc.logger.Infof("Start backup for pause pod from replicaset[%s/%s]", replicaSet.Namespace, replicaSet.Name)
		err := svc.backupVolumeFromPauseReplicaSet(ctx, replicaSet)
		if err != nil {
			return err
		}
	}

	return nil
}

func (svc *service) backupVolumeFromPauseReplicaSet(ctx context.Context,
	replicaSet *appv1.ReplicaSet) error {
	podSelector := replicaSet.Spec.Selector
	namespace := replicaSet.Namespace

	svc.logger.Infof("Pod selector [%s] ", podSelector.String())
	podlabelSelector, err := metav1.LabelSelectorAsSelector(podSelector)
	if err != nil {
		return err
	}
	var pods *corev1.PodList
	err = wait.ExponentialBackoffWithContext(ctx, backupBackoff, func() (done bool, err error) {
		svc.logger.Infof("Checking pause pod with selector[%s] ", podlabelSelector.String())
		pods, err = svc.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: podlabelSelector.String(),
		})
		if err != nil {
			svc.logger.Warnf("Unable to list pause replicaset[%s/%s] pods[] for backup", replicaSet.Namespace, replicaSet.Name)
			return false, err
		}
		if len(pods.Items) < 1 {
			return false, fmt.Errorf("replicaset[%s/%s] pods is still not available for backup", replicaSet.Namespace, replicaSet.Name)
		}

		return true, nil
	})
	if err != nil {
		svc.logger.Errorf("Unable to list pause replicaset[%s/%s] pods for backup", replicaSet.Namespace, replicaSet.Name)
		return err
	}

	// pick one pod
	pod := pods.Items[0]
	nodeName := pod.Spec.NodeName
	podUID := pod.UID
	var pvcName string
	for _, volume := range pod.Spec.Volumes {
		svc.logger.Infof("Checking pause pod volumes[%+v] ", volume)
		if volume.Name == "volume" && volume.PersistentVolumeClaim != nil {
			pvcName = volume.PersistentVolumeClaim.ClaimName
		}
	}

	return svc.backupVolumeFromHostPath(ctx, nodeName, namespace, string(podUID), pvcName)
}

func (svc *service) backupVolumeFromHostPath(ctx context.Context,
	nodeName string,
	namespace string,
	podUID string,
	pvcName string) error {
	svc.logger.Infof("Backup from pause pod[%s] and pvc[%s] ", podUID, pvcName)
	pvc, err := svc.kubeClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), pvcName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	volumeService, err := svc.serviceProvisioner.Start(ctx, func(pts *corev1.PodTemplateSpec) {
		pts.Name = svc.backupLocation.Name + "-" + uuid.New().String()
		pts.Namespace = svc.deployNamespace
		pts.Spec.NodeName = nodeName

		pts.Spec.Volumes = append(pts.Spec.Volumes, corev1.Volume{
			Name: pvcName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fmt.Sprintf(csiVolumeNodePath, podUID, pvc.Spec.VolumeName),
					Type: &csiHostPathType,
				},
			},
		})

		// add volume mount
		for i, container := range pts.Spec.Containers {
			if container.Name == defaultContainerName {
				continue
			}
			pts.Spec.Containers[i].VolumeMounts = append(pts.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{
				Name:      pvcName,
				MountPath: "/source",
			})
		}
	})
	if err != nil {
		return err
	}
	defer volumeService.close()

	// err = wait.ExponentialBackoffWithContext(ctx, backupBackoff, func() (done bool, err error) {
	// 	backupReq, err := svc.constructBackupRequest(vbc)
	// 	if err != nil {
	// 		return false, err
	// 	}

	// 	backupClient, err := volumeService.Backup(ctx, backupReq)
	// 	if err != nil {
	// 		return false, err
	// 	}

	// 	completed := false
	// 	for {
	// 		backupRes, err := backupClient.Recv()
	// 		if err == io.EOF {
	// 			svc.logger.Info("Volume backup[%s] completed ....", vbc.Name)
	// 			break
	// 		}
	// 		if err != nil {
	// 			svc.logger.Errorf("Unable to do Volume backup[%s]. %s", vbc.Name, err)
	// 			return false, err
	// 		}

	// 		if vbc.Name != backupRes.Name {
	// 			svc.logger.Warningf("Out of context response for Volume backup[%s]", vbc.Name)
	// 			continue
	// 		}

	// 		if event := backupRes.GetEvent(); event != nil {
	// 			svc.processEvent(vbc, event)
	// 		}

	// 		if state := backupRes.GetState(); state != nil {
	// 			vbc, completed, err = svc.updateVBCState(vbc, state)
	// 			if err != nil {
	// 				return false, err
	// 			}
	// 			if completed {
	// 				return true, nil
	// 			}
	// 		}
	// 	}

	// 	return false, fmt.Errorf("retry volume backup for vbc[%s] again", vbc.Name)
	// })

	return err

}

func (svc *service) ensurePausePodsForPVCs(ctx context.Context,
	pvcs []*corev1.PersistentVolumeClaim) ([]*appv1.ReplicaSet, error) {
	replicas := make([]*appv1.ReplicaSet, 0)
	var pausePodReplica int32 = 1
	for _, pvc := range pvcs {
		svc.logger.Infof("Ensuring pause pod for pvc [%s]", pvc.Name)
		namespace := pvc.Namespace
		if namespace == "" {
			namespace = "default"
		}
		replicaSet := &appv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvc.Name,
				Namespace: namespace,
			},
			Spec: appv1.ReplicaSetSpec{
				Replicas: &pausePodReplica,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kahu.io/pause-pod": pvc.Name,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"kahu.io/pause-pod": pvc.Name,
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "pause-container",
								Image: "registry.k8s.io/pause:3.7",
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "volume",
										MountPath: "/volume",
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "volume",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: pvc.Name,
									},
								},
							},
						},
					},
				},
			},
		}

		replica, err := svc.kubeClient.AppsV1().ReplicaSets(namespace).Create(ctx, replicaSet, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, err
		}

		if apierrors.IsAlreadyExists(err) {
			replica, err = svc.kubeClient.AppsV1().ReplicaSets(namespace).Get(ctx, replicaSet.Name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
		}

		replicas = append(replicas, replica)
	}

	return replicas, nil
}

func (svc *service) pvcFromSnapshot(ctx context.Context, vbc *kahuapi.VolumeBackupContent) ([]*corev1.PersistentVolumeClaim, error) {
	pvcs := make([]*corev1.PersistentVolumeClaim, 0)
	for _, volRef := range vbc.Spec.VolumeRef {
		// currently only handle for CSI
		csiSnapshot := volRef.CSISnapshot
		if csiSnapshot != nil {
			snapshotName := csiSnapshot.SnapshotRef.Name
			snapshotNamespace := csiSnapshot.SnapshotRef.Namespace

			pvcTemplate := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapshotName,
					Namespace: snapshotNamespace,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					DataSourceRef: &corev1.TypedLocalObjectReference{
						Kind:     k8sresource.CSIVolumeSnapshotGVK.Kind,
						APIGroup: &csiVolSnapshotGroup,
						Name:     snapshotName,
					},
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			}
			pvc, err := svc.kubeClient.CoreV1().
				PersistentVolumeClaims(snapshotNamespace).
				Create(ctx, pvcTemplate, metav1.CreateOptions{})
			if err != nil && !apierrors.IsAlreadyExists(err) {
				return nil, err
			}

			if apierrors.IsAlreadyExists(err) {
				pvc, err = svc.kubeClient.CoreV1().
					PersistentVolumeClaims(snapshotNamespace).Get(ctx, pvcTemplate.Name, metav1.GetOptions{})
				if err != nil {
					return nil, err
				}
			}

			pvcs = append(pvcs, pvc)
		}
	}

	return pvcs, nil
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
	volumeService, err := svc.serviceProvisioner.Start(ctx, func(pts *corev1.PodTemplateSpec) {
		pts.Name = svc.backupLocation.Name
		pts.Namespace = svc.deployNamespace
	})
	if err != nil {
		return err
	}
	defer volumeService.close()

	_, err = volumeService.DeleteBackup(ctx, &volumeservice.DeleteBackupRequest{
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
