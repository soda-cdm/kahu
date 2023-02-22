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
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/client-go/kubernetes"
)

type RoleBindingPlugin struct {
	kubeCli kubernetes.Interface
	logger  log.FieldLogger
}

func newRoleBindingPlugin(kubeCli kubernetes.Interface) framework.Plugin {
	return &RoleBindingPlugin{
		kubeCli: kubeCli,
		logger:  log.WithField("plugin", k8sresource.RoleBindingGVK.String()),
	}
}

func (plugin *RoleBindingPlugin) For() (schema.GroupVersionKind, framework.Stages) {
	return k8sresource.RoleBindingGVK, framework.WithStages(framework.PreBackup)
}

func (plugin *RoleBindingPlugin) Name() string {
	return k8sresource.RoleBindingGVK.String()
}

// get all dependencies of roleBinding
func (plugin *RoleBindingPlugin) Execute(ctx context.Context,
	stage framework.Stage,
	resource k8sresource.Resource) (k8sresource.Resource, []k8sresource.Resource, error) {
	var rb rbacv1.RoleBinding
	deps := make([]k8sresource.Resource, 0)
	err := k8sresource.FromResource(resource, &rb)
	if err != nil {
		return resource, nil, err
	}

	switch rb.RoleRef.Kind {
	case k8sresource.ClusterRoleGVK.Kind:
		clusterRole, err := plugin.kubeCli.RbacV1().ClusterRoles().Get(ctx, rb.RoleRef.Name, metav1.GetOptions{})
		if err != nil {
			plugin.logger.Errorf("Failed to retrieve clusterrole[%s] for rolebinding[%s]",
				rb.RoleRef.Name, rb.Name)
			return resource, deps, err
		}
		k8sResource, err := k8sresource.ToResource(clusterRole)
		if err != nil {
			plugin.logger.Errorf("Failed to translate clusterrole into k8s resources. %s", err)
			return resource, deps, err
		}

		k8sResource.SetAPIVersion(schema.GroupVersion{
			Group:   k8sresource.ClusterRoleGVK.Group,
			Version: k8sresource.ClusterRoleGVK.Version,
		}.String())
		k8sResource.SetKind(k8sresource.ClusterRoleGVK.Kind)
		deps = append(deps, k8sResource)
	case k8sresource.RoleGVK.Kind:
		role, err := plugin.kubeCli.RbacV1().Roles(rb.Namespace).Get(ctx, rb.RoleRef.Name, metav1.GetOptions{})
		if err != nil {
			plugin.logger.Errorf("Failed to retrieve clusterrole[%s] for rolebinding[%s]",
				rb.RoleRef.Name, rb.Name)
			return resource, deps, err
		}
		k8sResource, err := k8sresource.ToResource(role)
		if err != nil {
			plugin.logger.Errorf("Failed to translate clusterrole into k8s resources. %s", err)
			return resource, deps, err
		}

		k8sResource.SetAPIVersion(schema.GroupVersion{
			Group:   k8sresource.RoleGVK.Group,
			Version: k8sresource.RoleGVK.Version,
		}.String())
		k8sResource.SetKind(k8sresource.RoleGVK.Kind)
		deps = append(deps, k8sResource)
	default:
		return resource, nil, nil
	}

	return resource, deps, nil
}
