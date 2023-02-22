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
	appv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

type ReplicaSetPlugin struct {
	kubeCli         kubernetes.Interface
	logger          log.FieldLogger
	dynamicClient   dynamic.Interface
	discoveryHelper discovery.DiscoveryHelper
}

func newReplicaSetPlugin(kubeCli kubernetes.Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper) framework.Plugin {
	return &ReplicaSetPlugin{
		kubeCli:         kubeCli,
		logger:          log.WithField("plugin", k8sresource.ReplicaSetGVK.String()),
		dynamicClient:   dynamicClient,
		discoveryHelper: discoveryHelper,
	}
}

func (plugin *ReplicaSetPlugin) For() (schema.GroupVersionKind, framework.Stages) {
	return k8sresource.ReplicaSetGVK, framework.WithStages(framework.PreBackup)
}

func (plugin *ReplicaSetPlugin) Name() string {
	return k8sresource.ReplicaSetGVK.String()
}

// get all dependencies of replicaSet
func (plugin *ReplicaSetPlugin) Execute(ctx context.Context,
	_ framework.Stage,
	resource k8sresource.Resource) (k8sresource.Resource, []k8sresource.Resource, error) {
	plugin.logger.Infof("Processing plugin for %s", resource.GetName())
	var rs appv1.ReplicaSet
	err := k8sresource.FromResource(resource, &rs)
	if err != nil {
		return resource, nil, err
	}

	deps, err := plugin.getPods(ctx, &rs)
	if err != nil {
		return resource, nil, err
	}

	return resource, deps, nil
}

func (plugin *ReplicaSetPlugin) getPods(ctx context.Context, rs *appv1.ReplicaSet) ([]k8sresource.Resource, error) {
	plugin.logger.Infof("Fetching replicaset[%s/%s] pods", rs.Namespace, rs.Name)

	selector, err := metav1.LabelSelectorAsSelector(rs.Spec.Selector)
	if err != nil {
		plugin.logger.Errorf("Failed to translate replicaset[%s/%s] selector", rs.Namespace, rs.Name)
		return nil, err
	}

	pods, err := plugin.kubeCli.CoreV1().Pods(rs.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		plugin.logger.Errorf("Failed to fetch replicaset[%s] pods. %s", rs.Namespace, rs.Name, err)
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
