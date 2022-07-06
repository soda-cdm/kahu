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

package backup

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/utils"

	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
)

func (c *controller) getServices(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

	c.logger.Infoln("starting collecting services")
	k8sClinet, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()
	allServices, err := k8sClinet.CoreV1().Services(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}

	var allServicesList []string
	for _, sc := range allServices.Items {
		allServicesList = append(allServicesList, sc.Name)
	}

	allServicesList = utils.FindMatchedStrins("services", allServicesList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, service := range allServices.Items {
		if utils.Contains(allServicesList, service.Name) {
			serviceData, err := k8sClinet.CoreV1().Services(namespace).Get(context.TODO(), service.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			err = c.backupSend(serviceData, serviceData.Name, backupClient)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func (c *controller) getConfigMapS(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

	c.logger.Infoln("starting collecting configmaps")
	k8sClinet, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()
	configList, err := k8sClinet.CoreV1().ConfigMaps(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}
	var configAllLits []string
	for _, deployment := range configList.Items {
		configAllLits = append(configAllLits, deployment.Name)
	}

	configAllLits = utils.FindMatchedStrins("configmaps", configAllLits, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, item := range configList.Items {
		if utils.Contains(configAllLits, item.Name) {
			config_data, err := c.GetConfigMap(namespace, item.Name)

			err = c.backupSend(config_data, config_data.Name, backupClient)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func (c *controller) getSecrets(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

	c.logger.Infoln("starting collecting secrets")
	k8sClinet, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()
	secretList, err := k8sClinet.CoreV1().Secrets(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}

	var allSecretsList []string
	for _, sc := range secretList.Items {
		allSecretsList = append(allSecretsList, sc.Name)
	}

	allSecretsList = utils.FindMatchedStrins("secrets", allSecretsList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, secret := range secretList.Items {
		if utils.Contains(allSecretsList, secret.Name) {
			secretData, err := k8sClinet.CoreV1().Secrets(namespace).Get(context.TODO(), secret.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			err = c.backupSend(secretData, secret.Name, backupClient)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func (c *controller) getEndpoints(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

	c.logger.Infoln("starting collecting endpoints")
	k8sClinet, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()
	endpointList, err := k8sClinet.CoreV1().Endpoints(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}

	var allendpointList []string
	for _, sc := range endpointList.Items {
		allendpointList = append(allendpointList, sc.Name)
	}

	allendpointList = utils.FindMatchedStrins("endpoints", allendpointList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, endpoint := range endpointList.Items {
		if utils.Contains(allendpointList, endpoint.Name) {
			endpointData, err := k8sClinet.CoreV1().Endpoints(namespace).Get(context.TODO(), endpoint.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			err = c.backupSend(endpointData, endpointData.Name, backupClient)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *controller) getReplicasets(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

	c.logger.Infoln("starting collecting replicasets")
	k8sClinet, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()
	replicasetList, err := k8sClinet.AppsV1().ReplicaSets(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}

	var allreplicasetList []string
	for _, sc := range replicasetList.Items {
		allreplicasetList = append(allreplicasetList, sc.Name)
	}

	allreplicasetList = utils.FindMatchedStrins("replicasets", allreplicasetList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, replicaset := range replicasetList.Items {

		if utils.Contains(allreplicasetList, replicaset.Name) {
			replicasetData, err := k8sClinet.AppsV1().ReplicaSets(namespace).Get(context.TODO(), replicaset.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			err = c.backupSend(replicasetData, replicaset.Name, backupClient)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *controller) getStatefulsets(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

	c.logger.Infoln("starting collecting statefulsets")
	k8sClinet, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()
	statefulList, err := k8sClinet.AppsV1().StatefulSets(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}

	var allstatefulList []string
	for _, sc := range statefulList.Items {
		allstatefulList = append(allstatefulList, sc.Name)
	}

	allstatefulList = utils.FindMatchedStrins("statefulsets", allstatefulList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, stateful := range statefulList.Items {
		if utils.Contains(allstatefulList, stateful.Name) {
			statefulData, err := k8sClinet.AppsV1().StatefulSets(namespace).Get(context.TODO(), stateful.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			err = c.backupSend(statefulData, stateful.Name, backupClient)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
