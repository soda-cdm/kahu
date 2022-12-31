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

package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"github.com/soda-cdm/kahu/utils"
)

func (ctrl *controller) processMetadataBackup(backup *kahuapi.Backup) (*kahuapi.Backup, error) {
	if backup.Status.State == kahuapi.BackupStateCompleted {
		return backup, nil
	}
	// Validate the Metadatalocation
	locationName := backup.Spec.MetadataLocation
	backup.Status.ValidationErrors = []string{}
	ctrl.logger.Infof("Preparing backup for backup location: %s ", locationName)
	metaLocation, err := ctrl.backupLocationLister.Get(locationName)
	if err != nil {
		ctrl.logger.Errorf("failed to validate backup location, reason: %s", err)
		return ctrl.updateBackupStatus(backup, kahuapi.BackupStatus{
			State: kahuapi.BackupStateFailed,
		})
	}

	provider := metaLocation.Spec.ProviderName
	ctrl.logger.Infof("Preparing backup request for Provider:%s", provider)
	prepareBackupReq := ctrl.prepareBackupRequest(backup)

	metaServiceClient, grpcConn, err := ctrl.fetchMetaServiceClient(locationName)
	if err != nil && metaServiceClient == nil {
		ctrl.logger.Errorf("Unable to connect metadata service. %s", err)
		return ctrl.updateBackupStatusWithEvent(backup, kahuapi.BackupStatus{
			State: kahuapi.BackupStateFailed,
		}, v1.EventTypeWarning, EventResourceBackupFailed, "Unable to connect metadata service")
	}
	defer grpcConn.Close()
	backupClient, err := metaServiceClient.Backup(context.Background())
	if err != nil {
		ctrl.logger.Errorf("Unable to get backup client. %s", err)
		return ctrl.updateBackupStatusWithEvent(backup, kahuapi.BackupStatus{
			State: kahuapi.BackupStateFailed,
		}, v1.EventTypeWarning, EventResourceBackupFailed, "Unable to get backup client")
	}

	// Initialize hooks
	err = ctrl.runBackup(prepareBackupReq, backupClient)
	if err != nil {
		prepareBackupReq.Status.State = kahuapi.BackupStateFailed
	} else {
		prepareBackupReq.Status.State = kahuapi.BackupStateCompleted
	}
	_, err = backupClient.CloseAndRecv()
	if err != nil {
		return ctrl.updateBackupStatusWithEvent(backup, kahuapi.BackupStatus{
			State: kahuapi.BackupStateFailed,
		}, v1.EventTypeWarning, EventResourceBackupFailed, "Unable to backup resources")
	}

	ctrl.logger.Infof("completed backup with status: %s", prepareBackupReq.Status.State)
	return ctrl.updateBackupStatus(backup, kahuapi.BackupStatus{
		State: prepareBackupReq.Status.State})
}

func (ctrl *controller) prepareBackupRequest(backup *kahuapi.Backup) *PrepareBackup {
	backupRequest := &PrepareBackup{
		Backup: backup.DeepCopy(),
	}

	return backupRequest
}

func (ctrl *controller) getResultant(backup *PrepareBackup) []string {
	// validate the resources from include and exlude list
	var includedresourceKindList []string
	var excludedresourceKindList []string

	for _, resource := range backup.Spec.IncludeResources {
		includedresourceKindList = append(includedresourceKindList, resource.Kind)
	}

	if len(includedresourceKindList) == 0 {
		for _, resource := range backup.Spec.ExcludeResources {
			if resource.Name == "" || resource.Name == "*" {
				excludedresourceKindList = append(excludedresourceKindList, resource.Kind)
			}
		}
	}

	ctrl.logger.Infof("the supportedResources got from user args: %s", ctrl.config.SupportedResources)

	var supportedResources []string
	supportedResources = utils.SupportedResourceList
	if ctrl.config.SupportedResources != "" {
		supportedResources = strings.Split(ctrl.config.SupportedResources, ",")
		ctrl.logger.Infof("the supportedResources consider for backup is: %s", supportedResources)
	}

	return utils.GetResultantItems(supportedResources, includedresourceKindList, excludedresourceKindList)
}

func (ctrl *controller) runBackup(backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) (returnErr error) {
	ctrl.logger.Infoln("Starting to run backup")
	var backupStatus = []string{}

	err := backupClient.Send(&metaservice.BackupRequest{
		Backup: &metaservice.BackupRequest_Identifier{
			Identifier: &metaservice.BackupIdentifier{
				BackupHandle: backup.Name,
			},
		},
	})
	if err != nil {
		ctrl.logger.Errorf("Unable to send data to metadata service %s", err)
		return err
	}

	resultantResource := sets.NewString(ctrl.getResultant(backup)...)

	var allNamespace []string
	if len(backup.Spec.IncludeNamespaces) == 0 {
		allNamespace, _ = ctrl.ListNamespaces(backup)
	}
	resultNs := utils.GetResultantItems(allNamespace, backup.Spec.IncludeNamespaces, backup.Spec.ExcludeNamespaces)

	resultantNamespace := sets.NewString(resultNs...)
	ctrl.logger.Infof("backup will be taken for these resources:%+v", resultantResource)
	ctrl.logger.Infof("backup will be taken for these namespaces:%+v", resultantNamespace)

	for ns, nsVal := range resultantNamespace {
		ctrl.logger.Infof("started backup for namespace:%s", ns)
		for name, val := range resultantResource {
			ctrl.logger.Debug(nsVal, val)
			switch name {
			case utils.Pod:
				err = ctrl.podBackup(ns, backup, backupClient)
				if err != nil {
					return err
				}
			case utils.Deployment:
				err = ctrl.deploymentBackup(ns, backup, backupClient)
				if err != nil {
					return err
				}
			case utils.Configmap:
				err = ctrl.getConfigMapS(ns, backup, backupClient)
				if err != nil {
					return err
				}
			case utils.PVC:
				err = ctrl.getPersistentVolumeClaims(ns, backup, backupClient)
				if err != nil {
					return err
				}
			case utils.Service:
				err = ctrl.getServices(ns, backup, backupClient)
				if err != nil {
					return err
				}
			case utils.Secret:
				err = ctrl.getSecrets(ns, backup, backupClient)
				if err != nil {
					return err
				}
			case utils.Endpoint:
				err = ctrl.getEndpoints(ns, backup, backupClient)
				if err != nil {
					return err
				}
			case utils.Replicaset:
				err = ctrl.replicaSetBackup(ns, backup, backupClient)
				if err != nil {
					return err
				}
			case utils.StatefulSet:
				err = ctrl.getStatefulsets(ns, backup, backupClient)
				if err != nil {
					return err
				}
			case utils.DaemonSet:
				err = ctrl.daemonSetBackup(ns, backup, backupClient)
				if err != nil {
					return err
				}
			default:
				continue
			}
		}
	}

	// add volume backup content in backup
	err = ctrl.backupVolumeBackupContent(backup, backupClient)
	if err != nil {
		backup.Status.State = kahuapi.BackupStateFailed
		ctrl.logger.Errorf("Failed to backup volume backup contents. %s", err)
		return errors.Wrap(err, "unable to backup volume backup contents")
	}

	ctrl.logger.Infof("the intermediate status:%s", backupStatus)

	return err
}

// sortCoreGroup sorts the core API group.
func sortCoreGroup(group *metav1.APIResourceList) {
	sort.SliceStable(group.APIResources, func(i, j int) bool {
		return CoreGroupResourcePriority(group.APIResources[i].Name) < CoreGroupResourcePriority(group.APIResources[j].Name)
	})
}

func (ctrl *controller) backupSend(obj runtime.Object, metadataName string,
	backupSendClient metaservice.MetaService_BackupClient) error {

	gvk, err := addTypeInformationToObject(obj)
	if err != nil {
		ctrl.logger.Errorf("Unable to get gvk: %s", err)
		return err
	}

	resourceData, err := json.Marshal(obj)
	if err != nil {
		ctrl.logger.Errorf("Unable to get resource content: %s", err)
		return err
	}

	ctrl.logger.Infof("sending metadata for object %s/%s", gvk, metadataName)

	err = backupSendClient.Send(&metaservice.BackupRequest{
		Backup: &metaservice.BackupRequest_BackupResource{
			BackupResource: &metaservice.BackupResource{
				Resource: &metaservice.Resource{
					Name:    metadataName,
					Group:   gvk.Group,
					Version: gvk.Version,
					Kind:    gvk.Kind,
				},
				Data: resourceData,
			},
		},
	})
	return err
}

func (ctrl *controller) deleteMetadataBackup(backup *kahuapi.Backup) error {

	// Proceed to metadata backup delete only if backup was successful
	if backup.Status.State == kahuapi.BackupStateFailed ||
		toOrdinal(backup.Status.Stage) < toOrdinal(kahuapi.BackupStageResources) {
		return nil
	}

	deleteRequest := &metaservice.DeleteRequest{
		Id: &metaservice.BackupIdentifier{
			BackupHandle: backup.Name,
		},
	}

	metaservice, grpcConn, err := ctrl.fetchMetaServiceClient(backup.Spec.MetadataLocation)
	if err != nil {
		return err
	}
	defer grpcConn.Close()

	_, err = metaservice.Delete(context.Background(), deleteRequest)
	if err != nil {
		ctrl.logger.Errorf("Unable to delete metadata backup file %s. %s", backup.Name, err)
		return fmt.Errorf("unable to delete metadata backup file %v", backup.Name)
	}

	return nil
}
