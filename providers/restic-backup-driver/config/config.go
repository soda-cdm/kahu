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

package config

import (
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/client"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/providers/restic-backup-driver/options"
)

const (
	resticDriverClientName = "ResticDriver"
)

type config struct {
	options.ResticProviderFlags

	kahuClient clientset.Interface
	kubeClient kubernetes.Interface
}

type Config interface {
	GetUnixSocketPath() string
	GetProviderName() string
	GetProviderVersion() string
	GetKubeClient() kubernetes.Interface

	Complete() error
}

func NewConfig(flags *options.ResticProviderFlags) Config {
	return &config{
		ResticProviderFlags: *flags,
	}
}

func (cfg *config) GetUnixSocketPath() string {
	return cfg.ResticFlags.UnixSocketPath
}

func (cfg *config) GetProviderName() string {
	return cfg.ResticFlags.ProviderName
}

func (cfg *config) GetProviderVersion() string {
	return cfg.ResticFlags.ProviderVersion
}

func (cfg *config) GetKubeClient() kubernetes.Interface {
	return cfg.kubeClient
}

func (cfg *config) Complete() error {

	clientFactory := client.NewFactory(resticDriverClientName, cfg.KubeClientConfig)

	// in itialize kube client
	kubeClient, err := clientFactory.KubeClient()
	if err != nil {
		return err
	}
	cfg.kubeClient = kubeClient

	// in itialize kahu client
	kahuClient, err := clientFactory.KahuClient()
	if err != nil {
		return err
	}
	cfg.kahuClient = kahuClient

	return nil
}
