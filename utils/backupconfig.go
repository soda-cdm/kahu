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

package utils

import (
	"fmt"


	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
)

func GetBackupParameters(
	logger log.FieldLogger,
	backupName,
	backupLocationName string,
	backupLocationLister kahulister.BackupLocationLister,
	backupProviderLister kahulister.ProviderLister) (map[string]string, error) {
	parameters := make(map[string]string)

	backupLocation, err := fetchBackupLocation(logger, backupLocationName, backupLocationLister)
	if err != nil {
		return parameters, errors.Wrap(err, fmt.Sprintf("unable to get backup location for %s",
			backupName))
	}

	backupProvider, err := fetchProvider(logger, backupLocation.Spec.ProviderName, backupProviderLister)
	if err != nil {
		return parameters, errors.Wrap(err, fmt.Sprintf("unable to get backup location for %s",
			backupName))
	}
	// populate Provider manifests
	for key, value := range backupProvider.Spec.Manifest {
		parameters[key] = value
	}
	// populate Backup location config
	for key, value := range backupLocation.Spec.Config {
		parameters[key] = value
	}

	return parameters, nil
}

func fetchBackupLocation(
	logger log.FieldLogger,
	locationName string,
	backupLocationLister kahulister.BackupLocationLister) (*kahuapi.BackupLocation, error) {
	// fetch backup location
	backupLocation, err := backupLocationLister.Get(locationName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Errorf("Backup location(%s) do not exist", locationName)
			return nil, err
		}
		logger.Errorf("Failed to get backup location. %s", err)
		return nil, err
	}

	return backupLocation, err
}

func fetchProvider(
	logger log.FieldLogger,
	providerName string,
	backupProviderLister kahulister.ProviderLister) (*kahuapi.Provider, error) {
	// fetch provider
	provider, err := backupProviderLister.Get(providerName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Errorf("Metadata Provider(%s) do not exist", providerName)
			return nil, err
		}
		logger.Errorf("Failed to get metadata provider. %s", err)
		return nil, err
	}

	return provider, nil
}
