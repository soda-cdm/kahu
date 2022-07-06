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

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/utils"

	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
)

func (c *controller) GetDeploymentAndBackup(name, namespace string,
	backupClient metaservice.MetaService_BackupClient) error {
	k8sClient, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}

	deployment, err := k8sClient.AppsV1().Deployments(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	err = c.backupSend(deployment, deployment.Name, backupClient)
	if err != nil {
		return err
	}
	return nil

}

func (c *controller) deploymentBackup(namespace string,
	backup *PrepareBackup, backupClient metaservice.MetaService_BackupClient) error {

	c.logger.Infoln("starting collecting deployments")
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
	dList, err := k8sClient.AppsV1().Deployments(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}
	var deploymentAllList []string
	for _, deployment := range dList.Items {
		deploymentAllList = append(deploymentAllList, deployment.Name)
	}

	deploymentAllList = utils.FindMatchedStrings(utils.Deployment, deploymentAllList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, deployment := range dList.Items {
		if utils.Contains(deploymentAllList, deployment.Name) {
			// backup the deployment yaml
			err = c.GetDeploymentAndBackup(deployment.Name, deployment.Namespace, backupClient)
			if err != nil {
				return err
			}

			// backup the volumespec releted object like, configmaps, secret, pvc and sc
			err = c.GetVolumesSpec(deployment.Spec.Template.Spec, deployment.Namespace, backupClient)
			if err != nil {
				return err
			}

			// get service account relared objects
			err = c.GetServiceAccountSpec(deployment.Spec.Template.Spec, deployment.Namespace, backupClient)
			if err != nil {
				return err
			}

		}
	}
	return nil
}

func (c *controller) GetServiceAccount(namespace, name string) (*corev1.ServiceAccount, error) {

	k8sClient, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return nil, err
	}

	sa, err := k8sClient.CoreV1().ServiceAccounts(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return sa, err
}

func (c *controller) GetConfigMap(namespace, name string) (*corev1.ConfigMap, error) {

	k8sClient, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return nil, err
	}

	configmap, err := k8sClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return configmap, err
}

func (c *controller) GetPVC(namespace, name string) (*corev1.PersistentVolumeClaim, error) {

	k8sClient, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return nil, err
	}

	pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return pvc, err
}

func (c *controller) ListNamespaces(backup *PrepareBackup) ([]string, error) {
	k8sClient, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return nil, err
	}

	var namespaceList []string
	namespaces, err := k8sClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return namespaceList, err
	}
	for _, ns := range namespaces.Items {
		namespaceList = append(namespaceList, ns.Name)

	}
	return namespaceList, nil
}

func (c *controller) getPersistentVolumeClaims(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {

	c.logger.Infoln("starting collecting persistentvolumeclaims")
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
	allPVC, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}

	var allPVCList []string
	for _, pvc := range allPVC.Items {
		allPVCList = append(allPVCList, pvc.Name)
	}

	allPVCList = utils.FindMatchedStrings(utils.Pvc, allPVCList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, item := range allPVC.Items {
		if utils.Contains(allPVCList, item.Name) {
			pvcData, err := c.GetPVC(namespace, item.Name)
			if err != nil {
				return err
			}
			err = c.backupSend(pvcData, pvcData.Name, backupClient)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func (c *controller) getStorageClass(backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {
	c.logger.Infoln("starting collecting storageclass")
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
	allSC, err := k8sClient.StorageV1().StorageClasses().List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}

	var allSCList []string
	for _, sc := range allSC.Items {
		allSCList = append(allSCList, sc.Name)
	}

	allSCList = utils.FindMatchedStrings(utils.Sc, allSCList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, item := range allSC.Items {
		if utils.Contains(allSCList, item.Name) {
			scData, err := c.GetSC(item.Name)
			err = c.backupSend(scData, scData.Name, backupClient)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *controller) GetSC(name string) (*storagev1.StorageClass, error) {

	k8sClient, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return nil, err
	}

	sc, err := k8sClient.StorageV1().StorageClasses().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return sc, err
}
