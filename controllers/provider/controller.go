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
	"encoding/json"
	"fmt"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	jsonpatch "github.com/evanphx/json-patch"
	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuscheme "github.com/soda-cdm/kahu/client/clientset/versioned/scheme"
	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	kahuinformer "github.com/soda-cdm/kahu/client/informers/externalversions"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/utils"
	"github.com/soda-cdm/kahu/volume"
)

const (
	controllerName              = "provider-controller"
	defaultReSyncTimeLoop       = 15 * time.Minute
	finalizerProviderProtection = "kahu.io/provider-protection"
)

type controller struct {
	ctx               context.Context
	logger            log.FieldLogger
	genericController controllers.Controller
	providerClient    kahuv1client.ProviderInterface
	eventRecorder     record.EventRecorder
	providerLister    kahulister.ProviderLister
	volFactory        volume.Interface
}

func NewController(
	ctx context.Context,
	kahuClient versioned.Interface,
	informer kahuinformer.SharedInformerFactory,
	eventBroadcaster record.EventBroadcaster,
	volFactory volume.Interface) (controllers.Controller, error) {

	logger := log.WithField("controller", controllerName)
	registrationController := &controller{
		ctx:            ctx,
		logger:         logger,
		volFactory:     volFactory,
		providerClient: kahuClient.KahuV1beta1().Providers(),
		providerLister: informer.Kahu().V1beta1().Providers().Lister(),
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
		Providers().
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
	ctrl.logger.Info("Running soft reconciliation for providers")
	locations, err := ctrl.providerLister.List(labels.Everything())
	if err != nil {
		ctrl.logger.Errorf("Unable to get provider list for sync. %s", err)
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

	ctrl.logger.Infof("Processing provider(%s) request", name)
	provider, err := ctrl.providerLister.Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Infof("Provider %s already deleted", name)
			return nil
		}
		// re enqueue for processing
		return errors.Wrap(err, fmt.Sprintf("error getting provider[%s] from lister", name))
	}

	if provider.DeletionTimestamp != nil {
		// make provider inactive
		_, err = ctrl.handleDelete(provider)
		return err
	}

	// check for finalizer
	if !utils.ContainsFinalizer(provider, finalizerProviderProtection) {
		clone := provider.DeepCopy()
		utils.SetFinalizer(clone, finalizerProviderProtection)
		provider, err = ctrl.patchProvider(provider, clone)
		if err != nil {
			ctrl.logger.Errorf("Unable to update finalizer for provider(%s)", name)
			return errors.Wrap(err, "Unable to update finalizer")
		}
	}

	if provider.Spec.Type == kahuapi.ProviderTypeVolume {
		if provider.Spec.SupportedVolumeProvisioner == nil &&
			provider.Status.State == kahuapi.ProviderStateAvailable {
			provider.Status.State = kahuapi.ProviderStateUnavailable
			provider.Status.Message = "Supported volume provisioner is not available"
			_, err = ctrl.providerClient.UpdateStatus(context.TODO(), provider, metav1.UpdateOptions{})
			if err != nil {
				ctrl.logger.Errorf("Unable to mark provider[%s] unavailable", name)
				return err
			}
			return fmt.Errorf("supported volume provisioner is not available for provider[%s]", name)
		}

		clone, err := ctrl.handleVolumeServiceProvider(provider)
		if err != nil {
			if provider.Status.State == kahuapi.ProviderStateUnavailable {
				return nil
			}
			provider.Status.State = kahuapi.ProviderStateUnavailable
			provider.Status.Message = err.Error()
			_, err := ctrl.providerClient.UpdateStatus(context.TODO(), provider, metav1.UpdateOptions{})
			if err != nil {
				ctrl.logger.Errorf("Unable to mark provider[%s] unavailable", name)
				return err
			}
			return fmt.Errorf(provider.Status.Message)
		}
		provider = clone
	}

	// return if marked available already
	if provider.Status.State == kahuapi.ProviderStateAvailable {
		return nil
	}

	provider.Status.State = kahuapi.ProviderStateAvailable
	provider.Status.Message = ""
	_, err = ctrl.providerClient.UpdateStatus(context.TODO(), provider, metav1.UpdateOptions{})
	if err != nil {
		ctrl.logger.Errorf("Unable to mark provider[%s] available", name)
	}

	return err
}

func (ctrl *controller) handleVolumeServiceProvider(provider *kahuapi.Provider) (*kahuapi.Provider, error) {
	err := ctrl.volFactory.Provider().SyncProvider(provider)
	if err != nil {
		return provider, err
	}
	// set for default annotation
	if utils.ContainsAnnotation(provider, kahuapi.AnnDefaultVolumeProvider) {
		err := ctrl.volFactory.Provider().SetDefaultProvider(provider)
		if err != nil {
			return provider, err
		}
	}

	return provider, nil
}

func (ctrl *controller) handleDelete(provider *kahuapi.Provider) (*kahuapi.Provider, error) {
	var err error
	provider.Status.State = kahuapi.ProviderStateUnavailable
	name := provider.Name
	provider, err = ctrl.providerClient.UpdateStatus(context.TODO(), provider, metav1.UpdateOptions{})
	if err != nil {
		ctrl.logger.Errorf("Unable to mark provider[%s] unavailable", name)
		return provider, err
	}

	// remove provider info for volume providers in volume package
	if provider.Spec.Type == kahuapi.ProviderTypeVolume {
		err := ctrl.volFactory.Provider().RemoveProvider(provider)
		if err != nil {
			return provider, err
		}
	}

	if utils.ContainsFinalizer(provider, finalizerProviderProtection) {
		clone := provider.DeepCopy()
		utils.RemoveFinalizer(clone, finalizerProviderProtection)
		provider, err = ctrl.patchProvider(provider, clone)
		if err != nil {
			ctrl.logger.Errorf("Unable to remove finalizer for provider(%s)", name)
			return provider, errors.Wrap(err, "Unable to remove finalizer")
		}
	}
	return provider, nil
}

func (ctrl *controller) patchProvider(oldProvider, newProvider *kahuapi.Provider) (*kahuapi.Provider, error) {
	origBytes, err := json.Marshal(oldProvider)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original backup")
	}

	updatedBytes, err := json.Marshal(newProvider)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated backup")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for backup")
	}

	updatedProvider, err := ctrl.providerClient.Patch(context.TODO(),
		oldProvider.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "error patching backup")
	}

	return updatedProvider, nil
}
