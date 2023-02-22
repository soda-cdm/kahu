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

package backuplocation

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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuscheme "github.com/soda-cdm/kahu/client/clientset/versioned/scheme"
	kahuinformer "github.com/soda-cdm/kahu/client/informers/externalversions"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/utils"
	"github.com/soda-cdm/kahu/volume"
)

const (
	controllerName                    = "backup-location-controller"
	defaultReSyncTimeLoop             = 60 * time.Minute
	finalizerBackupLocationProtection = "kahu.io/backup-location-protection"
)

type controller struct {
	ctx                  context.Context
	logger               log.FieldLogger
	genericController    controllers.Controller
	kubeClient           kubernetes.Interface
	kahuClient           versioned.Interface
	eventRecorder        record.EventRecorder
	processedLocation    utils.Store
	backupLocationLister kahulister.BackupLocationLister
	framework            framework.Interface
	volFactory           volume.Interface
}

func NewController(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	kahuClient versioned.Interface,
	informer kahuinformer.SharedInformerFactory,
	eventBroadcaster record.EventBroadcaster,
	framework framework.Interface,
	volFactory volume.Interface) (controllers.Controller, error) {

	logger := log.WithField("controller", controllerName)
	registrationController := &controller{
		ctx:                  ctx,
		logger:               logger,
		kahuClient:           kahuClient,
		kubeClient:           kubeClient,
		processedLocation:    utils.NewStore(utils.DeletionHandlingMetaNamespaceKeyFunc),
		backupLocationLister: informer.Kahu().V1beta1().BackupLocations().Lister(),
		framework:            framework,
		volFactory:           volFactory,
	}

	// construct controller interface to process worker queue
	genericController, err := controllers.NewControllerBuilder(controllerName).
		SetLogger(logger).
		SetHandler(registrationController.processQueue).
		SetReSyncHandler(registrationController.reSync).
		SetReSyncPeriod(defaultReSyncTimeLoop).
		Build()
	if err != nil {
		return nil, err
	}

	// register to informer to receive events and push events to worker queue
	informer.Kahu().
		V1beta1().
		BackupLocations().
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
	registrationController.eventRecorder = eventRecorder

	// reference back
	registrationController.genericController = genericController
	return genericController, err
}

func (ctrl *controller) reSync() {
	ctrl.logger.Info("Running soft reconciliation for backup location")
	locations, err := ctrl.backupLocationLister.List(labels.Everything())
	if err != nil {
		ctrl.logger.Errorf("Unable to get provider backup location list for sync. %s", err)
		return
	}

	// enqueue inactive backup location for soft reconciliation
	for _, location := range locations {
		ctrl.genericController.Enqueue(location)
	}
}

func (ctrl *controller) processQueue(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ctrl.logger.Errorf("splitting key into namespace and name, error %s", err)
		return err
	}

	ctrl.logger.Infof("Processing provider backup location(%s) request", name)
	backupLocation, err := ctrl.backupLocationLister.Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Infof("Backup location %s already deleted", name)
			return nil
		}
		// re enqueue for processing
		return errors.Wrap(err, fmt.Sprintf("error getting backup location %s from lister", name))
	}

	if backupLocation.DeletionTimestamp != nil {
		err = ctrl.framework.Executors().Uninstall(ctrl.ctx, backupLocation.Name)
		if err != nil {
			ctrl.logger.Errorf("Failed to uninstall backup location. %s", err)
			return err
		}

		// remove traces of location from volume factory
		ctrl.volFactory.Location().RemoveLocation(backupLocation)

		if utils.ContainsFinalizer(backupLocation, finalizerBackupLocationProtection) {
			utils.RemoveFinalizer(backupLocation, finalizerBackupLocationProtection)
			backupLocation, err = ctrl.kahuClient.KahuV1beta1().BackupLocations().Update(context.TODO(),
				backupLocation, metav1.UpdateOptions{})
			if err != nil {
				ctrl.logger.Errorf("Unable to update finalizer for backup location(%s)", name)
				return errors.Wrap(err, "Unable to update finalizer")
			}
			return err
		}

		return nil
	}

	newObj, err := utils.StoreRevisionUpdate(ctrl.processedLocation, backupLocation, "BackupLocation")
	if err != nil {
		ctrl.logger.Errorf("%s", err)
	}
	if !newObj {
		return nil
	}

	// check for finalizer
	if !utils.ContainsFinalizer(backupLocation, finalizerBackupLocationProtection) {
		utils.SetFinalizer(backupLocation, finalizerBackupLocationProtection)
		backupLocation, err = ctrl.kahuClient.KahuV1beta1().BackupLocations().Update(context.TODO(),
			backupLocation, metav1.UpdateOptions{})
		if err != nil {
			ctrl.logger.Errorf("Unable to update finalizer for backup location(%s)", name)
			return errors.Wrap(err, "Unable to update finalizer")
		}
	}

	// check provider
	ctrl.logger.Infof("Checking provider [%s]", getProviderName(backupLocation))
	provider, err := ctrl.kahuClient.KahuV1beta1().
		Providers().
		Get(context.TODO(), getProviderName(backupLocation), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		ctrl.logger.Errorf("Provider[%s] not available for backup location[%s]")
		backupLocation.Status.Active = false
		backupLocation, err = ctrl.kahuClient.KahuV1beta1().
			BackupLocations().
			UpdateStatus(ctrl.ctx, backupLocation, metav1.UpdateOptions{})
		return err
	} else if err != nil {
		return err
	}

	ctrl.logger.Infof("Try to install backup location [%s]", backupLocation.Name)
	err = ctrl.framework.Executors().Install(ctrl.ctx, backupLocation.Name)
	if err == nil {
		backupLocation.Status.Active = true
		_, err = ctrl.kahuClient.KahuV1beta1().
			BackupLocations().
			UpdateStatus(ctrl.ctx, backupLocation, metav1.UpdateOptions{})
		return err
	}

	if provider.Spec.Type == kahuapi.ProviderTypeVolume {
		// add into volume factory of default annotation set
		if utils.ContainsAnnotation(backupLocation, kahuapi.AnnDefaultBackupLocation) {
			err = ctrl.volFactory.Location().SetDefaultLocation(backupLocation)
			if err != nil {
				ctrl.logger.Warningf("Unable to set default backup location. %s", err)
			}
		}
	}

	return err
}

func getProviderName(bl *kahuapi.BackupLocation) string {
	return bl.Spec.ProviderName
}
