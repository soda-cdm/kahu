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
	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/utils/k8sresource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/client-go/kubernetes"
)

type serviceAccountPlugin struct {
	kubeCli kubernetes.Interface
	logger  log.FieldLogger
}

func newServiceAccountPlugin(kubeCli kubernetes.Interface) framework.Plugin {
	return &serviceAccountPlugin{
		kubeCli: kubeCli,
		logger:  log.WithField("plugin", k8sresource.ServiceAccountGVK.String()),
	}
}

func (plugin *serviceAccountPlugin) For() (schema.GroupVersionKind, framework.Stages) {
	return k8sresource.ServiceAccountGVK, framework.WithStages(framework.PreBackup)
}

func (plugin *serviceAccountPlugin) Name() string {
	return k8sresource.ServiceAccountGVK.String()
}

// get all dependencies of sa
func (plugin *serviceAccountPlugin) Execute(ctx context.Context,
	stage framework.Stage,
	resource k8sresource.Resource) (k8sresource.Resource, []k8sresource.Resource, error) {
	plugin.logger.Infof("Processing plugin for %s", resource.GetName())
	var sa corev1.ServiceAccount
	deps := make([]k8sresource.Resource, 0)
	err := k8sresource.FromResource(resource, &sa)
	if err != nil {
		return resource, nil, err
	}

	resources, err := plugin.getClusterRoleBinding(ctx, sa.Name, sa.Namespace)
	if err != nil {
		return resource, deps, err
	}
	deps = append(deps, resources...)

	resources, err = plugin.getRoleBinding(ctx, sa.Name, sa.Namespace)
	if err != nil {
		return resource, deps, err
	}
	deps = append(deps, resources...)

	return resource, deps, nil
}

func (plugin *serviceAccountPlugin) getClusterRoleBinding(ctx context.Context,
	saName string,
	namespace string) (dep []k8sresource.Resource, err error) {
	plugin.logger.Infof("Fetching cluster role binding for %s", saName)
	resources := make([]k8sresource.Resource, 0)
	list, err := plugin.kubeCli.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return resources, err
	}

	for _, crb := range list.Items {
		for _, subject := range crb.Subjects {
			if subject.Kind == k8sresource.ServiceAccountGVK.Kind &&
				subject.Name == saName &&
				subject.Namespace == namespace {
				k8sResource, err := k8sresource.ToResource(&crb)
				if err != nil {
					plugin.logger.Errorf("Failed to translate clusterrolebinding into k8s resources. %s", err)
					return resources, err
				}

				k8sResource.SetAPIVersion(schema.GroupVersion{
					Group:   k8sresource.ClusterRoleBindingGVK.Group,
					Version: k8sresource.ClusterRoleBindingGVK.Version,
				}.String())
				k8sResource.SetKind(k8sresource.ClusterRoleBindingGVK.Kind)
				resources = append(resources, k8sResource)
				// found SA
				break
			}
		}

	}

	return resources, nil
}

func (plugin *serviceAccountPlugin) getRoleBinding(ctx context.Context,
	saName string,
	namespace string) (dep []k8sresource.Resource, err error) {
	resources := make([]k8sresource.Resource, 0)
	plugin.logger.Infof("Fetching role binding for %s", saName)
	list, err := plugin.kubeCli.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return resources, err
	}

	for _, rb := range list.Items {
		for _, subject := range rb.Subjects {
			if subject.Kind == k8sresource.ServiceAccountGVK.Kind &&
				subject.Name == saName {
				k8sResource, err := k8sresource.ToResource(&rb)
				if err != nil {
					plugin.logger.Errorf("Failed to translate rolebinding into k8s resources. %s", err)
					return resources, err
				}

				k8sResource.SetAPIVersion(schema.GroupVersion{
					Group:   k8sresource.RoleBindingGVK.Group,
					Version: k8sresource.RoleBindingGVK.Version,
				}.String())
				k8sResource.SetKind(k8sresource.RoleBindingGVK.Kind)
				resources = append(resources, k8sResource)
				// found SA
				break
			}
		}

	}

	return resources, nil
}
