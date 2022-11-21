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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

func WaitUntilServiceAccountCreated(ctx context.Context, c clientset.Interface, namespace, serviceAccount string, timeout time.Duration) error {
	return wait.PollImmediate(5*time.Second, timeout,
		func() (bool, error) {
			if _, err := c.CoreV1().ServiceAccounts(namespace).Get(ctx, serviceAccount, metav1.GetOptions{}); err != nil {
				if !apierrors.IsNotFound(err) {
					return false, err
				}
				return false, nil
			}
			return true, nil
		})
}

func CreateServiceAccount(ctx context.Context, c clientset.Interface, namespace string, serviceaccount string) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceaccount,
		},
		AutomountServiceAccountToken: nil,
	}
	_, err := c.CoreV1().ServiceAccounts(namespace).Create(ctx, sa, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func GetServiceAccount(ctx context.Context, c clientset.Interface, namespace string, serviceAccount string) (*corev1.ServiceAccount, error) {
	return c.CoreV1().ServiceAccounts(namespace).Get(ctx, serviceAccount, metav1.GetOptions{})
}

func DeleteServiceAccount(ctx context.Context, c clientset.Interface, name, namespace string) error {
	return c.CoreV1().ServiceAccounts(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}
