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

func CreateRBACWithBindingSAWithRole(ctx context.Context, c clientset.Interface, saNamespace string, ns, serviceaccount string, role string, rolebinding string) error {
	role1 := &v1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      role,
			Namespace: ns,
		},
	}

	_, err := c.RbacV1().Roles(ns).Create(ctx, role1, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	//creating role binding and binding it to the test service account
	rolebinding1 := &v1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: rolebinding,
		},
		Subjects: []v1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceaccount,
				Namespace: saNamespace,
			},
		},
		RoleRef: v1.RoleRef{
			Kind: "Role",
			Name: role,
		},
	}

	_, err = c.RbacV1().RoleBindings(ns).Create(ctx, rolebinding1, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func GetRole(ctx context.Context, c clientset.Interface, role, ns string) (*v1.Role, error) {
	return c.RbacV1().Roles(ns).Get(ctx, role, metav1.GetOptions{})
}

func GetRoleBinding(ctx context.Context, c clientset.Interface, rolebinding, ns string) (*v1.RoleBinding, error) {
	return c.RbacV1().RoleBindings(ns).Get(ctx, rolebinding, metav1.GetOptions{})
}

func CleanupRole(ctx context.Context, c clientset.Interface, nsBaseName, ns string) error {

	roles, err := c.RbacV1().Roles(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "Could not retrieve clusterroles")
	}

	for _, checkRole := range roles.Items {
		if strings.HasPrefix(checkRole.Name, "role-"+nsBaseName) {
			fmt.Printf("Cleaning up role %s\n", checkRole.Name)
			err = c.RbacV1().Roles(ns).Delete(ctx, checkRole.Name, metav1.DeleteOptions{})
			if err != nil {
				return errors.Wrapf(err, "Could not delete role %s", checkRole.Name)
			}
		}
	}
	return nil
}

func CleanupRoleBinding(ctx context.Context, c clientset.Interface, nsBaseName, ns string) error {

	rolebindings, err := c.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "Could not retrieve rolebindings")
	}

	for _, checkRoleBinding := range rolebindings.Items {
		if strings.HasPrefix(checkRoleBinding.Name, "rolebinding-"+nsBaseName) {
			fmt.Printf("Cleaning up rolebinding %s\n", checkRoleBinding.Name)
			err = c.RbacV1().RoleBindings(ns).Delete(ctx, checkRoleBinding.Name, metav1.DeleteOptions{})
			if err != nil {
				return errors.Wrapf(err, "Could not delete rolebinding %s", checkRoleBinding.Name)
			}
		}
	}
	return nil
}
