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
	"fmt"
	"sort"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahuv1beta1 "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahuclientset "github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	kinf "github.com/soda-cdm/kahu/client/informers/externalversions/kahu/v1beta1"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/controllers/backup/cmd/options"
	metaservice "github.com/soda-cdm/kahu/providerframework/meta_service/lib/go"
	utils "github.com/soda-cdm/kahu/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	grpcServer     *grpc.Server
	grpcConnection *grpc.ClientConn
	metaClient     metaservice.MetaServiceClient
)

type Controller struct {
	*controllers.BaseController
	client       kubernetes.Interface
	klient       kahuclientset.Interface
	backupSynced []cache.InformerSynced
	kLister      kahulister.BackupLister
	config       *restclient.Config
	bkpClient    kahuv1client.BackupsGetter
	bkpLocClient kahuv1client.BackupLocationsGetter
	flags        *options.BackupControllerFlags
}

func NewController(klient kahuclientset.Interface,
	backupInformer kinf.BackupInformer,
	config *restclient.Config,
	bkpClient kahuv1client.BackupsGetter,
	bkpLocClient kahuv1client.BackupLocationsGetter,
	flags *options.BackupControllerFlags) *Controller {

	c := &Controller{
		BaseController: controllers.NewBaseController(controllers.Backup),
		klient:         klient,
		kLister:        backupInformer.Lister(),
		config:         config,
		bkpClient:      bkpClient,
		bkpLocClient:   bkpLocClient,
		flags:          flags,
	}

	backupInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.handleAdd,
			DeleteFunc: c.handleDel,
		},
	)

	return c
}

func (c *Controller) Run(ch chan struct{}) error {
	// only want to log about cache sync waiters if there are any
	if len(c.backupSynced) > 0 {
		c.Logger.Infoln("Waiting for caches to sync")
		if !cache.WaitForCacheSync(context.Background().Done(), c.backupSynced...) {
			return errors.New("timed out waiting for caches to sync")
		}
		c.Logger.Infoln("Caches are synced")
	}

	if ok := cache.WaitForCacheSync(ch, c.backupSynced...); !ok {
		c.Logger.Infoln("cache was not sycned")
	}

	go wait.Until(c.worker, time.Second, ch)

	<-ch
	return nil
}

func (c *Controller) worker() {
	for c.processNextItem() {
	}
}

type restoreContext struct {
	namespaceClient corev1.NamespaceInterface
}

func (c *Controller) processNextItem() bool {

	item, shutDown := c.Workqueue.Get()
	if shutDown {
		return false
	}

	defer c.Workqueue.Forget(item)

	key, err := cache.MetaNamespaceKeyFunc(item)
	if err != nil {
		c.Logger.Errorf("error %s calling Namespace key func on cache for item", err.Error())
		return false
	}

	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("splitting key into namespace and name, error %s\n", err.Error())
		return false
	}

	backup, err := c.kLister.Get(name)
	if err != nil {
		c.Logger.Errorf("error %s, Getting the backup resource from lister", err.Error())

		if apierrors.IsNotFound(err) {
			c.Logger.Debugf("backup %s not found", name)
		}
		return false
	}

	// Validate the Metadatalocation
	backupProvider := backup.Spec.MetadataLocation
	c.Logger.Infof("preparing backup for provider %s: ", backupProvider)
	backuplocation, err := c.bkpLocClient.BackupLocations().Get(context.Background(), backupProvider, metav1.GetOptions{})
	if err != nil {
		c.Logger.Errorf("failed to validate backup location, reason: %s", err)
		backup.Status.Phase = kahuv1beta1.BackupPhaseFailedValidation
		backup.Status.ValidationErrors = append(backup.Status.ValidationErrors, fmt.Sprintf("%v", err))
		c.updateStatus(backup, c.bkpClient, backup.Status.Phase)
		return false
	}
	c.Logger.Debugf("the provider name in backuplocation:%s", backuplocation)

	c.Logger.Infof("Preparing backup request for Provider:%s", backupProvider)
	prepareBackupReq := c.prepareBackupRequest(backup)

	if len(prepareBackupReq.Status.ValidationErrors) > 0 {
		prepareBackupReq.Status.Phase = kahuv1beta1.BackupPhaseFailedValidation
	} else {
		prepareBackupReq.Status.StartTimestamp = &metav1.Time{Time: time.Now()}
		prepareBackupReq.Status.Phase = kahuv1beta1.BackupPhaseInProgress
	}
	prepareBackupReq.Status.StartTimestamp = &metav1.Time{Time: time.Now()}
	c.updateStatus(prepareBackupReq.Backup, c.bkpClient, prepareBackupReq.Status.Phase)

	// prepare and run backup
	err = c.runBackup(prepareBackupReq)
	if err != nil {
		prepareBackupReq.Status.Phase = kahuv1beta1.BackupPhaseFailed
	}

	prepareBackupReq.Status.Phase = kahuv1beta1.BackupPhaseCompleted
	c.Logger.Infof("updating the final status of backup %s", prepareBackupReq.Status.Phase)
	c.updateStatus(prepareBackupReq.Backup, c.bkpClient, prepareBackupReq.Status.Phase)
	return true
}

func (c *Controller) prepareBackupRequest(backup *kahuv1beta1.Backup) *PrepareBackup {
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
	for _, err := range utils.ValidateIncludesExcludes(backupRequest.Spec.IncludedResources, backupRequest.Spec.ExcludedResources) {
		backupRequest.Status.ValidationErrors = append(backupRequest.Status.ValidationErrors, fmt.Sprintf("Include/Exclude resourse list is not valid: %v", err))
	}

	// validate the namespace from include and exlude list
	for _, err := range utils.ValidateNamespace(backupRequest.Spec.IncludedNamespaces, backupRequest.Spec.ExcludedNamespaces) {
		backupRequest.Status.ValidationErrors = append(backupRequest.Status.ValidationErrors, fmt.Sprintf("Include/Exclude namespace list is not valid: %v", err))
	}

	ResultantNamespace = utils.GetResultantItems(backupRequest.Spec.IncludedNamespaces, backupRequest.Spec.ExcludedNamespaces)
	ResultantResource = utils.GetResultantItems(backupRequest.Spec.IncludedResources, backupRequest.Spec.ExcludedResources)

	// till now validation is ok. Set the backupphase as New to start backup
	backupRequest.Status.Phase = v1beta1.BackupPhaseInit

	return backupRequest
}

func (c *Controller) updateStatus(bkp *v1beta1.Backup, client kahuv1client.BackupsGetter, phase kahuv1beta1.BackupPhase) {
	backup, err := client.Backups().Get(context.Background(), bkp.Name, metav1.GetOptions{})
	if err != nil {
		c.Logger.Errorf("failed to get backup for updating status :%+s", err)
		return
	}

	backup.Status.Phase = phase
	backup.Status.ValidationErrors = bkp.Status.ValidationErrors
	_, err = client.Backups().UpdateStatus(context.Background(), backup, metav1.UpdateOptions{})
	if err != nil {
		c.Logger.Errorf("failed to update backup status :%+s", err)
	}

	return
}

func (c *Controller) runBackup(backup *PrepareBackup) error {
	c.Logger.Infoln("starting to run backup")

	k8sClinet, err := utils.GetK8sClient(c.config)
	if err != nil {
		c.Logger.Errorf("unable to get k8s client:%s", err)
		return err
	}

	_, resource, _ := k8sClinet.ServerGroupsAndResources()

	for _, group := range resource {
		c.getGroupItems(group)

	}

	backupClient := utils.GetMetaserviceBackupClient("127.0.0.1", 8181)

	err = backupClient.Send(&metaservice.BackupRequest{
		Backup: &metaservice.BackupRequest_Identifier{
			Identifier: &metaservice.BackupIdentifier{
				BackupHandle: backup.Name,
			},
		},
	})

	if err != nil {
		c.Logger.Errorf("Unable to connect metadata service %s", err)
		return err
	}

	resultantResource := sets.NewString(ResultantResource...)

	for _, ns := range ResultantNamespace {
		for _, gvr := range gvrList {
			if len(resultantResource) == 0 || resultantResource.Has(gvr.resourceName) {
				switch gvr.resourceName {
				case "deployments":
					c.resourcesBackup(gvr, ns, backup, backupClient)
					c.deploymentBackup(gvr, ns, backup, backupClient)
				case "pods":
					c.resourcesBackup(gvr, ns, backup, backupClient)
				case "replicasets":
					c.resourcesBackup(gvr, ns, backup, backupClient)
				case "configmaps":
					c.resourcesBackup(gvr, ns, backup, backupClient)
				case "secrets":
					c.resourcesBackup(gvr, ns, backup, backupClient)
				case "services":
					c.resourcesBackup(gvr, ns, backup, backupClient)
				case "endpoints":
					c.resourcesBackup(gvr, ns, backup, backupClient)
				case "storageclasses":
					c.resourcesBackup(gvr, ns, backup, backupClient)
				case "persistentvolumeclaims":
					c.resourcesBackup(gvr, ns, backup, backupClient)
				case "statefulsets":
					c.resourcesBackup(gvr, ns, backup, backupClient)
				default:
					continue
				}
			}
		}
	}

	return nil
}

func (c *Controller) getResourceObjects(backup *PrepareBackup,
	gvr GroupResouceVersion, ns string,
	labelSelectors map[string]string) (*unstructured.UnstructuredList, error) {
	dynamicClient, err := utils.GetDynamicClient(c.config)
	if err != nil {
		c.Logger.Errorf("error creating dynamic client: %v\n", err)
		return nil, err
	}

	res_gvr := schema.GroupVersionResource{
		Group:    gvr.group,
		Version:  gvr.version,
		Resource: gvr.resourceName,
	}
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	var ObjectList *unstructured.UnstructuredList
	selectors := labels.Set(labelSelectors).String()

	if ns != "" {
		ObjectList, err = dynamicClient.Resource(res_gvr).Namespace(ns).List(context.Background(), metav1.ListOptions{
			LabelSelector: selectors,
		})
	} else {
		ObjectList, err = dynamicClient.Resource(res_gvr).List(context.Background(), metav1.ListOptions{})
	}
	return ObjectList, nil
}

// getGroupItems collects all relevant items from a single API group.
func (c *Controller) getGroupItems(group *metav1.APIResourceList) error {
	c.Logger.WithField("group", group.GroupVersion)

	// Parse so we can check if this is the core group
	gv, err := schema.ParseGroupVersion(group.GroupVersion)
	if err != nil {
		return errors.Wrapf(err, "error parsing GroupVersion %q", group.GroupVersion)
	}
	if gv.Group == "" {
		sortCoreGroup(group)
	}

	for _, resource := range group.APIResources {
		c.getResourceItems(gv, resource)
	}

	return nil
}

// getResourceItems collects all relevant items for a given group-version-resource.
func (c *Controller) getResourceItems(gv schema.GroupVersion, resource metav1.APIResource) {
	gvr := gv.WithResource(resource.Name)

	_, ok := gvrList[resource.Kind]
	if !ok {
		groupResourceVersion := GroupResouceVersion{
			resourceName: gvr.Resource,
			version:      gvr.Version,
			group:        gvr.Group,
		}
		gvrList[resource.Kind] = groupResourceVersion
	}
}

// sortCoreGroup sorts the core API group.
func sortCoreGroup(group *metav1.APIResourceList) {
	sort.SliceStable(group.APIResources, func(i, j int) bool {
		return CoreGroupResourcePriority(group.APIResources[i].Name) < CoreGroupResourcePriority(group.APIResources[j].Name)
	})
}

func (c *Controller) backupSend(gvr GroupResouceVersion,
	resourceData []byte, metadataName string,
	backupSendClient metaservice.MetaService_BackupClient) error {

	err := backupSendClient.Send(&metaservice.BackupRequest{
		Backup: &metaservice.BackupRequest_BackupResource{
			BackupResource: &metaservice.BackupResource{
				Resource: &metaservice.Resource{
					Name:    metadataName,
					Group:   gvr.group,
					Version: gvr.version,
					Kind:    gvr.resourceName,
				},
				Data: resourceData,
			},
		},
	})
	return err
}

func (c *Controller) handleAdd(obj interface{}) {
	backup := obj.(*v1beta1.Backup)

	switch backup.Status.Phase {
	case "", v1beta1.BackupPhaseInit:
	default:
		c.Logger.WithFields(logrus.Fields{
			"backup": utils.NamespaceAndName(backup),
			"phase":  backup.Status.Phase,
		}).Infof("Backup: %s is not New, so will not be processed", backup.Name)
		return
	}
	c.Workqueue.Add(obj)
}

func (c *Controller) handleDel(obj interface{}) {
	c.Workqueue.Add(obj)
}
