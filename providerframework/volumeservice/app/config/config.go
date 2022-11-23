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
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/soda-cdm/kahu/client"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/client/informers/externalversions"
)

const (
	volumeServiceClientAgent = "kahu-volume-service"
)

type Config struct {
	ControllerWorkers    int               `json:"controllerWorkers"`
	EnableLeaderElection bool              `json:"enableLeaderElection"`
	DriverEndpoint       string            `json:"driverEndpoint"`
	DisableControllers   []string          `json:"disableControllers"`
	KahuClientConfig     client.Config     `json:",inline"`
	Provider             string            `json:"provider"`
	Version              string            `json:"version,omitempty"`
	Manifest             map[string]string `json:"manifest,omitempty"`
	LeaderLockNamespace  string            `json:"leaderLockNamespace,omitempty"`
	LeaderLeaseDuration  time.Duration
	LeaderRenewDeadline  time.Duration
	LeaderRetryPeriod    time.Duration
}

type CompletedConfig struct {
	*Config
	ClientFactory    client.Factory
	KubeClient       kubernetes.Interface
	KahuClient       versioned.Interface
	InformerFactory  externalversions.SharedInformerFactory
	EventBroadcaster record.EventBroadcaster
}

func (cfg *Config) Complete() (*CompletedConfig, error) {
	clientFactory := client.NewFactory(volumeServiceClientAgent, &cfg.KahuClientConfig)

	kubeClient, err := clientFactory.KubeClient()
	if err != nil {
		return nil, err
	}

	kahuClient, err := clientFactory.KahuClient()
	if err != nil {
		return nil, err
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(
		&corev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})

	return &CompletedConfig{
		Config:           cfg,
		ClientFactory:    clientFactory,
		KubeClient:       kubeClient,
		KahuClient:       kahuClient,
		EventBroadcaster: eventBroadcaster,
		InformerFactory:  externalversions.NewSharedInformerFactoryWithOptions(kahuClient, 0),
	}, nil
}

func (cfg *CompletedConfig) Print() {
	printPretty, err := json.MarshalIndent(cfg.Config, "", "    ")
	if err != nil {
		log.Infof("%+v", *cfg)
		return
	}
	log.Infof("%s", string(printPretty))
}
