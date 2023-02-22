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

package executor

import (
	"context"
	"fmt"
	"time"

	kahuscheme "github.com/soda-cdm/kahu/client/clientset/versioned/scheme"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	"github.com/soda-cdm/kahu/framework/executor/resourcebackup"
	"github.com/soda-cdm/kahu/framework/executor/volumebackup"
	"github.com/soda-cdm/kahu/utils"
)

const (
	defaultSyncTime = 60 * time.Second
)

type executor struct {
	ctx           context.Context
	cfg           Config
	logger        log.FieldLogger
	kubeClient    kubernetes.Interface
	kahuClient    kahuv1client.KahuV1beta1Interface
	rsCollection  *resourcebackup.StoreCollection
	volCollection *volumebackup.StoreCollection
	eventRecorder record.EventRecorder
}

func NewExecutor(ctx context.Context,
	cfg Config,
	kubeClient kubernetes.Interface,
	kahuClient kahuv1client.KahuV1beta1Interface,
	eventBroadcaster record.EventBroadcaster) Interface {
	eventRecorder := eventBroadcaster.NewRecorder(kahuscheme.Scheme,
		corev1.EventSource{Component: "volume-backup"})
	executor := &executor{
		ctx:           ctx,
		cfg:           cfg,
		logger:        log.WithField("module", "executor"),
		kubeClient:    kubeClient,
		kahuClient:    kahuClient,
		rsCollection:  resourcebackup.NewStore(),
		volCollection: volumebackup.NewStore(),
		eventRecorder: eventRecorder,
	}

	// run goroutine for executor sync
	go wait.UntilWithContext(ctx, executor.sync, defaultSyncTime)
	return executor
}

func (exec *executor) ResourceBackupService(location string) (resourcebackup.Service, error) {
	service, ok := exec.rsCollection.Get(location)
	if !ok {
		// ensure resource backup service
		err := exec.Install(exec.ctx, location)
		if err != nil {
			return nil, fmt.Errorf("unable to ensure resource store for %s", location)
		}

		// get service
		service, ok := exec.rsCollection.Get(location)
		if !ok {
			return nil, fmt.Errorf("unable to get resource store for %s", location)
		}
		return service, nil
	}
	return service, nil
}

func (exec *executor) VolumeBackupService(location string) (volumebackup.Service, error) {
	service, ok := exec.volCollection.Get(location)
	if !ok {
		// ensure resource backup service
		err := exec.Install(exec.ctx, location)
		if err != nil {
			return nil, fmt.Errorf("unable to ensure resource store for %s", location)
		}

		// get service
		service, ok := exec.volCollection.Get(location)
		if !ok {
			return nil, fmt.Errorf("unable to get resource store for %s", location)
		}
		return service, nil
	}
	return service, nil
}

func (exec *executor) Install(ctx context.Context, location string) error {
	exec.logger.Infof("Installing backup location [%s]", location)
	bl, err := exec.kahuClient.BackupLocations().Get(ctx, location, metav1.GetOptions{})
	if err != nil {
		return err
	}

	provider, err := exec.kahuClient.Providers().Get(ctx, bl.Spec.ProviderName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if exec.exist(bl, provider) {
		return nil
	}

	var registration *kahuapi.ProviderRegistration
	// get provider registration info
	if utils.ContainsOwnerReference(provider) &&
		utils.ContainsAnnotation(provider, utils.AnnProviderRegistrationUID) {
		uid, ok := utils.GetAnnotation(provider, utils.AnnProviderRegistrationUID)
		if !ok {
			exec.logger.Errorf("registration annotation not available in provider[%s]", provider.Name)
			return fmt.Errorf("registration annotation not available in provider[%s]", provider.Name)
		}

		ownerRef, ok := utils.GetOwnerReference(provider, uid)
		if !ok {
			exec.logger.Errorf("registration owner reference not available in provider[%s]", provider.Name)
			return fmt.Errorf("registration owner reference not available in provider[%s]", provider.Name)
		}

		registration, err = exec.kahuClient.ProviderRegistrations().Get(ctx, ownerRef.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
	}

	switch provider.Spec.Type {
	case kahuapi.ProviderTypeMetadata:
		return exec.installResourceBackupper(ctx, bl, provider, registration)
	case kahuapi.ProviderTypeVolume:
		return exec.installVolumeBackupper(ctx, bl, provider, registration)
	default:
		return fmt.Errorf("invalid provider %s", provider.Spec.Type)
	}
}

func (exec *executor) Uninstall(ctx context.Context, location string) error {
	exec.logger.Infof("Uninstalling backup location [%s]", location)

	bl, err := exec.kahuClient.BackupLocations().Get(ctx, location, metav1.GetOptions{})
	if err != nil {
		return err
	}

	provider, err := exec.kahuClient.Providers().Get(ctx, bl.Spec.ProviderName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if exec.exist(bl, provider) {
		return nil
	}

	switch provider.Spec.Type {
	case kahuapi.ProviderTypeMetadata:
		return exec.uninstallResourceBackupper(location)
	case kahuapi.ProviderTypeVolume:
		return exec.uninstallVolumeBackupper(ctx, location)
	default:
		return fmt.Errorf("invalid provider %s", provider.Spec.Type)
	}

}

func (exec *executor) sync(_ context.Context) {
	exec.logger.Info("Started soft reconciliation of executors")
	for _, service := range exec.rsCollection.List() {
		go service.Sync()
	}

	for _, service := range exec.volCollection.List() {
		go service.Sync()
	}
}

func (exec *executor) exist(location *kahuapi.BackupLocation, provider *kahuapi.Provider) bool {
	switch provider.Spec.Type {
	case kahuapi.ProviderTypeMetadata:
		_, ok := exec.rsCollection.Get(location.Name)
		return ok
	case kahuapi.ProviderTypeVolume:
		return false
	}

	return false
}

func (exec *executor) installResourceBackupper(ctx context.Context,
	bl *kahuapi.BackupLocation,
	provider *kahuapi.Provider,
	registration *kahuapi.ProviderRegistration) error {
	service := resourcebackup.NewResourceBackupService(ctx,
		exec.cfg.ResourceBackup,
		exec.cfg.Namespace,
		bl,
		provider,
		registration,
		exec.kubeClient,
		exec.kahuClient)

	location := bl.Name

	exec.logger.Infof("Starting resource store [%s]", location)
	backupStore, err := service.Start(exec.ctx)
	if err != nil {
		return err
	}
	defer service.Done()
	exec.logger.Infof("Starting resource store [%s] success", location)

	ctx, cancel := context.WithTimeout(exec.ctx, 30*time.Second)
	defer cancel()
	exec.logger.Infof("Starting resource store [%s] probe", location)
	err = backupStore.Probe(ctx)
	if err != nil {
		exec.logger.Errorf("Failed to probe service. %s", err)
		return err
	}
	exec.logger.Infof("Resource store [%s] probe success", location)

	err = exec.rsCollection.Add(location, service)
	if err != nil {
		exec.logger.Errorf("Failed to add service in service store. %s", err)
	}
	return err
}

func (exec *executor) uninstallResourceBackupper(location string) error {
	service, ok := exec.rsCollection.Get(location)
	if !ok {
		return nil
	}

	err := service.Remove()
	if err != nil {
		exec.logger.Errorf("Unable to remove meta service for backup location[%s]. %s", location, err)
		return err
	}

	err = exec.rsCollection.Remove(location)
	if !ok {
		exec.logger.Errorf("Unable to uninstall meta service for location[%s]. %s", location, err)
		return err
	}

	return nil
}

func (exec *executor) installVolumeBackupper(ctx context.Context,
	bl *kahuapi.BackupLocation,
	provider *kahuapi.Provider,
	registration *kahuapi.ProviderRegistration) error {
	service := volumebackup.NewService(
		ctx,
		exec.cfg.VolumeBackup,
		exec.cfg.Namespace,
		bl,
		provider,
		registration,
		exec.kahuClient,
		exec.kubeClient,
		exec.eventRecorder,
	)

	ctx, cancel := context.WithTimeout(exec.ctx, 30*time.Second)
	defer cancel()
	exec.logger.Infof("Starting resource store [%s] probe", bl.Name)
	err := service.Probe(ctx)
	if err != nil {
		exec.logger.Errorf("Failed to probe service. %s", err)
		return err
	}
	exec.logger.Infof("Resource store [%s] probe success", bl.Name)

	err = exec.volCollection.Add(bl.Name, service)
	if err != nil {
		exec.logger.Errorf("Failed to add service in service store. %s", err)
	}
	return err
}

func (exec *executor) uninstallVolumeBackupper(ctx context.Context, location string) error {
	service, ok := exec.volCollection.Get(location)
	if !ok {
		return nil
	}

	err := service.Cleanup(ctx)
	if err != nil {
		exec.logger.Errorf("Unable to remove meta service for backup location[%s]. %s", location, err)
		return err
	}

	err = exec.volCollection.Remove(location)
	if !ok {
		exec.logger.Errorf("Unable to uninstall meta service for location[%s]. %s", location, err)
		return err
	}

	return nil
}
