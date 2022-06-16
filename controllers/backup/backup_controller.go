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

package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahuv1beta1 "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	pkgbackup "github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/controllers/backup/cmd/options"
	kahuclientset "github.com/soda-cdm/kahu/controllers/client/clientset/versioned"
	kahuv1client "github.com/soda-cdm/kahu/controllers/client/clientset/versioned/typed/kahu/v1beta1"
	kinf "github.com/soda-cdm/kahu/controllers/client/informers/externalversions/kahu/v1beta1"
	kahulister "github.com/soda-cdm/kahu/controllers/client/listers/kahu/v1beta1"
	metaservice "github.com/soda-cdm/kahu/providerframework/meta_service/lib/go"
	collections "github.com/soda-cdm/kahu/utils"
	utils "github.com/soda-cdm/kahu/utils"
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
	clock        clock.Clock
	config       *restclient.Config
	kahuC        kahuv1client.BackupsGetter
	flags        *options.BackupControllerFlags
}

func NewController(klient kahuclientset.Interface,
	backupInformer kinf.BackupInformer,
	config *restclient.Config,
	kahuC kahuv1client.BackupsGetter,
	flags *options.BackupControllerFlags) *Controller {
	// func NewController(klient kahuclientset.Interface, backupInformer kinf.BackupInformer, config *restclient.Config) *Controller {

	c := &Controller{
		BaseController: controllers.NewBaseController(controllers.Backup),
		klient:         klient,
		// backupSynced:   backupInformer.Informer().HasSynced,
		kLister: backupInformer.Lister(),
		clock:   &clock.RealClock{},
		config:  config,
		kahuC:   kahuC,
		flags:   flags,
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

	// var wg sync.WaitGroup

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

	item, shutDown := c.Wq.Get()
	if shutDown {
		return false
	}

	defer c.Wq.Forget(item)
	key, err := cache.MetaNamespaceKeyFunc(item)
	if err != nil {
		c.Logger.Errorf("error %s calling Namespace key func on cache for item", err.Error())
		return false
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("splitting key into namespace and name, error %s\n", err.Error())
		return false
	}

	bkp, err := c.kLister.Backups(ns).Get(name)
	if err != nil {
		c.Logger.Errorf("error %s, Getting the backup resource from lister", err.Error())
		return false
	}
	c.Logger.Infof("backup from k8s %+v", bkp)

	if apierrors.IsNotFound(err) {
		c.Logger.Debugf("backup %s not found", name)
		return false
	}
	if err != nil {
		return false
	}

	c.Logger.Debug("Preparing backup request")
	request := c.prepareBackupRequest(bkp)

	if len(request.Status.ValidationErrors) > 0 {
		request.Status.Phase = kahuv1beta1.BackupPhaseFailedValidation
	} else {
		request.Status.Phase = kahuv1beta1.BackupPhaseInProgress
	}

	updatedBackup, err := c.patchBackup(bkp, request.Backup, c.kahuC)
	if err != nil {
		errors.Wrapf(err, "error updating Backup status to %s", request.Status.Phase)
	}
	c.Logger.Infof("Updated backup %+v", updatedBackup)

	request.Backup = updatedBackup.DeepCopy()
	c.Logger.Infof("Request status %+v", request)

	if request.Backup == nil {
		c.Logger.Errorf("request.Status %v", request)
	}

	if request.Status.Phase == v1beta1.BackupPhaseFailedValidation {
		return false
	}

	if err := c.runBackup(request); err != nil {
		c.Logger.WithError(err).Error("backup failed")
		request.Status.Phase = v1beta1.BackupPhaseFailed
	}

	return true
}

func (c *Controller) patchBackup(original,
	updated *v1beta1.Backup,
	client kahuv1client.BackupsGetter) (*v1beta1.Backup, error) {

	origBytes, err := json.Marshal(original)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original backup")
	}

	updatedBytes, err := json.Marshal(updated)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated backup")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for backup")
	}
	c.Logger.Infof("patch bytes  %s", patchBytes)

	res, err := client.Backups(original.Namespace).Patch(context.TODO(), original.Name, types.MergePatchType,
		patchBytes, metav1.PatchOptions{})
	if err != nil {
		c.Logger.Errorf("patch response error %s", err)
		return nil, errors.Wrap(err, "error patching backup")
	}
	c.Logger.Infof("patch response  %+v", res)

	_, err = client.Backups(original.Namespace).UpdateStatus(context.TODO(), updated,
		metav1.UpdateOptions{})
	if err != nil {
		c.Logger.Errorf("update status response error %s", err)
		return nil, errors.Wrap(err, "error patching backup")
	}
	return res, nil
}

func (c *Controller) runBackup(backup *pkgbackup.Request) error {
	c.Logger.WithField(controllers.Backup, utils.NamespaceAndName(backup)).Info("Setting up backup log")

	grpcConnection, err := metaservice.NewLBDial(fmt.Sprintf("%s:%d", c.flags.MetaServiceAddress,
		c.flags.MetaServicePort),
		grpc.WithInsecure())
	if err != nil {
		c.Logger.Errorf("grpc connection error %s", err)
		return err
	}

	metaClient = metaservice.NewMetaServiceClient(grpcConnection)
	backupClient, err := metaClient.Backup(context.Background())
	if err != nil {
		c.Logger.Errorf("backup request error %s", err)
		return err
	}

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

	k8sClinet, err := kubernetes.NewForConfig(c.config)
	if err != nil {
		c.Logger.Errorf("Unable to connect metadata service %s", err)
		return err
	}


	for _, ns := range backup.Backup.Spec.IncludedNamespaces {
		for _, rs := range backup.Backup.Spec.IncludedResources {
			if rs == "pod" {
				pods, _ := k8sClinet.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{})
				for _, item := range pods.Items {
					itemCopy := item.DeepCopy()
					err := addTypeInformationToObject(itemCopy)
					if err != nil {
						c.Logger.Errorf("Unable to get resource content: %s", err)
					}

					resourceData, err := json.Marshal(itemCopy)
					if err != nil {
						c.Logger.Errorf("Unable to get resource content: %s", err)
						return err
					}
					err = backupClient.Send(&metaservice.BackupRequest{
						Backup: &metaservice.BackupRequest_BackupResource{
							BackupResource: &metaservice.BackResource{
								Resource: &metaservice.Resource{
									Name:    itemCopy.Name,
									Group:   itemCopy.GroupVersionKind().Group,
									Version: itemCopy.GroupVersionKind().Version,
									Kind:    itemCopy.GroupVersionKind().Kind,
								},
								Data: resourceData,
							},
						},
					})

				}
			}
		}
	}

	_, err = backupClient.CloseAndRecv()
	return err
}

// addTypeInformationToObject adds TypeMeta information to a runtime.Object based upon the loaded scheme.Scheme
// inspired by: https://github.com/kubernetes/cli-runtime/blob/v0.19.2/pkg/printers/typesetter.go#L41
func addTypeInformationToObject(obj runtime.Object) error {
	gvks, _, err := scheme.Scheme.ObjectKinds(obj)
	if err != nil {
		return fmt.Errorf("missing apiVersion or kind and cannot assign it; %w", err)
	}

	for _, gvk := range gvks {
		if len(gvk.Kind) == 0 {
			continue
		}
		if len(gvk.Version) == 0 || gvk.Version == runtime.APIVersionInternal {
			continue
		}
		obj.GetObjectKind().SetGroupVersionKind(gvk)
		break
	}

	return nil
}

func (c *Controller) prepareBackupRequest(backup *kahuv1beta1.Backup) *pkgbackup.Request {
	request := &pkgbackup.Request{
		Backup: backup.DeepCopy(),
	}

	if request.Annotations == nil {
		request.Annotations = make(map[string]string)
	}

	// validate the included/excluded resources
	for _, err := range collections.ValidateIncludesExcludes(request.Spec.IncludedResources, request.Spec.ExcludedResources) {
		request.Status.ValidationErrors = append(request.Status.ValidationErrors, fmt.Sprintf("Invalid included/excluded resource lists: %v", err))
	}

	// validate the included/excluded namespaces
	for _, err := range collections.ValidateNamespaceIncludesExcludes(request.Spec.IncludedNamespaces, request.Spec.ExcludedNamespaces) {
		request.Status.ValidationErrors = append(request.Status.ValidationErrors, fmt.Sprintf("Invalid included/excluded namespace lists: %v", err))
	}

	c.Logger.Infoln("validation done:", request.Status.ValidationErrors)
	return request
}

// NamespaceAndName returns a string in the format <namespace>/<name>
func NamespaceAndName(objMeta metav1.Object) string {
	if objMeta.GetNamespace() == "" {
		return objMeta.GetName()
	}
	return fmt.Sprintf("%s/%s", objMeta.GetNamespace(), objMeta.GetName())
}

func (c *Controller) handleAdd(obj interface{}) {
	c.Logger.Infoln("handleAdd was called")
	c.Wq.Add(obj)
}

func (c *Controller) handleDel(obj interface{}) {
	c.Logger.Infoln("handleDel was called")
	c.Wq.Add(obj)
}
