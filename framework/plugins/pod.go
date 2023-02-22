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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

type PodPlugin struct {
	logger          *log.Entry
	kubeCli         kubernetes.Interface
	dynamicClient   dynamic.Interface
	discoveryHelper discovery.DiscoveryHelper
}

func newPodPlugin(kubeCli kubernetes.Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper) framework.Plugin {
	return &PodPlugin{
		logger:          log.WithField("plugin", k8sresource.PodGVK.String()),
		kubeCli:         kubeCli,
		dynamicClient:   dynamicClient,
		discoveryHelper: discoveryHelper,
	}
}

func (_ *PodPlugin) For() (schema.GroupVersionKind, framework.Stages) {
	return k8sresource.PodGVK, framework.WithStages(framework.PreBackup)
}

func (_ *PodPlugin) Name() string {
	return k8sresource.PodGVK.String()
}

// get all dependencies of pod
func (plugin *PodPlugin) Execute(ctx context.Context,
	_ framework.Stage,
	resource k8sresource.Resource) (k8sresource.Resource, []k8sresource.Resource, error) {
	plugin.logger.Infof("Processing plugin for %s", resource.GetName())
	var pod corev1.Pod
	err := k8sresource.FromResource(resource, &pod)
	if err != nil {
		return resource, nil, err
	}

	depRefs, err := getPodDependencies(pod.Namespace, pod.Spec)
	if err != nil {
		return resource, nil, err
	}

	deps, err := fetchResources(ctx, depRefs, plugin.dynamicClient, plugin.discoveryHelper)
	if err != nil {
		return resource, nil, err
	}

	return resource, deps, nil
}
