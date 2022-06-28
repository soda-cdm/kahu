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

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
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

// var KindList map[string]int
var KindList = make(map[string]GroupResouceVersion)

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
	var resourcesList []*KubernetesResource

	k8sClinet, err := utils.GetK8sClient(c.config)
	if err != nil {
		c.Logger.Errorf("unable to get k8s client:%s", err)
		return err
	}

	_, resource, _ := k8sClinet.ServerGroupsAndResources()
	for _, group := range resource {
		groupItems, err := c.getGroupItems(group)

		if err != nil {
			c.Logger.WithError(err).WithField("apiGroup", group.String()).Error("Error collecting resources from API group")
			continue
		}

		resourcesList = append(resourcesList, groupItems...)

	}

	for _, kind := range KindList {
		err := c.backup(kind.group, kind.version, kind.resourceName, backup)
		if err != nil {
			// c.Logger.Errorf("backup was not successful. %s", err)
			// return err
			continue
		}
	}

	return nil
}

// getGroupItems collects all relevant items from a single API group.
func (c *Controller) getGroupItems(group *metav1.APIResourceList) ([]*KubernetesResource, error) {
	c.Logger.WithField("group", group.GroupVersion)

	// Parse so we can check if this is the core group
	gv, err := schema.ParseGroupVersion(group.GroupVersion)
	if err != nil {
		return nil, errors.Wrapf(err, "error parsing GroupVersion %q", group.GroupVersion)
	}
	if gv.Group == "" {
		sortCoreGroup(group)
	}

	var items []*KubernetesResource
	for _, resource := range group.APIResources {
		resourceItems, err := c.getResourceItems(gv, resource)
		if err != nil {
			c.Logger.WithError(err).WithField("resource", resource.String()).Error("Error getting items for resource")
			continue
		}

		items = append(items, resourceItems...)
	}

	return items, nil
}

// getResourceItems collects all relevant items for a given group-version-resource.
func (c *Controller) getResourceItems(gv schema.GroupVersion, resource metav1.APIResource) ([]*KubernetesResource, error) {
	gvr := gv.WithResource(resource.Name)

	_, ok := KindList[resource.Kind]
	if !ok {
		groupResourceVersion := GroupResouceVersion{
			resourceName: gvr.Resource,
			version:      gvr.Version,
			group:        gvr.Group,
		}
		KindList[resource.Kind] = groupResourceVersion
	}

	var items []*KubernetesResource

	return items, nil
}

// sortCoreGroup sorts the core API group.
func sortCoreGroup(group *metav1.APIResourceList) {
	sort.SliceStable(group.APIResources, func(i, j int) bool {
		return CoreGroupResourcePriority(group.APIResources[i].Name) < CoreGroupResourcePriority(group.APIResources[j].Name)
	})
}

func (c *Controller) backup(group, version, resource string, backup *PrepareBackup) error {

	dynamicClient, err := dynamic.NewForConfig(c.config)
	if err != nil {
		c.Logger.Errorf("error creating dynamic client: %v\n", err)
		return err
	}

	gvr := schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resource,
	}

	c.Logger.Debugf("group:%s, version:%s, resource:%s", gvr.Group, gvr.Version, gvr.Resource)
	objectsList, err := dynamicClient.Resource(gvr).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		c.Logger.Debugf("error getting %s, %v\n", resource, err)
	}

	resourceObjects, err := meta.ExtractList(objectsList)
	if err != nil {
		return err
	}

	// TODO: Get address and port from backup location
	grpcConnection, err := utils.GetgrpcConn("127.0.0.1", 8181)
	if err != nil {
		c.Logger.Errorf("grpc connection error %s", err)
		return err
	}

	metaClient = utils.GetMetaserviceClient(grpcConnection)
	backupClient, err := metaClient.Backup(context.Background())
	if err != nil {
		c.Logger.Errorf("backup request error %s", err)
		return err
	}

	for _, o := range resourceObjects {
		runtimeObject, ok := o.(runtime.Unstructured)
		if !ok {
			c.Logger.Errorf("error casting object: %v", o)
			return err
		}

		metadata, err := meta.Accessor(runtimeObject)
		if err != nil {
			return err
		}

		resourceData, err := json.Marshal(metadata)
		if err != nil {
			c.Logger.Errorf("Unable to get resource content: %s", err)
			return err
		}
		err = backupClient.Send(&metaservice.BackupRequest{
			Backup: &metaservice.BackupRequest_BackupResource{
				BackupResource: &metaservice.BackupResource{
					Resource: &metaservice.Resource{
						Name:    metadata.GetName(),
						Group:   runtimeObject.GetObjectKind().GroupVersionKind().Group,
						Version: runtimeObject.GetObjectKind().GroupVersionKind().Version,
						Kind:    runtimeObject.GetObjectKind().GroupVersionKind().Kind,
					},
					Data: resourceData,
				},
			},
		})

	}
	return nil
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
