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

	"github.com/soda-cdm/kahu/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
)

func (ctrl *controller) getServices(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {
	ctrl.logger.Infoln("Starting collecting services")

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()
	allServices, err := ctrl.kubeClient.CoreV1().Services(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}

	var allServicesList []string
	for _, sc := range allServices.Items {
		allServicesList = append(allServicesList, sc.Name)
	}

	allServicesList = utils.FindMatchedStrings(utils.Service, allServicesList, backup.Spec.IncludeResources,
		backup.Spec.ExcludeResources)

	for _, service := range allServices.Items {
		if utils.Contains(allServicesList, service.Name) {
			err = ctrl.backupSend(&service, service.Name, backupClient)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func (ctrl *controller) getConfigMapS(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {
	ctrl.logger.Infoln("Starting collecting configmaps")

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()
	configList, err := ctrl.kubeClient.CoreV1().
		ConfigMaps(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}
	var configAllLits []string
	for _, deployment := range configList.Items {
		configAllLits = append(configAllLits, deployment.Name)
	}

	configAllLits = utils.FindMatchedStrings(utils.Configmap, configAllLits, backup.Spec.IncludeResources,
		backup.Spec.ExcludeResources)

	for _, item := range configList.Items {
		if utils.Contains(configAllLits, item.Name) {
			err = ctrl.backupSend(&item, item.Name, backupClient)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func (ctrl *controller) getSecrets(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {
	ctrl.logger.Infoln("Starting collecting secrets")

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()
	secretList, err := ctrl.kubeClient.CoreV1().
		Secrets(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}

	var allSecretsList []string
	for _, sc := range secretList.Items {
		allSecretsList = append(allSecretsList, sc.Name)
	}

	allSecretsList = utils.FindMatchedStrings(utils.Secret, allSecretsList, backup.Spec.IncludeResources,
		backup.Spec.ExcludeResources)

	for _, secret := range secretList.Items {
		if utils.Contains(allSecretsList, secret.Name) {
			err = ctrl.backupSend(&secret, secret.Name, backupClient)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func (ctrl *controller) GetSecret(namespace, name string) (*v1.Secret, error) {
	secret, err := ctrl.kubeClient.CoreV1().
		Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return secret, err
}

func (ctrl *controller) getEndpoints(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {
	ctrl.logger.Infoln("Starting collecting endpoints")

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()
	endpointList, err := ctrl.kubeClient.CoreV1().
		Endpoints(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}

	var allendpointList []string
	for _, sc := range endpointList.Items {
		allendpointList = append(allendpointList, sc.Name)
	}

	allendpointList = utils.FindMatchedStrings(utils.Endpoint, allendpointList, backup.Spec.IncludeResources,
		backup.Spec.ExcludeResources)

	for _, endpoint := range endpointList.Items {
		if utils.Contains(allendpointList, endpoint.Name) {
			endpointData, err := ctrl.kubeClient.CoreV1().
				Endpoints(namespace).Get(context.TODO(), endpoint.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			err = ctrl.backupSend(endpointData, endpointData.Name, backupClient)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (ctrl *controller) replicaSetBackup(namespace string,
	backup *PrepareBackup, backupClient metaservice.MetaService_BackupClient) error {
	ctrl.logger.Infoln("Starting collecting replicaset")

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()

	rList, err := ctrl.kubeClient.AppsV1().
		ReplicaSets(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}
	var replicasetAllList []string
	for _, replicaset := range rList.Items {
		replicasetAllList = append(replicasetAllList, replicaset.Name)
	}

	replicasetAllList = utils.FindMatchedStrings(utils.Replicaset, replicasetAllList, backup.Spec.IncludeResources,
		backup.Spec.ExcludeResources)

	for _, replicaset := range rList.Items {
		if utils.Contains(replicasetAllList, replicaset.Name) {

			// skiping the repicaset backup because it is not deployed independently
			if replicaset.GetOwnerReferences() != nil {
				ctrl.logger.Infof("skipping the backup for replicaset:%s, because it has owenreference", replicaset.Name)
				continue
			}

			// backup the replicaset yaml
			err = ctrl.backupSend(&replicaset, replicaset.Name, backupClient)
			if err != nil {
				return err
			}

			// backup the volumespec releted object like, configmaps, secret, pvc and sc
			err = ctrl.GetVolumesSpec(replicaset.Spec.Template.Spec, replicaset.Namespace, backupClient)
			if err != nil {
				return err
			}

			// get service account relared objects
			err = ctrl.GetServiceAccountSpec(replicaset.Spec.Template.Spec, replicaset.Namespace, backupClient)
			if err != nil {
				return err
			}

			// collect all the clusterrolebindings
			err = ctrl.getclusterRoleBindings(replicaset.Spec.Template.Spec.ServiceAccountName, backup, backupClient)
			if err != nil {
				return err
			}

			// collect all the rolebindings
			err = ctrl.getRoleBindings(replicaset.Spec.Template.Spec.ServiceAccountName, replicaset.Namespace, backup,
				backupClient)
			if err != nil {
				return err
			}

			// get services based on selectors
			var selectorList []map[string]string
			selectorList = append(selectorList, replicaset.Spec.Selector.MatchLabels)
			err = ctrl.GetServiceForPod(replicaset.Namespace, selectorList, backupClient)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (ctrl *controller) getStatefulsets(namespace string, backup *PrepareBackup,
	backupClient metaservice.MetaService_BackupClient) error {
	ctrl.logger.Infoln("starting collecting statefulsets")

	var labelSelectors map[string]string
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}

	selectors := labels.Set(labelSelectors).String()
	statefulList, err := ctrl.kubeClient.AppsV1().
		StatefulSets(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectors,
	})
	if err != nil {
		return err
	}
	var statefulsetAllList []string
	for _, statefulset := range statefulList.Items {
		statefulsetAllList = append(statefulsetAllList, statefulset.Name)
	}

	statefulsetAllList = utils.FindMatchedStrings(utils.StatefulSet, statefulsetAllList, backup.Spec.IncludeResources,
		backup.Spec.ExcludeResources)

	for _, statefulset := range statefulList.Items {
		if utils.Contains(statefulsetAllList, statefulset.Name) {
			// backup the statefulset yaml
			err = ctrl.backupSend(&statefulset, statefulset.Name, backupClient)
			if err != nil {
				return err
			}

			// backup the volumespec releted object like, configmaps, secret, pvc and sc
			err = ctrl.GetVolumesSpec(statefulset.Spec.Template.Spec, statefulset.Namespace, backupClient)
			if err != nil {
				return err
			}

			// get PVC for statefulset
			pvcList, err := ctrl.kubeClient.CoreV1().PersistentVolumeClaims(namespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: metav1.FormatLabelSelector(statefulset.Spec.Selector),
			})
			if err != nil {
				return err
			}
			for _, pvc := range pvcList.Items {
				pvcData, err := ctrl.GetPVC(&pvc)
				if err != nil {
					ctrl.logger.Errorf("unable to get pvc:%s", pvc.Name, err)
					return err
				}
				if pvcData == nil {
					continue
				}

				err = ctrl.backupSend(pvcData, pvc.Name, backupClient)
				if err != nil {
					ctrl.logger.Errorf("unable to backup pvc:%s, error:%s", pvc.Name, err)
					return err
				}

				// now get the storageclass used in PVC
				var storageClassName string
				if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
					storageClassName = *pvc.Spec.StorageClassName
					sc, err := ctrl.GetSC(storageClassName)
					if err != nil {
						ctrl.logger.Errorf("unable to get storageclass:%s", storageClassName, err)
						return err
					}

					err = ctrl.backupSend(sc, storageClassName, backupClient)
					if err != nil {
						ctrl.logger.Errorf("unable to backup storageclass:%s, error:%s", storageClassName, err)
						return err
					}
				}

			}

			// get service account related objects
			err = ctrl.GetServiceAccountSpec(statefulset.Spec.Template.Spec, statefulset.Namespace, backupClient)
			if err != nil {
				return err
			}

			// collect all the clusterrolebindings
			err = ctrl.getclusterRoleBindings(statefulset.Spec.Template.Spec.ServiceAccountName, backup, backupClient)
			if err != nil {
				return err
			}

			// collect all the rolebindings
			err = ctrl.getRoleBindings(statefulset.Spec.Template.Spec.ServiceAccountName, statefulset.Namespace,
				backup, backupClient)
			if err != nil {
				return err
			}

			// get services based on selectors
			var selectorList []map[string]string
			selectorList = append(selectorList, statefulset.Spec.Selector.MatchLabels)
			err = ctrl.GetServiceForPod(statefulset.Namespace, selectorList, backupClient)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (ctrl *controller) GetVolumesSpec(podspec v1.PodSpec, namespace string,
	backupClient metaservice.MetaService_BackupClient) error {

	for _, v := range podspec.Volumes {

		// collect config maps used in deployment
		if v.ConfigMap != nil {
			configMap, err := ctrl.GetConfigMap(namespace, v.ConfigMap.Name)
			if err != nil {
				ctrl.logger.Errorf("unable to get configmap for name: %s", v.ConfigMap.Name)
				return err
			}

			err = ctrl.backupSend(configMap, v.ConfigMap.Name, backupClient)
			if err != nil {
				ctrl.logger.Errorf("unable to backup configmap for name: %s", v.ConfigMap.Name)
				return err
			}
		}

		// collect pvc used in deployment
		if v.PersistentVolumeClaim != nil {
			pvc, err := ctrl.GetPVCByName(namespace, v.PersistentVolumeClaim.ClaimName)
			if err != nil {
				ctrl.logger.Errorf("unable to get pvc:%s, error:%s", v.PersistentVolumeClaim.ClaimName, err)
				return err
			}
			if pvc == nil {
				continue
			}

			err = ctrl.backupSend(pvc, v.PersistentVolumeClaim.ClaimName, backupClient)
			if err != nil {
				ctrl.logger.Errorf("unable to backup pvc:%s, error:%s", v.PersistentVolumeClaim.ClaimName, err)
				return err
			}

			// now get the storageclass used in PVC
			var storageClassName string
			if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
				storageClassName = *pvc.Spec.StorageClassName
				sc, err := ctrl.GetSC(storageClassName)
				if err != nil {
					ctrl.logger.Errorf("unable to get storageclass:%s", storageClassName, err)
					return err
				}

				err = ctrl.backupSend(sc, storageClassName, backupClient)
				if err != nil {
					ctrl.logger.Errorf("unable to backup storageclass:%s, error:%s", storageClassName, err)
					return err
				}
			}
		}

		// collect secret used in deployment
		if v.Secret != nil {
			secret, err := ctrl.GetSecret(namespace, v.Secret.SecretName)
			if err != nil {
				ctrl.logger.Errorf("unable to get secret:%s, error:%s", v.Secret.SecretName, err)
				return err
			}
			err = ctrl.backupSend(secret, secret.Name, backupClient)
			if err != nil {
				ctrl.logger.Errorf("unable to backup secret: %s, error:%s", v.Secret.SecretName, err)
				return err
			}
		}
	}

	return nil
}

func (ctrl *controller) GetServiceAccountSpec(podspec v1.PodSpec, namespace string,
	backupClient metaservice.MetaService_BackupClient) error {

	// now collect service account name
	saName := podspec.ServiceAccountName
	if saName != "" {
		sa, err := ctrl.GetServiceAccount(namespace, saName)
		if err != nil {
			ctrl.logger.Errorf("unable to get service account:%s, error:%s", saName, err)
			return err
		}
		return ctrl.backupSend(sa, saName, backupClient)
	}

	return nil
}
