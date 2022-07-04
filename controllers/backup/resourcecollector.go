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
	"encoding/json"

	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

func (c *Controller) getServices(gvr GroupResouceVersion, namespace string, backup *PrepareBackup,
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

	for _, service := range allServices.Items {
		serviceData, err := k8sClinet.CoreV1().Services(namespace).Get(context.TODO(), service.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		resourceData, err := json.Marshal(serviceData)
		if err != nil {
			c.logger.Errorf("Unable to get resource content of service %s", err)
			return err
		}
		c.logger.Debug(resourceData)
		err = c.backupSend(gvr, resourceData, serviceData.Name, backupClient)
		if err != nil {
			return err
		}

	}
	return nil
}

func (c *Controller) getConfigMapS(gvr GroupResouceVersion, namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

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

	for _, configMap := range configList.Items {
		config_data, err := c.GetConfigMap(namespace, configMap.Name)

		resourceData, err := json.Marshal(config_data)
		if err != nil {
			c.logger.Errorf("Unable to get resource content of configmaps: %s", err)
			return err
		}
		c.logger.Debug(resourceData)

		err = c.backupSend(gvr, resourceData, config_data.Name, backupClient)
		if err != nil {
			return err
		}

	}
	return nil
}

func (c *Controller) getSecrets(gvr GroupResouceVersion, namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

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

	for _, secret := range secretList.Items {
		secretData, err := k8sClinet.CoreV1().Secrets(namespace).Get(context.TODO(), secret.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		resourceData, err := json.Marshal(secretData)
		if err != nil {
			c.logger.Errorf("Unable to get resource content of secret: %s", err)
			return err
		}
		c.logger.Debug(resourceData)

		err = c.backupSend(gvr, resourceData, secretData.Name, backupClient)
		if err != nil {
			return err
		}

	}
	return nil
}

func (c *Controller) getEndpoints(gvr GroupResouceVersion, namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

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

	for _, endpoint := range endpointList.Items {
		endpointData, err := k8sClinet.CoreV1().Endpoints(namespace).Get(context.TODO(), endpoint.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		resourceData, err := json.Marshal(endpointData)
		if err != nil {
			c.logger.Errorf("Unable to get resource content of endpoint: %s", err)
			return err
		}
		c.logger.Debug(resourceData)

		err = c.backupSend(gvr, resourceData, endpointData.Name, backupClient)
		if err != nil {
			return err
		}

	}
	return nil
}

func (c *Controller) getReplicasets(gvr GroupResouceVersion, namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

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

	for _, replicaset := range replicasetList.Items {
		replicasetData, err := k8sClinet.AppsV1().ReplicaSets(namespace).Get(context.TODO(), replicaset.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		resourceData, err := json.Marshal(replicasetData)
		if err != nil {
			c.logger.Errorf("Unable to get resource content of replicaset: %s", err)
			return err
		}
		c.logger.Debug(resourceData)

		err = c.backupSend(gvr, resourceData, replicaset.Name, backupClient)
		if err != nil {
			return err
		}

	}
	return nil
}

func (c *Controller) getStatefulsets(gvr GroupResouceVersion, namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

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

	for _, stateful := range statefulList.Items {
		statefulData, err := k8sClinet.AppsV1().StatefulSets(namespace).Get(context.TODO(), stateful.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		resourceData, err := json.Marshal(statefulData)
		if err != nil {
			c.logger.Errorf("Unable to get resource content of stateful: %s", err)
			return err
		}
		c.logger.Debug(resourceData)

		err = c.backupSend(gvr, resourceData, stateful.Name, backupClient)
		if err != nil {
			return err
		}

	}
	return nil
}
