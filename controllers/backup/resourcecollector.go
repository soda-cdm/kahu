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

	v1 "k8s.io/api/core/v1"
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

	allServicesList = utils.FindMatchedStrings(utils.Service, allServicesList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, service := range allServices.Items {
		if utils.Contains(allServicesList, service.Name) {
			serviceData, err := c.GetService(namespace, service.Name)
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

func (c *controller) GetService(namespace, name string) (*v1.Service, error) {

	k8sClient, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return nil, err
	}

	service, err := k8sClient.CoreV1().Services(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return service, err
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

	configAllLits = utils.FindMatchedStrings(utils.Configmap, configAllLits, backup.Spec.IncludedResources,
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

	allSecretsList = utils.FindMatchedStrings(utils.Secret, allSecretsList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, secret := range secretList.Items {
		if utils.Contains(allSecretsList, secret.Name) {
			secretData, err := c.GetSecret(namespace, secret.Name)
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

func (c *controller) GetSecret(namespace, name string) (*v1.Secret, error) {

	k8sClient, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return nil, err
	}

	secret, err := k8sClient.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return secret, err
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

	allendpointList = utils.FindMatchedStrings(utils.Endpoint, allendpointList, backup.Spec.IncludedResources,
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

func (c *controller) GetReplicaSetAndBackup(name, namespace string,
	backupClient metaservice.MetaService_BackupClient) error {
	k8sClient, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}

	replicaset, err := k8sClient.AppsV1().ReplicaSets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	err = c.backupSend(replicaset, replicaset.Name, backupClient)
	if err != nil {
		return err
	}
	return nil

}

func (c *controller) replicaSetBackup(namespace string,
	backup *PrepareBackup, backupClient metaservice.MetaService_BackupClient) error {

	c.logger.Infoln("starting collecting replicaset")
	k8sClient, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()

	rList, err := k8sClient.AppsV1().ReplicaSets(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}
	var replicasetAllList []string
	for _, replicaset := range rList.Items {
		replicasetAllList = append(replicasetAllList, replicaset.Name)
	}

	replicasetAllList = utils.FindMatchedStrings(utils.Replicaset, replicasetAllList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, replicaset := range rList.Items {
		if utils.Contains(replicasetAllList, replicaset.Name) {
			// backup the replicaset yaml
			err = c.GetReplicaSetAndBackup(replicaset.Name, replicaset.Namespace, backupClient)
			if err != nil {
				return err
			}

			// backup the volumespec releted object like, configmaps, secret, pvc and sc
			err = c.GetVolumesSpec(replicaset.Spec.Template.Spec, replicaset.Namespace, backupClient)
			if err != nil {
				return err
			}

			// get service account relared objects
			err = c.GetServiceAccountSpec(replicaset.Spec.Template.Spec, replicaset.Namespace, backupClient)
			if err != nil {
				return err
			}

			// get services based on selectors
			var selectorList []map[string]string
			selectorList = append(selectorList, replicaset.Spec.Selector.MatchLabels)
			err = c.GetServiceForPod(replicaset.Namespace, selectorList, backupClient, k8sClient)
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

	allstatefulList = utils.FindMatchedStrings(utils.Statefulset, allstatefulList, backup.Spec.IncludedResources,
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

func (c *controller) GetVolumesSpec(podspec v1.PodSpec, namespace string,
	backupClient metaservice.MetaService_BackupClient) error {

	for _, v := range podspec.Volumes {

		// collect config maps used in deployment
		if v.ConfigMap != nil {
			configMap, err := c.GetConfigMap(namespace, v.ConfigMap.Name)
			if err != nil {
				c.logger.Errorf("unable to get configmap for name: %s", v.ConfigMap.Name)
				return err
			}

			err = c.backupSend(configMap, v.ConfigMap.Name, backupClient)
			if err != nil {
				c.logger.Errorf("unable to backup configmap for name: %s", v.ConfigMap.Name)
				return err
			}
		}

		// collect pvc used in deployment
		if v.PersistentVolumeClaim != nil {
			pvc, err := c.GetPVC(namespace, v.PersistentVolumeClaim.ClaimName)
			if err != nil {
				c.logger.Errorf("unable to get pvc:%s, error:%s", v.PersistentVolumeClaim.ClaimName, err)
				return err
			}

			err = c.backupSend(pvc, v.PersistentVolumeClaim.ClaimName, backupClient)
			if err != nil {
				c.logger.Errorf("unable to backup pvc:%s, error:%s", v.PersistentVolumeClaim.ClaimName, err)
				return err
			}

			// now get the storageclass used in PVC
			var storageClassName string
			if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
				storageClassName = *pvc.Spec.StorageClassName
				sc, err := c.GetSC(storageClassName)
				if err != nil {
					c.logger.Errorf("unable to get storageclass:%s", storageClassName, err)
					return err
				}

				err = c.backupSend(sc, storageClassName, backupClient)
				if err != nil {
					c.logger.Errorf("unable to backup storageclass:%s, error:%s", storageClassName, err)
					return err
				}
			}
		}

		// collect secret used in deployment
		if v.Secret != nil {
			secret, err := c.GetSecret(namespace, v.Secret.SecretName)
			if err != nil {
				c.logger.Errorf("unable to get secret:%s, error:%s", v.Secret.SecretName, err)
				return err
			}
			err = c.backupSend(secret, secret.Name, backupClient)
			if err != nil {
				c.logger.Errorf("unable to backup secret: %s, error:%s", v.Secret.SecretName, err)
				return err
			}
		}
	}

	return nil
}

func (c *controller) GetServiceAccountSpec(podspec v1.PodSpec, namespace string,
	backupClient metaservice.MetaService_BackupClient) error {

	// now collect service account name
	saName := podspec.ServiceAccountName
	if saName != "" {
		sa, err := c.GetServiceAccount(namespace, saName)
		if err != nil {
			c.logger.Errorf("unable to get service account:%s, error:%s", saName, err)
			return err
		}
		return c.backupSend(sa, saName, backupClient)
	}

	return nil
}
