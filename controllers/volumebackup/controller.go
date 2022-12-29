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
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	grm "k8s.io/kubernetes/pkg/util/goroutinemap"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuscheme "github.com/soda-cdm/kahu/client/clientset/versioned/scheme"
	kahuinformer "github.com/soda-cdm/kahu/client/informers/externalversions"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/utils"
)

const (
	controllerName           = "volume-backup-controller"
	backupFinalizer          = "kahu.io/backup-protection"
	defaultReconcileTimeLoop = 5 * time.Second
	defaultReSyncTimeLoop    = 30 * time.Minute
)

var volBackupSupport = map[string]string{
	"zfs.csi.openebs.io": "zfs.backup.openebs.io",
}

type controller struct {
	ctx               context.Context
	logger            log.FieldLogger
	genericController controllers.Controller
	kubeClient        kubernetes.Interface
	kahuClient        versioned.Interface
	volBackupLister   kahulister.VolumeBackupContentLister
	dynamicClient     dynamic.Interface
	eventRecorder     record.EventRecorder
	discoveryHelper   discovery.DiscoveryHelper
	processedBackup   utils.Store
	csiBackupHandler  grm.GoRoutineMap
}

func NewController(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	kahuClient versioned.Interface,
	dynamicClient dynamic.Interface,
	informer kahuinformer.SharedInformerFactory,
	eventBroadcaster record.EventBroadcaster,
	discoveryHelper discovery.DiscoveryHelper) (controllers.Controller, error) {

	logger := log.WithField("controller", controllerName)
	processedCache := utils.NewStore(utils.DeletionHandlingMetaNamespaceKeyFunc)

	backupController := &controller{
		ctx:              ctx,
		logger:           logger,
		kahuClient:       kahuClient,
		kubeClient:       kubeClient,
		dynamicClient:    dynamicClient,
		discoveryHelper:  discoveryHelper,
		processedBackup:  processedCache,
		volBackupLister:  informer.Kahu().V1beta1().VolumeBackupContents().Lister(),
		csiBackupHandler: grm.NewGoRoutineMap(false),
	}

	// construct controller interface to process worker queue
	genericController, err := controllers.NewControllerBuilder(controllerName).
		SetLogger(logger).
		SetHandler(backupController.processQueue).
		SetReSyncHandler(backupController.reSync).
		SetReSyncPeriod(defaultReSyncTimeLoop).
		Build()
	if err != nil {
		return nil, err
	}

	// register to informer to receive events and push events to worker queue
	informer.Kahu().
		V1beta1().
		VolumeBackupContents().
		Informer().
		AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc: genericController.Enqueue,
				UpdateFunc: func(oldObj, newObj interface{}) {
					genericController.Enqueue(newObj)
				},
			},
		)

	// initialize event recorder
	eventRecorder := eventBroadcaster.NewRecorder(kahuscheme.Scheme,
		v1.EventSource{Component: controllerName})
	backupController.eventRecorder = eventRecorder

	// reference back
	backupController.genericController = genericController

	return genericController, err
}

func (ctrl *controller) reSync() {
	ctrl.logger.Info("Running soft reconciliation for volume-backups")
	backups, err := ctrl.volBackupLister.List(labels.Everything())
	if err != nil {
		// re enqueue for processing
		ctrl.logger.Errorf("Unable to get volume-backup list for re sync. %s", err)
		return
	}

	// enqueue all backup for soft reconciliation
	for _, backup := range backups {
		ctrl.genericController.Enqueue(backup)
	}
}

func (ctrl *controller) processQueue(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ctrl.logger.Errorf("splitting key into namespace and name, error %s", err)
		return err
	}

	ctrl.logger.Infof("Processing volume backup (%s) request", name)
	volBackup, err := ctrl.volBackupLister.Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Infof("Backup %s already deleted", name)
			return nil
		}
		// re enqueue for processing
		return errors.Wrap(err, fmt.Sprintf("error getting backup %s from lister", name))
	}

	newObj, err := utils.StoreRevisionUpdate(ctrl.processedBackup, volBackup, "VolumeBackup")
	if err != nil {
		ctrl.logger.Errorf("%s", err)
	}
	if !newObj {
		return nil
	}

	if volBackup.DeletionTimestamp != nil {
		return nil
	}

	if needVolumeBackupDriver(volBackup) {
		driver, err := ctrl.pickVolumeBackupDriver(volBackup.Spec.VolumeProvider)
		if err != nil {
			return err
		}

		volBackup.Status.VolumeBackupProvider = &driver
		_, err = ctrl.kahuClient.KahuV1beta1().
			VolumeBackupContents().
			UpdateStatus(context.TODO(), volBackup, metav1.UpdateOptions{})

		return err
	}

	return nil
}

func needVolumeBackupDriver(content *kahuapi.VolumeBackupContent) bool {
	return content.Status.VolumeBackupProvider == nil
}

func (ctrl *controller) pickVolumeBackupDriver(volProvider *string) (string, error) {
	if volProvider == nil {
		return "", errors.New("volume provider not available")
	}

	backupDriver, ok := volBackupSupport[*volProvider]
	if !ok {
		return "", errors.New("volume driver not supported")
	}

	return backupDriver, nil
}
