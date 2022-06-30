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

package manager

import (
	"context"
	"fmt"
	"github.com/soda-cdm/kahu/controllers"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	kahuv1beta1 "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/client/informers/externalversions"
	"github.com/soda-cdm/kahu/controllers/app/config"
	"github.com/soda-cdm/kahu/controllers/backup"
)

type ControllerManager struct {
	ctx                      context.Context
	restConfig               *rest.Config
	controllerRuntimeManager manager.Manager
	completeConfig           *config.CompletedConfig
	informerFactory          externalversions.SharedInformerFactory
	kahuClient               versioned.Interface
}

func NewControllerManager(ctx context.Context,
	completeConfig *config.CompletedConfig,
	clientFactory client.Factory,
	informerFactory externalversions.SharedInformerFactory) (*ControllerManager, error) {

	scheme := runtime.NewScheme()
	kahuv1beta1.AddToScheme(scheme)

	clientConfig, err := clientFactory.ClientConfig()
	if err != nil {
		return nil, err
	}

	ctrlRuntimeManager, err := ctrl.NewManager(clientConfig, ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}

	return &ControllerManager{
		ctx:                      ctx,
		restConfig:               clientConfig,
		controllerRuntimeManager: ctrlRuntimeManager,
		completeConfig:           completeConfig,
		kahuClient:               completeConfig.KahuClient,
		informerFactory:          informerFactory,
	}, nil
}

func (mgr *ControllerManager) InitControllers() (map[string]controllers.Controller, error) {
	availableControllers := make(map[string]controllers.Controller, 0)
	// add controllers here

	backupController, err := backup.NewController(&mgr.completeConfig.BackupControllerConfig,
		mgr.restConfig,
		mgr.kahuClient,
		mgr.informerFactory.Kahu().V1beta1().Backups())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize backup controller. %s", err)
	}
	availableControllers[backupController.Name()] = backupController

	return availableControllers, nil
}

func (mgr *ControllerManager) RemoveDisabledControllers(controllers map[string]controllers.Controller) error {
	for _, controllerName := range mgr.completeConfig.DisableControllers {
		if _, ok := controllers[controllerName]; ok {
			log.Infof("Disabling controller: %s", controllerName)
			delete(controllers, controllerName)
		}
	}
	return nil
}

func (mgr *ControllerManager) RunControllers(controllers map[string]controllers.Controller) error {
	for _, controller := range controllers {
		mgr.controllerRuntimeManager.
			Add(manager.RunnableFunc(
				func(ctx context.Context) error {
					return controller.Run(ctx, mgr.completeConfig.ControllerWorkers)
				}))
	}

	log.Info("Server starting...")
	return mgr.controllerRuntimeManager.Start(mgr.ctx)
}
