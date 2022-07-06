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
	"time"

	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"

	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"

	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	kahuinformer "github.com/soda-cdm/kahu/client/informers/externalversions/kahu/v1beta1"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/utils"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	controllerName = "BackupController"
	controllerOps  = "Backup"
)

type Config struct {
	MetaServicePort    uint
	MetaServiceAddress string
}

type controller struct {
	config               *Config
	logger               log.FieldLogger
	restClientconfig     *restclient.Config
	genericController    controllers.Controller
	client               kubernetes.Interface
	kahuClient           versioned.Interface
	backupLister         kahulister.BackupLister
	backupClient         kahuv1client.BackupInterface
	backupLocationClient kahuv1client.BackupLocationInterface
}

func NewController(config *Config,
	restClientconfig *restclient.Config,
	kahuClient versioned.Interface,
	backupInformer kahuinformer.BackupInformer) (controllers.Controller, error) {

	logger := log.WithField("controller", controllerName)
	backupController := &controller{
		kahuClient:           kahuClient,
		backupLister:         backupInformer.Lister(),
		restClientconfig:     restClientconfig,
		backupClient:         kahuClient.KahuV1beta1().Backups(),
		backupLocationClient: kahuClient.KahuV1beta1().BackupLocations(),
		config:               config,
		logger:               logger,
	}

	// register to informer to receive events and push events to worker queue
	backupInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    backupController.handleAdd,
			DeleteFunc: backupController.handleDel,
		},
	)

	// construct controller interface to process worker queue
	genericController, err := controllers.NewControllerBuilder(controllerName).
		SetLogger(logger).
		SetHandler(backupController.doBackup).
		Build()
	if err != nil {
		return nil, err
	}

	// reference back
	backupController.genericController = genericController
	return genericController, err
}

func (c *controller) doBackup(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.logger.Errorf("splitting key into namespace and name, error %s\n", err.Error())
		return err
	}

	backup, err := c.backupLister.Get(name)
	if err != nil {
		c.logger.Errorf("error %s, Getting the backup resource from lister", err.Error())

		if apierrors.IsNotFound(err) {
			c.logger.Debugf("backup %s not found", name)
		}
		return err
	}

	c.logger.WithField(controllerOps, utils.NamespaceAndName(backup)).
		Info("Setting up backup log")

	// Validate the Metadatalocation
	backupProvider := backup.Spec.MetadataLocation
	c.logger.Infof("preparing backup for provider: %s ", backupProvider)
	backuplocation, err := c.backupLocationClient.Get(context.Background(), backupProvider, metav1.GetOptions{})
	if err != nil {
		c.logger.Errorf("failed to validate backup location, reason: %s", err)
		backup.Status.Phase = v1beta1.BackupPhaseFailedValidation
		backup.Status.ValidationErrors = append(backup.Status.ValidationErrors, fmt.Sprintf("%v", err))
		c.updateStatus(backup, c.backupClient, backup.Status.Phase)
		return err
	}
	c.logger.Debugf("the provider name in backuplocation:%s", backuplocation)

	c.logger.Infof("Preparing backup request for Provider:%s", backupProvider)
	prepareBackupReq := c.prepareBackupRequest(backup)

	if len(prepareBackupReq.Status.ValidationErrors) > 0 {
		prepareBackupReq.Status.Phase = v1beta1.BackupPhaseFailedValidation
		c.updateStatus(prepareBackupReq.Backup, c.backupClient, prepareBackupReq.Status.Phase)
		return err
	} else {
		prepareBackupReq.Status.Phase = v1beta1.BackupPhaseInProgress
	}
	prepareBackupReq.Status.StartTimestamp = &metav1.Time{Time: time.Now()}
	c.updateStatus(prepareBackupReq.Backup, c.backupClient, prepareBackupReq.Status.Phase)

	// start taking backup
	err = c.runBackup(prepareBackupReq)
	if err != nil {
		prepareBackupReq.Status.Phase = v1beta1.BackupPhaseFailed
	}
	prepareBackupReq.Status.LastBackup = &metav1.Time{Time: time.Now()}

	c.logger.Infof("completed backup with status: %s", prepareBackupReq.Status.Phase)
	c.updateStatus(prepareBackupReq.Backup, c.backupClient, prepareBackupReq.Status.Phase)
	return err
}

func (c *controller) prepareBackupRequest(backup *v1beta1.Backup) *PrepareBackup {
	backupRequest := &PrepareBackup{
		Backup: backup.DeepCopy(),
	}

	if backupRequest.Annotations == nil {
		backupRequest.Annotations = make(map[string]string)
	}

	if backupRequest.Labels == nil {
		backupRequest.Labels = make(map[string]string)
	}

	// validate the resources from include and exlude list
	var includedresourceKindList []string
	var excludedresourceKindList []string

	for _, resource := range backupRequest.Spec.IncludedResources {
		includedresourceKindList = append(includedresourceKindList, resource.Kind)
	}

	if len(includedresourceKindList) == 0 {
		for _, resource := range backupRequest.Spec.ExcludedResources {
			excludedresourceKindList = append(excludedresourceKindList, resource.Kind)
		}
	}

	ResultantResource = utils.GetResultantItems(utils.SupportedResourceList, includedresourceKindList, excludedresourceKindList)

	// validate the namespace from include and exlude list
	for _, err := range utils.ValidateNamespace(backupRequest.Spec.IncludedNamespaces, backupRequest.Spec.ExcludedNamespaces) {
		backupRequest.Status.ValidationErrors = append(backupRequest.Status.ValidationErrors, fmt.Sprintf("Include/Exclude namespace list is not valid: %v", err))
	}

	var allNamespace []string
	if len(backupRequest.Spec.IncludedNamespaces) == 0 {
		allNamespace, _ = c.ListNamespaces(backupRequest)
	}
	ResultantNamespace = utils.GetResultantItems(allNamespace, backupRequest.Spec.IncludedNamespaces, backupRequest.Spec.ExcludedNamespaces)

	// till now validation is ok. Set the backupphase as New to start backup
	backupRequest.Status.Phase = v1beta1.BackupPhaseInit

	return backupRequest
}

func (c *controller) updateStatus(bkp *v1beta1.Backup, client kahuv1client.BackupInterface, phase v1beta1.BackupPhase) {
	backup, err := client.Get(context.Background(), bkp.Name, metav1.GetOptions{})
	if err != nil {
		c.logger.Errorf("failed to get backup for updating status :%+s", err)
		return
	}

	if backup.Status.Phase == v1beta1.BackupPhaseCompleted && phase == v1beta1.BackupPhaseFailed {
		backup.Status.Phase = v1beta1.BackupPhasePartiallyFailed
	} else if backup.Status.Phase == v1beta1.BackupPhasePartiallyFailed {
		backup.Status.Phase = v1beta1.BackupPhasePartiallyFailed
	} else {
		backup.Status.Phase = phase
	}
	backup.Status.ValidationErrors = bkp.Status.ValidationErrors
	_, err = client.UpdateStatus(context.Background(), backup, metav1.UpdateOptions{})
	if err != nil {
		c.logger.Errorf("failed to update backup status :%+s", err)
	}

	return
}

func (c *controller) runBackup(backup *PrepareBackup) error {
	c.logger.Infoln("starting to run backup")

	backupClient := utils.GetMetaserviceBackupClient(c.config.MetaServiceAddress, c.config.MetaServicePort)

	err := backupClient.Send(&metaservice.BackupRequest{
		Backup: &metaservice.BackupRequest_Identifier{
			Identifier: &metaservice.BackupIdentifier{
				BackupHandle: backup.Name,
			},
		},
	})

	if err != nil {
		c.logger.Errorf("Unable to connect metadata service %s", err)
		return err
	}

	resultantResource := sets.NewString(ResultantResource...)
	resultantNamespace := sets.NewString(ResultantNamespace...)
	c.logger.Infof("backup will be taken for these resources:%+v", resultantResource)
	c.logger.Infof("backup will be taken for these namespaces:%+v", resultantNamespace)

	type status struct {
		kind   string
		status string
	}

	var backupStatus = []status{}

	for ns, nsVal := range resultantNamespace {
		c.logger.Infof("started backup for namespace:%s", ns)
		for name, val := range resultantResource {
			c.logger.Debug(nsVal, val)
			switch name {
			case utils.Pod:
				err = c.podBackup(ns, backup, backupClient)
				if err != nil {
					backupStatus = append(backupStatus, status{kind: utils.Pod, status: string(v1beta1.BackupPhaseFailed)})
				} else {
					backupStatus = append(backupStatus, status{kind: utils.Pod, status: string(v1beta1.BackupPhaseCompleted)})
				}
			case utils.Deployment:
				err = c.deploymentBackup(ns, backup, backupClient)
				if err != nil {
					backupStatus = append(backupStatus, status{kind: utils.Deployment, status: string(v1beta1.BackupPhaseFailed)})
				} else {
					backupStatus = append(backupStatus, status{kind: utils.Deployment, status: string(v1beta1.BackupPhaseCompleted)})
				}
			case utils.Configmap:
				err = c.getConfigMapS(ns, backup, backupClient)
				if err != nil {
					backupStatus = append(backupStatus, status{kind: utils.Configmap, status: string(v1beta1.BackupPhaseFailed)})
				} else {
					backupStatus = append(backupStatus, status{kind: utils.Configmap, status: string(v1beta1.BackupPhaseCompleted)})
				}
			case utils.Pvc:
				err = c.getPersistentVolumeClaims(ns, backup, backupClient)
				if err != nil {
					backupStatus = append(backupStatus, status{kind: utils.Pvc, status: string(v1beta1.BackupPhaseFailed)})
				} else {
					backupStatus = append(backupStatus, status{kind: utils.Pvc, status: string(v1beta1.BackupPhaseCompleted)})
				}
			case utils.Sc:
				err = c.getStorageClass(backup, backupClient)
				if err != nil {
					backupStatus = append(backupStatus, status{kind: utils.Sc, status: string(v1beta1.BackupPhaseFailed)})
				} else {
					backupStatus = append(backupStatus, status{kind: utils.Sc, status: string(v1beta1.BackupPhaseCompleted)})
				}
			case utils.Service:
				err = c.getServices(ns, backup, backupClient)
				if err != nil {
					backupStatus = append(backupStatus, status{kind: utils.Service, status: string(v1beta1.BackupPhaseFailed)})
				} else {
					backupStatus = append(backupStatus, status{kind: utils.Service, status: string(v1beta1.BackupPhaseCompleted)})
				}
			case utils.Secret:
				err = c.getSecrets(ns, backup, backupClient)
				if err != nil {
					backupStatus = append(backupStatus, status{kind: utils.Secret, status: string(v1beta1.BackupPhaseFailed)})
				} else {
					backupStatus = append(backupStatus, status{kind: utils.Secret, status: string(v1beta1.BackupPhaseCompleted)})
				}
			case utils.Endpoint:
				err = c.getEndpoints(ns, backup, backupClient)
				if err != nil {
					backupStatus = append(backupStatus, status{kind: utils.Endpoint, status: string(v1beta1.BackupPhaseFailed)})
				} else {
					backupStatus = append(backupStatus, status{kind: utils.Endpoint, status: string(v1beta1.BackupPhaseCompleted)})
				}
			case utils.Replicaset:
				err = c.getReplicasets(ns, backup, backupClient)
				if err != nil {
					backupStatus = append(backupStatus, status{kind: utils.Replicaset, status: string(v1beta1.BackupPhaseFailed)})
				} else {
					backupStatus = append(backupStatus, status{kind: utils.Replicaset, status: string(v1beta1.BackupPhaseCompleted)})
				}
			case utils.Statefulset:
				err = c.getStatefulsets(ns, backup, backupClient)
				if err != nil {
					backupStatus = append(backupStatus, status{kind: utils.Statefulset, status: string(v1beta1.BackupPhaseFailed)})
				} else {
					backupStatus = append(backupStatus, status{kind: utils.Statefulset, status: string(v1beta1.BackupPhaseCompleted)})
				}
			default:
				continue
			}
		}
	}

	for _, phase := range backupStatus {
		if backup.Status.Phase == v1beta1.BackupPhaseInProgress {
			c.updateStatus(backup.Backup, c.backupClient, v1beta1.BackupPhase(phase.status))
		} else if backup.Status.Phase == v1beta1.BackupPhaseFailed && v1beta1.BackupPhase(phase.status) == v1beta1.BackupPhaseCompleted {
			c.updateStatus(backup.Backup, c.backupClient, v1beta1.BackupPhasePartiallyFailed)
		} else if backup.Status.Phase == v1beta1.BackupPhaseCompleted && v1beta1.BackupPhase(phase.status) == v1beta1.BackupPhaseFailed {
			c.updateStatus(backup.Backup, c.backupClient, v1beta1.BackupPhasePartiallyFailed)
		} else {
			c.updateStatus(backup.Backup, c.backupClient, v1beta1.BackupPhase(phase.status))
		}
	}

	_, err = backupClient.CloseAndRecv()

	return err
}

// sortCoreGroup sorts the core API group.
func sortCoreGroup(group *metav1.APIResourceList) {
	sort.SliceStable(group.APIResources, func(i, j int) bool {
		return CoreGroupResourcePriority(group.APIResources[i].Name) < CoreGroupResourcePriority(group.APIResources[j].Name)
	})
}

func (c *controller) backupSend(obj runtime.Object, metadataName string,
	backupSendClient metaservice.MetaService_BackupClient) error {

	gvk, err := addTypeInformationToObject(obj)
	if err != nil {
		c.logger.Errorf("Unable to get gvk: %s", err)
		return err
	}

	resourceData, err := json.Marshal(obj)
	if err != nil {
		c.logger.Errorf("Unable to get resource content: %s", err)
		return err
	}

	c.logger.Infof("sending metadata for object %s/%s", gvk, metadataName)

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

func (c *controller) deleteBackup(name string, backup *v1beta1.Backup) {
	// TODO: delete need to be added
	c.logger.Infof("delete is called for backup:%s", name)

}

func (c *controller) handleAdd(obj interface{}) {
	backup := obj.(*v1beta1.Backup)

	switch backup.Status.Phase {
	case "", v1beta1.BackupPhaseInit:
	default:
		c.logger.WithFields(log.Fields{
			"backup": utils.NamespaceAndName(backup),
			"phase":  backup.Status.Phase,
		}).Infof("Backup: %s is not New, so will not be processed", backup.Name)
		return
	}
	c.genericController.Enqueue(obj)
}

func (c *controller) handleDel(obj interface{}) {
	c.genericController.Enqueue(obj)
}

// addTypeInformationToObject adds TypeMeta information to a runtime.Object based upon the loaded scheme.Scheme
// inspired by: https://github.com/kubernetes/cli-runtime/blob/v0.19.2/pkg/printers/typesetter.go#L41
func addTypeInformationToObject(obj runtime.Object) (schema.GroupVersionKind, error) {
	gvks, _, err := scheme.Scheme.ObjectKinds(obj)
	if err != nil {
		return schema.GroupVersionKind{}, fmt.Errorf("missing apiVersion or kind and cannot assign it; %w", err)
	}

	for _, gvk := range gvks {
		if len(gvk.Kind) == 0 {
			continue
		}
		if len(gvk.Version) == 0 || gvk.Version == runtime.APIVersionInternal {
			continue
		}

		obj.GetObjectKind().SetGroupVersionKind(gvk)
		return gvk, nil
	}

	return schema.GroupVersionKind{}, err
}
