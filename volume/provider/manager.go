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
	"fmt"
	"sync"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
)

type Interface interface {
	GetNameByProvisioner(provisioner string) (string, error)
	SyncProvider(provider *kahuapi.Provider) error
	RemoveProvider(provider *kahuapi.Provider) error
	DefaultProvider() (string, error)
	SetDefaultProvider(provider *kahuapi.Provider) error
}

type manager struct {
	sync.RWMutex

	defaultProvider string
	// map of supported provisioner
	supportedProvisioner map[string]string
}

func NewFactory() Interface {
	return &manager{
		supportedProvisioner: make(map[string]string),
	}
}

func (m *manager) GetNameByProvisioner(provisioner string) (string, error) {
	m.RLock()
	defer m.RUnlock()
	provider, ok := m.supportedProvisioner[provisioner]
	if !ok {
		if provider, ok := m.supportedProvisioner[kahuapi.SupportAllProvisioner]; ok {
			return provider, nil
		}
		return "", fmt.Errorf("volume data protection provider not available "+
			"for volume provisioner[%s]", provisioner)
	}
	return provider, nil
}

func (m *manager) DefaultProvider() (string, error) {
	m.RLock()
	defer m.RUnlock()
	if m.defaultProvider == "" {
		if provider, ok := m.supportedProvisioner[kahuapi.SupportAllProvisioner]; ok {
			return provider, nil
		}
		return "", fmt.Errorf("volume data protection default provider not available")
	}
	return m.defaultProvider, nil
}

func (m *manager) SyncProvider(provider *kahuapi.Provider) error {
	m.Lock()
	defer m.Unlock()
	if provider.Spec.Type != kahuapi.ProviderTypeVolume ||
		provider.Spec.SupportedProvisioner == nil {
		return nil
	}

	provisioner := *provider.Spec.SupportedProvisioner
	current, ok := m.supportedProvisioner[provisioner]
	if ok && current != provider.Name {
		return fmt.Errorf("volume service already available for volume provisioner[%s]", provisioner)
	}

	m.supportedProvisioner[provisioner] = provider.Name
	return nil
}

func (m *manager) SetDefaultProvider(provider *kahuapi.Provider) error {
	m.Lock()
	defer m.Unlock()
	if provider.Spec.Type != kahuapi.ProviderTypeVolume {
		return nil
	}

	if m.defaultProvider != "" {
		return fmt.Errorf("default volume provider already available")
	}

	m.defaultProvider = provider.Name
	return nil
}

func (m *manager) RemoveProvider(provider *kahuapi.Provider) error {
	m.Lock()
	defer m.Unlock()
	if provider.Spec.Type != kahuapi.ProviderTypeVolume {
		return nil
	}

	if m.defaultProvider == provider.Name {
		m.defaultProvider = ""
	}

	for provisioner, providerName := range m.supportedProvisioner {
		if providerName == provider.Name {
			delete(m.supportedProvisioner, provisioner)
			break
		}
	}
	return nil
}
