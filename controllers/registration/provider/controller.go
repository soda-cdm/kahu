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

package provider

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
	"github.com/soda-cdm/kahu/utils"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

const (
	controllerName                 = "provider-registration-controller"
	defaultReSyncTimeLoop          = 30 * time.Minute
	finalizerProviderRegProtection = "kahu.io/provider-registration-protection"
)

type controller struct {
	ctx                   context.Context
	logger                log.FieldLogger
	genericController     controllers.Controller
	kubeClient            kubernetes.Interface
	kahuClient            versioned.Interface
	eventRecorder         record.EventRecorder
	processedRegistration utils.Store
	registrationLister    kahulister.ProviderRegistrationLister
}

func NewController(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	kahuClient versioned.Interface,
	informer kahuinformer.SharedInformerFactory,
	eventBroadcaster record.EventBroadcaster) (controllers.Controller, error) {

	logger := log.WithField("controller", controllerName)
	registrationController := &controller{
		ctx:                   ctx,
		logger:                logger,
		kahuClient:            kahuClient,
		kubeClient:            kubeClient,
		processedRegistration: utils.NewStore(utils.DeletionHandlingMetaNamespaceKeyFunc),
		registrationLister:    informer.Kahu().V1beta1().ProviderRegistrations().Lister(),
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
		ProviderRegistrations().
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
	ctrl.logger.Info("Running soft reconciliation for registration")
	registrations, err := ctrl.registrationLister.List(labels.Everything())
	if err != nil {
		ctrl.logger.Errorf("Unable to get provider registration list for sync. %s", err)
		return
	}

	// enqueue all registration for soft reconciliation
	for _, registration := range registrations {
		ctrl.genericController.Enqueue(registration)
	}
}

func (ctrl *controller) processQueue(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ctrl.logger.Errorf("splitting key into namespace and name, error %s", err)
		return err
	}

	ctrl.logger.Infof("Processing provider registration(%s) request", name)
	registration, err := ctrl.registrationLister.Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Infof("Registration %s already deleted", name)
			return nil
		}
		// re enqueue for processing
		return errors.Wrap(err, fmt.Sprintf("error getting registration %s from lister", name))
	}

	if registration.DeletionTimestamp != nil {
		// no op
		// Provider gets delete event because of registration owner reference
		return nil
	}

	newObj, err := utils.StoreRevisionUpdate(ctrl.processedRegistration, registration, "Registration")
	if err != nil {
		ctrl.logger.Errorf("%s", err)
	}
	if !newObj {
		return nil
	}

	// check for finalizer
	if !utils.ContainsFinalizer(registration, finalizerProviderRegProtection) {
		utils.SetFinalizer(registration, finalizerProviderRegProtection)
		registration, err = ctrl.kahuClient.KahuV1beta1().ProviderRegistrations().Update(context.TODO(),
			registration, metav1.UpdateOptions{})
		if err != nil {
			ctrl.logger.Errorf("Unable to update finalizer for registration(%s)", name)
			return errors.Wrap(err, "Unable to update finalizer")
		}
	}

	// check provider
	_, err = ctrl.kahuClient.KahuV1beta1().
		Providers().
		Get(context.TODO(), getProviderName(registration), metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	switch registration.Spec.ProviderType {
	case kahuapi.ResourceBackup:
		return ctrl.handleResourceBackupProviderReg(registration)
	case kahuapi.VolumeBackup:
		return ctrl.handleVolumeBackupProviderReg(registration)
	default:
		return fmt.Errorf("invalid provider type %s", registration.Spec.ProviderType)
	}
}

func (ctrl *controller) handleVolumeBackupProviderReg(registration *kahuapi.ProviderRegistration) error {
	// create provider
	_, err := ctrl.createProvider(kahuapi.ProviderTypeVolume, registration)
	if err != nil {
		return err
	}

	// update status
	registration.Status.Active = true
	_, err = ctrl.kahuClient.KahuV1beta1().
		ProviderRegistrations().
		UpdateStatus(context.TODO(), registration, metav1.UpdateOptions{})
	return err
}

func (ctrl *controller) handleResourceBackupProviderReg(registration *kahuapi.ProviderRegistration) error {
	// create provider
	_, err := ctrl.createProvider(kahuapi.ProviderTypeMetadata, registration)
	if err != nil {
		return err
	}

	// update status
	registration.Status.Active = true
	_, err = ctrl.kahuClient.KahuV1beta1().
		ProviderRegistrations().
		UpdateStatus(context.TODO(), registration, metav1.UpdateOptions{})
	return err
}

// createProvider creates entry on behalf of the provider getting added.
func (ctrl *controller) createProvider(
	providerType kahuapi.ProviderType,
	registration *kahuapi.ProviderRegistration) (*kahuapi.Provider, error) {

	provider := &kahuapi.Provider{
		ObjectMeta: metav1.ObjectMeta{
			Name: getProviderName(registration),
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       registration.Name,
					Kind:       k8sresource.KahuProviderRegistrationGVK.Kind,
					UID:        registration.UID,
					APIVersion: kahuapi.SchemeGroupVersion.String(),
				},
			},
			Annotations: map[string]string{
				utils.AnnProviderRegistrationUID: string(registration.UID),
			},
		},
		Spec: kahuapi.ProviderSpec{
			Version:                    *registration.Spec.Version,
			Type:                       providerType,
			Manifest:                   registration.Spec.Parameters,
			Capabilities:               registration.Spec.Capabilities,
			SupportedVolumeProvisioner: registration.Spec.SupportedVolumeProvisioner,
		},
	}

	provider, err := ctrl.kahuClient.KahuV1beta1().Providers().Create(context.TODO(), provider, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return provider, nil
}

func getProviderName(reg *kahuapi.ProviderRegistration) string {
	return *reg.Spec.ProviderName
}
