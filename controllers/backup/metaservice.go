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
	"fmt"

	"google.golang.org/grpc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"github.com/soda-cdm/kahu/utils"
)

func (ctrl *controller) fetchBackupLocation(
	locationName string) (*kahuapi.BackupLocation, error) {
	// fetch backup location
	backupLocation, err := ctrl.backupLocationLister.Get(locationName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Errorf("Backup location(%s) do not exist", locationName)
			return nil, err
		}
		ctrl.logger.Errorf("Failed to get backup location. %s", err)
		return nil, err
	}

	return backupLocation, err
}

func (ctrl *controller) fetchProvider(
	providerName string) (*kahuapi.Provider, error) {
	// fetch provider
	provider, err := ctrl.providerLister.Get(providerName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Errorf("Metadata Provider(%s) do not exist", providerName)
			return nil, err
		}
		ctrl.logger.Errorf("Failed to get metadata provider. %s", err)
		return nil, err
	}

	return provider, nil
}

func (ctrl *controller) fetchMetaServiceClient(
	location string) (metaservice.MetaServiceClient, *grpc.ClientConn, error) {
	backLocation, err := ctrl.fetchBackupLocation(location)
	if err != nil {
		return nil, nil, err
	}

	backupProvider, err := ctrl.fetchProvider(backLocation.Spec.ProviderName)
	if err != nil {
		return nil, nil, err
	}

	if backupProvider.Spec.Type != kahuapi.ProviderTypeMetadata {
		return nil, nil, fmt.Errorf("invalid metadata provider type (%s)",
			backupProvider.Spec.Type)
	}

	// fetch service name
	providerService, exist := backupProvider.Annotations[utils.BackupLocationServiceAnnotation]
	if !exist {
		return nil, nil, fmt.Errorf("failed to get metadata provider(%s) service info",
			backupProvider.Name)
	}

	metaServiceClient, grpcConn, err := metaservice.GetMetaServiceClient(providerService)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get metadata service client(%s)",
			providerService)
	}

	return metaServiceClient, grpcConn, nil
}
