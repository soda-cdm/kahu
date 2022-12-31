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

	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (ctrl *controller) getclusterRoleBindings(serviceaccountName string,
	backup *PrepareBackup, backupClient metaservice.MetaService_BackupClient) error {

	ctrl.logger.Infoln("starting collecting clusterrolebindings")
	ctrl.logger.Infof("starting collecting clusterrolebindings for sa:%s", serviceaccountName)

	clusterrolebindingsList, err := ctrl.kubeClient.RbacV1().ClusterRoleBindings().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, clusterrolebinding := range clusterrolebindingsList.Items {
		// skip collecting the kubernetes clusterrolebindings
		if val, ok := clusterrolebinding.GetLabels()["kubernetes.io/bootstrapping"]; ok && val == "rbac-defaults" {
			continue
		}

		// skip collecting the clusterolebinding whihc serviceaccountName is not in it's subjects list
		for _, subject := range clusterrolebinding.Subjects {
			if subject.Kind == "ServiceAccount" && subject.Name == serviceaccountName {

				// if whole rcluster to be backuped
				ctrl.logger.Infof("collecting cbr, clusterole or roles.")

				err = ctrl.backupSend(&clusterrolebinding, clusterrolebinding.Name, backupClient)
				if err != nil {
					return err
				}

				// now collect the role or cluserrole from filter clusterbindings
				var kind, name string
				if clusterrolebinding.RoleRef.Size() != 0 {
					kind = clusterrolebinding.RoleRef.Kind
					name = clusterrolebinding.RoleRef.Name
				}
				ctrl.logger.Infof("started collecting role or clustrerole for name:%s, kind:%s, namespace:%s",
					name, kind, subject.Namespace)

				// collect the role or cluster rol
				err := ctrl.GetRoleOrClusterRole(name, kind, subject.Namespace, backupClient)
				if err != nil {
					return err
				}

			}
		}
	}
	return nil
}

func (ctrl *controller) getRoleBindings(serviceaccountName string, namespace string,
	backup *PrepareBackup, backupClient metaservice.MetaService_BackupClient) error {

	ctrl.logger.Infoln("starting collecting rolebindings")
	rolebindingsList, err := ctrl.kubeClient.RbacV1().RoleBindings(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, rolebinding := range rolebindingsList.Items {
		// skip collecting the kubernetes rolebindings
		if val, ok := rolebinding.GetLabels()["kubernetes.io/bootstrapping"]; ok && val == "rbac-defaults" {
			continue
		}

		// skip collecting the rolebinding whihc serviceaccountName is not in it's subjects list
		for _, subject := range rolebinding.Subjects {
			if subject.Kind == "ServiceAccount" && subject.Name == serviceaccountName {

				// if whole rcluster to be backuped
				ctrl.logger.Infof("collecting rolebindings, clusterole or roles.")

				err = ctrl.backupSend(&rolebinding, rolebinding.Name, backupClient)
				if err != nil {
					return err
				}

				// now collect the role or cluserrole from filter of rolebindings
				var kind, name string
				if rolebinding.RoleRef.Size() != 0 {
					kind = rolebinding.RoleRef.Kind
					name = rolebinding.RoleRef.Name
				}
				ctrl.logger.Infof("started collecting role or clustrerole for name:%s, kind:%s, namespace:%s",
					name, kind, subject.Namespace)

				// collect the role or cluster role
				err := ctrl.GetRoleOrClusterRole(name, kind, subject.Namespace, backupClient)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (ctrl *controller) GetRoleOrClusterRole(name, kind, namespace string,
	backupClient metaservice.MetaService_BackupClient) error {

	if kind == "Role" {
		role, err := ctrl.kubeClient.RbacV1().Roles(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		err = ctrl.backupSend(role, role.Name, backupClient)
		if err != nil {
			return err
		}
	}
	if kind == "ClusterRole" {
		clusterrole, err := ctrl.kubeClient.RbacV1().ClusterRoles().Get(context.TODO(), name, metav1.GetOptions{})
		err = ctrl.backupSend(clusterrole, clusterrole.Name, backupClient)
		if err != nil {
			return err
		}
	}
	return nil
}
