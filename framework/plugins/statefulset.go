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
	appv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"k8s.io/client-go/kubernetes"
)

type StatefulSetPlugin struct {
	kubeCli         kubernetes.Interface
	logger          log.FieldLogger
	dynamicClient   dynamic.Interface
	discoveryHelper discovery.DiscoveryHelper
}

func newStatefulSetPlugin(kubeCli kubernetes.Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper) framework.Plugin {
	return &StatefulSetPlugin{
		kubeCli:         kubeCli,
		logger:          log.WithField("plugin", k8sresource.StatefulSetGVK.String()),
		dynamicClient:   dynamicClient,
		discoveryHelper: discoveryHelper,
	}
}

func (plugin *StatefulSetPlugin) For() (schema.GroupVersionKind, framework.Stages) {
	return k8sresource.StatefulSetGVK, framework.WithStages(framework.PreBackup)
}

func (plugin *StatefulSetPlugin) Name() string {
	return k8sresource.StatefulSetGVK.String()
}

// get all dependencies of statefulSet
func (plugin *StatefulSetPlugin) Execute(ctx context.Context,
	_ framework.Stage,
	resource k8sresource.Resource) (k8sresource.Resource, []k8sresource.Resource, error) {
	var ss appv1.StatefulSet
	err := k8sresource.FromResource(resource, &ss)
	if err != nil {
		return resource, nil, err
	}

	deps, err := plugin.getPods(ctx, &ss)
	if err != nil {
		return resource, nil, err
	}

	return resource, deps, nil
}

func (plugin *StatefulSetPlugin) getPods(ctx context.Context, ss *appv1.StatefulSet) ([]k8sresource.Resource, error) {
	plugin.logger.Infof("Fetching statefulset[%s/%s] pods", ss.Namespace, ss.Name)

	selector, err := metav1.LabelSelectorAsSelector(ss.Spec.Selector)
	if err != nil {
		plugin.logger.Errorf("Failed to translate statefulset[%s/%s] selector", ss.Namespace, ss.Name)
		return nil, err
	}

	pods, err := plugin.kubeCli.CoreV1().Pods(ss.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		plugin.logger.Errorf("Failed to fetch statefulset[%s] pods. %s", ss.Namespace, ss.Name, err)
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
