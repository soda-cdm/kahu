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

package util

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	v1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func CreateRBACWithBindingSA(ctx context.Context, c clientset.Interface, namespace string, serviceaccount string, clusterrole string, clusterrolebinding string) error {
	role := &v1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterrole,
		},
	}

	_, err := c.RbacV1().ClusterRoles().Create(ctx, role, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	//creating role binding and binding it to the test service account
	rolebinding := &v1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterrolebinding,
		},
		Subjects: []v1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceaccount,
				Namespace: namespace,
			},
		},
		RoleRef: v1.RoleRef{
			Kind: "ClusterRole",
			Name: clusterrole,
		},
	}

	_, err = c.RbacV1().ClusterRoleBindings().Create(ctx, rolebinding, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func GetClusterRole(ctx context.Context, c clientset.Interface, role string) (*v1.ClusterRole, error) {
	return c.RbacV1().ClusterRoles().Get(ctx, role, metav1.GetOptions{})
}

func GetClusterRoleBinding(ctx context.Context, c clientset.Interface, rolebinding string) (*v1.ClusterRoleBinding, error) {
	return c.RbacV1().ClusterRoleBindings().Get(ctx, rolebinding, metav1.GetOptions{})
}

func CleanupClusterRole(ctx context.Context, c clientset.Interface, nsBaseName string) error {

	clusterroles, err := c.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "Could not retrieve clusterroles")
	}

	for _, checkClusterRole := range clusterroles.Items {
		if strings.HasPrefix(checkClusterRole.Name, "clusterrole-"+nsBaseName) {
			fmt.Printf("Cleaning up clusterrole %s\n", checkClusterRole.Name)
			err = c.RbacV1().ClusterRoles().Delete(ctx, checkClusterRole.Name, metav1.DeleteOptions{})
			if err != nil {
				return errors.Wrapf(err, "Could not delete clusterrole %s", checkClusterRole.Name)
			}
		}
	}
	return nil
}

func CleanupClusterRoleBinding(ctx context.Context, c clientset.Interface, nsBaseName string) error {

	clusterrolebindings, err := c.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "Could not retrieve clusterrolebindings")
	}

	for _, checkClusterRoleBinding := range clusterrolebindings.Items {
		if strings.HasPrefix(checkClusterRoleBinding.Name, "clusterrolebinding-"+nsBaseName) {
			fmt.Printf("Cleaning up clusterrolebinding %s\n", checkClusterRoleBinding.Name)
			err = c.RbacV1().ClusterRoleBindings().Delete(ctx, checkClusterRoleBinding.Name, metav1.DeleteOptions{})
			if err != nil {
				return errors.Wrapf(err, "Could not delete clusterrolebinding %s", checkClusterRoleBinding.Name)
			}
		}
	}
	return nil
}
