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
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahuclientset "github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	kinf "github.com/soda-cdm/kahu/client/informers/externalversions/kahu/v1beta1"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/controllers/backup/cmd/options"
	metaservice "github.com/soda-cdm/kahu/providerframework/meta_service/lib/go"
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

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("splitting key into namespace and name, error %s\n", err.Error())
		return false
	}

	backup, err := c.kLister.Backups(namespace).Get(name)
	if err != nil {
		c.Logger.Errorf("error %s, Getting the backup resource from lister", err.Error())

		if apierrors.IsNotFound(err) {
			c.Logger.Debugf("backup %s not found", name)
		}
		return false
	}

	// prepare and run backup
	c.runBackup(backup)

	return true
}

func (c *Controller) runBackup(backup *v1beta1.Backup) error {
	c.Logger.WithField(controllers.Backup, utils.NamespaceAndName(backup)).Info("Setting up backup log")
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
