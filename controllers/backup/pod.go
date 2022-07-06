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

func (c *controller) GetPodAndBackup(name, namespace string,
	backupClient metaservice.MetaService_BackupClient) error {
	k8sClient, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}

	pod, err := k8sClient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return c.backupSend(pod, pod.Name, backupClient)

}

func (c *controller) podBackup(namespace string,
	backup *PrepareBackup, backupClient metaservice.MetaService_BackupClient) error {

	c.logger.Infoln("starting collecting pods")
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
	podList, err := k8sClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}
	var podAllList []string
	for _, pod := range podList.Items {
		podAllList = append(podAllList, pod.Name)
	}

	podAllList = utils.FindMatchedStrings(utils.Deployment, podAllList, backup.Spec.IncludedResources,
		backup.Spec.ExcludedResources)

	for _, pod := range podList.Items {
		if utils.Contains(podAllList, pod.Name) {
			// backup the deployment yaml
			err = c.GetPodAndBackup(pod.Name, pod.Namespace, backupClient)
			if err != nil {
				return err
			}

			// backup the volumespec releted object like, configmaps, secret, pvc and sc
			err = c.GetVolumesSpec(pod.Spec, pod.Namespace, backupClient)
			if err != nil {
				return err
			}

			// get service account relared objects
			err = c.GetServiceAccountSpec(pod.Spec, pod.Namespace, backupClient)
			if err != nil {
				return err
			}

		}
	}
	return nil
}
