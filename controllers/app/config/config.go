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
	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/client/informers/externalversions"

	"github.com/soda-cdm/kahu/client"
	"github.com/soda-cdm/kahu/controllers/backup"
)

const (
	controllerManagerClientAgent = "controller-manager"
	eventComponentName           = "kahu-controller-manager"
)

type Config struct {
	ControllerWorkers      int
	EnableLeaderElection   bool
	DisableControllers     []string
	KahuClientConfig       client.Config
	BackupControllerConfig backup.Config
}

type CompletedConfig struct {
	*Config
	EventRecorder record.EventRecorder
	ClientFactory client.Factory
	KubeClient    kubernetes.Interface
	KahuClient    versioned.Interface
	KahuInformer  externalversions.SharedInformerFactory
}

func (cfg *Config) Complete() (*CompletedConfig, error) {
	clientFactory := client.NewFactory(controllerManagerClientAgent, &cfg.KahuClientConfig)

	kubeClient, err := clientFactory.KubeClient()
	if err != nil {
		return nil, err
	}

	kahuClient, err := clientFactory.KahuClient()
	if err != nil {
		return nil, err
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme,
		apiv1.EventSource{Component: eventComponentName})

	return &CompletedConfig{
		Config:        cfg,
		EventRecorder: recorder,
		ClientFactory: clientFactory,
		KubeClient:    kubeClient,
		KahuClient:    kahuClient,
		KahuInformer:  externalversions.NewSharedInformerFactoryWithOptions(kahuClient, 0),
	}, nil
}

func (cfg *CompletedConfig) Print() {
	log.Infof("Controller manager config %+v", cfg.Config)
}
