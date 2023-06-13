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

package plugins

import (
	"context"
	log "github.com/sirupsen/logrus"
	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/utils/k8sresource"
	batch "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"k8s.io/client-go/kubernetes"
)

type CronJobPlugin struct {
	kubeCli         kubernetes.Interface
	logger          log.FieldLogger
	dynamicClient   dynamic.Interface
	discoveryHelper discovery.DiscoveryHelper
}

func newCronJobPlugin(kubeCli kubernetes.Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper) framework.Plugin {
	return &CronJobPlugin{
		kubeCli:         kubeCli,
		logger:          log.WithField("plugin", k8sresource.PodGVK.String()),
		dynamicClient:   dynamicClient,
		discoveryHelper: discoveryHelper,
	}
}

func (plugin *CronJobPlugin) For() (schema.GroupVersionKind, framework.Stages) {
	return k8sresource.CronJobGVK, framework.WithStages(framework.PreBackup)
}

func (plugin *CronJobPlugin) Name() string {
	return k8sresource.CronJobGVK.String()
}

// get all dependencies of cronJob
func (plugin *CronJobPlugin) Execute(ctx context.Context,
	stage framework.Stage,
	resource k8sresource.Resource) (k8sresource.Resource, []k8sresource.Resource, error) {
	var cj batch.CronJob
	err := k8sresource.FromResource(resource, &cj)
	if err != nil {
		return resource, nil, err
	}

	depRefs, err := getPodDependencies(cj.Namespace, cj.Spec.JobTemplate.Spec.Template.Spec)
	if err != nil {
		return resource, nil, err
	}

	deps, err := fetchResources(ctx, depRefs, plugin.dynamicClient, plugin.discoveryHelper)
	if err != nil {
		return resource, nil, err
	}

	return resource, deps, nil
}
