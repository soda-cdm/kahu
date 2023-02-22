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

type ClusterRoleBindingPlugin struct {
	kubeCli kubernetes.Interface
	logger  log.FieldLogger
}

func newClusterRoleBindingPlugin(kubeCli kubernetes.Interface) framework.Plugin {
	return &ClusterRoleBindingPlugin{
		kubeCli: kubeCli,
		logger:  log.WithField("plugin", k8sresource.ClusterRoleBindingGVK.String()),
	}
}

func (plugin *ClusterRoleBindingPlugin) For() (schema.GroupVersionKind, framework.Stages) {
	return k8sresource.ClusterRoleBindingGVK, framework.WithStages(framework.PreBackup)
}

func (plugin *ClusterRoleBindingPlugin) Name() string {
	return k8sresource.ClusterRoleBindingGVK.String()
}

// get all dependencies of clusterRoleBinding
func (plugin *ClusterRoleBindingPlugin) Execute(ctx context.Context,
	stage framework.Stage,
	resource k8sresource.Resource) (k8sresource.Resource, []k8sresource.Resource, error) {
	var crb rbacv1.ClusterRoleBinding
	deps := make([]k8sresource.Resource, 0)
	err := k8sresource.FromResource(resource, &crb)
	if err != nil {
		return resource, nil, err
	}

	clusterRole, err := plugin.kubeCli.RbacV1().ClusterRoles().Get(ctx, crb.RoleRef.Name, metav1.GetOptions{})
	if err != nil {
		plugin.logger.Errorf("Failed to retrieve clusterrole[%s] for clusterrolebinding[%s]",
			crb.RoleRef.Name, crb.Name)
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

	return resource, deps, nil
}
