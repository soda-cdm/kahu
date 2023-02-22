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
	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/framework"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

func LoadPlugins(kubeClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper,
	registry framework.Registry) error {
	// register cluster role binding plugin
	if err := registry.Register(newClusterRoleBindingPlugin(kubeClient)); err != nil {
		return err
	}

	// register cronjob plugin
	if err := registry.Register(newCronJobPlugin(kubeClient, dynamicClient, discoveryHelper)); err != nil {
		return err
	}

	// register daemonset plugin
	if err := registry.Register(newDaemonSetPlugin(kubeClient, dynamicClient, discoveryHelper)); err != nil {
		return err
	}

	// register deployment plugin
	if err := registry.Register(newDeploymentPlugin(kubeClient, dynamicClient, discoveryHelper)); err != nil {
		return err
	}

	// register persistent volume plugin
	if err := registry.Register(newPersistentVolumePlugin(kubeClient)); err != nil {
		return err
	}

	// register persistentvolumeclaim plugin
	if err := registry.Register(newPersistentVolumeClaimPlugin(kubeClient)); err != nil {
		return err
	}

	// register pod plugin
	if err := registry.Register(newPodPlugin(kubeClient, dynamicClient, discoveryHelper)); err != nil {
		return err
	}

	// register replicaset plugin
	if err := registry.Register(newReplicaSetPlugin(kubeClient, dynamicClient, discoveryHelper)); err != nil {
		return err
	}

	// register rolebinding plugin
	if err := registry.Register(newRoleBindingPlugin(kubeClient)); err != nil {
		return err
	}

	// register service account plugin
	if err := registry.Register(newServiceAccountPlugin(kubeClient)); err != nil {
		return err
	}

	// register statefulset plugin
	if err := registry.Register(newStatefulSetPlugin(kubeClient, dynamicClient, discoveryHelper)); err != nil {
		return err
	}

	return nil
}
