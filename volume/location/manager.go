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

package location

import (
	"fmt"
	"sync"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
)

type Interface interface {
	GetDefaultLocation(provider string) (string, error)
	SetDefaultLocation(location *kahuapi.BackupLocation) error
	RemoveLocation(location *kahuapi.BackupLocation)
}

type manager struct {
	sync.RWMutex

	// map of location amd provider provisioner
	defaultLocation map[string]string
}

func NewFactory() Interface {
	return &manager{
		defaultLocation: make(map[string]string),
	}
}

func (m *manager) GetDefaultLocation(provider string) (string, error) {
	m.RLock()
	defer m.RUnlock()
	location, ok := m.defaultLocation[provider]
	if !ok {
		return "", fmt.Errorf("default backup location not available for provider[%s]", provider)
	}
	return location, nil
}

func (m *manager) SetDefaultLocation(location *kahuapi.BackupLocation) error {
	m.Lock()
	defer m.Unlock()

	_, ok := m.defaultLocation[location.Spec.ProviderName]
	if ok {
		return fmt.Errorf("default backup location already available")
	}

	m.defaultLocation[location.Spec.ProviderName] = location.Name
	return nil
}

func (m *manager) RemoveLocation(location *kahuapi.BackupLocation) {
	m.Lock()
	defer m.Unlock()
	delete(m.defaultLocation, location.Spec.ProviderName)
}
