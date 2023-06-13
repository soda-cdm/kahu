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
	"github.com/soda-cdm/kahu/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/framework/executor"
	"github.com/soda-cdm/kahu/framework/plugins"
	"github.com/soda-cdm/kahu/framework/registry"
)

type factory struct {
	registry framework.Registry
	executor executor.Interface
	cfg      framework.Config
}

func NewFramework(ctx context.Context,
	cfg framework.Config,
	kubeClient kubernetes.Interface,
	kahuClient kahuv1client.KahuV1beta1Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper,
	eventBroadcaster record.EventBroadcaster) (framework.Interface, error) {
	reg := registry.NewRegistry()
	if err := plugins.LoadPlugins(kubeClient, dynamicClient, discoveryHelper, reg); err != nil {
		return nil, err
	}

	return &factory{
		registry: reg,
		executor: executor.NewExecutor(ctx, cfg.Executor, kubeClient, kahuClient, eventBroadcaster),
	}, nil
}

func (f *factory) PluginRegistry() framework.Registry {
	return f.registry
}

func (f *factory) Executors() executor.Interface {
	return f.executor
}
