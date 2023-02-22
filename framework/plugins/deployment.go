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

type DeploymentPlugin struct {
	kubeCli         kubernetes.Interface
	logger          log.FieldLogger
	dynamicClient   dynamic.Interface
	discoveryHelper discovery.DiscoveryHelper
}

func newDeploymentPlugin(kubeCli kubernetes.Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper) framework.Plugin {
	return &DeploymentPlugin{
		kubeCli:         kubeCli,
		logger:          log.WithField("plugin", k8sresource.DeploymentGVK.String()),
		dynamicClient:   dynamicClient,
		discoveryHelper: discoveryHelper,
	}
}

func (plugin *DeploymentPlugin) For() (schema.GroupVersionKind, framework.Stages) {
	return k8sresource.DeploymentGVK, framework.WithStages(framework.PreBackup)
}

func (plugin *DeploymentPlugin) Name() string {
	return k8sresource.DeploymentGVK.String()
}

// get all dependencies of deployment
func (plugin *DeploymentPlugin) Execute(ctx context.Context,
	stage framework.Stage,
	resource k8sresource.Resource) (k8sresource.Resource, []k8sresource.Resource, error) {
	plugin.logger.Infof("Processing plugin for %s", resource.GetName())
	var deploy appv1.Deployment
	err := k8sresource.FromResource(resource, &deploy)
	if err != nil {
		return resource, nil, err
	}

	deps, err := plugin.getReplicaset(ctx, &deploy)
	if err != nil {
		return resource, nil, err
	}

	return resource, deps, nil
}

func (plugin *DeploymentPlugin) getReplicaset(ctx context.Context, deploy *appv1.Deployment) ([]k8sresource.Resource, error) {
	plugin.logger.Infof("Fetching deployment[%s/%s] replicaset", deploy.Namespace, deploy.Name)

	selector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		plugin.logger.Errorf("Failed to translate deployment[%s/%s] selector", deploy.Namespace, deploy.Name)
		return nil, err
	}

	list, err := plugin.kubeCli.AppsV1().ReplicaSets(deploy.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		plugin.logger.Errorf("Failed to fetch deployment[%s] replicaset. %s", deploy.Namespace, deploy.Name, err)
		return nil, err
	}

	resources := make([]k8sresource.Resource, 0)
	for _, rs := range list.Items {
		k8sResource, err := k8sresource.ToResource(&rs)
		if err != nil {
			plugin.logger.Errorf("Failed to translate replicaset into k8s resources. %s", err)
			return resources, err
		}

		k8sResource.SetAPIVersion(schema.GroupVersion{
			Group:   k8sresource.ReplicaSetGVK.Group,
			Version: k8sresource.ReplicaSetGVK.Version,
		}.String())
		k8sResource.SetKind(k8sresource.ReplicaSetGVK.Kind)

		resources = append(resources, k8sResource)
	}

	return resources, nil
}
