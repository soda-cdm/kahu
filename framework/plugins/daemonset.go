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
	appv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/utils/k8sresource"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

type DaemonSetPlugin struct {
	kubeCli         kubernetes.Interface
	logger          log.FieldLogger
	dynamicClient   dynamic.Interface
	discoveryHelper discovery.DiscoveryHelper
}

func newDaemonSetPlugin(kubeCli kubernetes.Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper) framework.Plugin {
	return &DaemonSetPlugin{
		kubeCli:         kubeCli,
		logger:          log.WithField("plugin", k8sresource.DaemonSetGVK.String()),
		dynamicClient:   dynamicClient,
		discoveryHelper: discoveryHelper,
	}
}

func (plugin *DaemonSetPlugin) For() (schema.GroupVersionKind, framework.Stages) {
	return k8sresource.DaemonSetGVK, framework.WithStages(framework.PreBackup)
}

func (plugin *DaemonSetPlugin) Name() string {
	return k8sresource.DaemonSetGVK.String()
}

// get all dependencies of daemonSet
func (plugin *DaemonSetPlugin) Execute(ctx context.Context,
	stage framework.Stage,
	resource k8sresource.Resource) (k8sresource.Resource, []k8sresource.Resource, error) {
	plugin.logger.Infof("Processing plugin for %s", resource.GetName())
	var ds appv1.DaemonSet
	err := k8sresource.FromResource(resource, &ds)
	if err != nil {
		return resource, nil, err
	}

	deps, err := plugin.getPods(ctx, &ds)
	if err != nil {
		return resource, nil, err
	}

	return resource, deps, nil
}

func (plugin *DaemonSetPlugin) getPods(ctx context.Context, ds *appv1.DaemonSet) ([]k8sresource.Resource, error) {
	plugin.logger.Infof("Fetching daemonset[%s/%s] pods", ds.Namespace, ds.Name)

	selector, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
	if err != nil {
		plugin.logger.Errorf("Failed to translate daemonset[%s/%s] selector", ds.Namespace, ds.Name)
		return nil, err
	}

	pods, err := plugin.kubeCli.CoreV1().Pods(ds.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		plugin.logger.Errorf("Failed to fetch daemonset[%s] pods. %s", ds.Namespace, ds.Name, err)
		return nil, err
	}

	resources := make([]k8sresource.Resource, 0)
	for _, pod := range pods.Items {
		k8sResource, err := k8sresource.ToResource(&pod)
		if err != nil {
			plugin.logger.Errorf("Failed to translate pod into k8s resources. %s", err)
			return resources, err
		}

		k8sResource.SetAPIVersion(schema.GroupVersion{
			Group:   k8sresource.PodGVK.Group,
			Version: k8sresource.PodGVK.Version,
		}.String())
		k8sResource.SetKind(k8sresource.PodGVK.Kind)
		resources = append(resources, k8sResource)
	}

	return resources, nil
}
