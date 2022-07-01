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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

var podIDList = make([]types.UID, 0)
var configMapList = make(map[string]string)

// GetPods returns pods for the given namespace
func (c *Controller) podsBackup(gvr GroupResouceVersion, namespace string,
	backup *PrepareBackup, backupClient metaservice.MetaService_BackupClient) error {

	resourceObjectList, err := c.getResourceObjectsWIthOptions(gvr, namespace, backup)
	if err != nil {
		return err
	}

	return c.unstructuredMarshalSend(gvr, namespace, backupClient, resourceObjectList)
}

func (c *Controller) getPodsByOwner(replicasetList []types.UID, namespace string,
	backupClient metaservice.MetaService_BackupClient) error {
	k8sClinet, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}
	podList, err := k8sClinet.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	gvr := GroupResouceVersion{
		group:        podList.GroupVersionKind().Group,
		version:      podList.APIVersion,
		resourceName: podList.Kind,
	}

	for _, replicasetID := range replicasetList {
		for _, pod := range podList.Items {
			for _, owner := range pod.OwnerReferences {
				if owner.UID == replicasetID {
					// backup the pod content which is part of deployment
					pod_data, err := k8sClinet.CoreV1().Pods(namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
					if err != nil {
						c.logger.Errorf("Unable to get resource content: %s", err)
					}

					podIDList = append(podIDList, pod_data.GetUID())

					for _, v1 := range pod.Spec.Volumes {
						if v1.VolumeSource.ConfigMap != nil {
							configMapList[v1.VolumeSource.ConfigMap.Name] = pod.Name
						}
					}

					resourceData, err := json.Marshal(pod_data)
					if err != nil {
						c.logger.Errorf("Unable to get resource content of pod: %s", err)
						return err
					}

					c.backupSend(gvr, resourceData, namespace, backupClient)
				}
			}
		}
	}

	// now get configs which is used by pods
	err = c.getConfigsByOwner(configMapList, namespace, backupClient)
	return nil
}

func (c *Controller) getConfigsByOwner(configMapList map[string]string, namespace string,
	backupClient metaservice.MetaService_BackupClient) error {
	k8sClinet, err := kubernetes.NewForConfig(c.restClientconfig)
	if err != nil {
		c.logger.Errorf("Unable to get k8sclient %s", err)
		return err
	}
	for configMapName := range configMapList {
		config, err := k8sClinet.CoreV1().ConfigMaps(namespace).Get(context.TODO(), configMapName, metav1.GetOptions{})
		if err != nil {
			c.logger.Errorf("Unable to get resource content of configMap: %s", err)
		}
		resourceData, err := json.Marshal(config)
		if err != nil {
			c.logger.Errorf("Unable to get resource content of config: %s", err)
			return err
		}

		gvr := GroupResouceVersion{
			group:        config.GroupVersionKind().Group,
			version:      config.APIVersion,
			resourceName: config.Kind,
		}
		c.backupSend(gvr, resourceData, namespace, backupClient)
	}

	return nil
}
