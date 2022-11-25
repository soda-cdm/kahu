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
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1api "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	waitutil "k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

func NewNameSpace(c clientset.Interface, namespace string) *corev1api.Namespace {
	return &corev1api.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1api.SchemeGroupVersion.String(),
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
}

func CreateNamespace(ctx context.Context, c clientset.Interface, name string) error {
	ns := NewNameSpace(c, name)
	_, err := c.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func CreateNamespaceWithLabel(ctx context.Context, c clientset.Interface, namespace string, label map[string]string) error {
	ns := NewNameSpace(c, namespace)
	ns.Labels = label
	_, err := c.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func CreateNamespaceWithAnnotation(ctx context.Context, c clientset.Interface, namespace string, annotation map[string]string) error {
	ns := NewNameSpace(c, namespace)
	ns.ObjectMeta.Annotations = annotation
	_, err := c.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func GetNamespace(ctx context.Context, c clientset.Interface, namespace string) (*corev1api.Namespace, error) {
	return c.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
}

func DeleteNamespace(ctx context.Context, c clientset.Interface, namespace string, wait bool) error {
	tenMinuteTimeout, _ := context.WithTimeout(context.Background(), time.Minute*10)
	if err := c.CoreV1().Namespaces().Delete(context.TODO(), namespace, metav1.DeleteOptions{}); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to delete the namespace %q", namespace))
	}
	if !wait {
		return nil
	}
	return waitutil.PollImmediateInfinite(5*time.Second,
		func() (bool, error) {
			if _, err := c.CoreV1().Namespaces().Get(tenMinuteTimeout, namespace, metav1.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			}
			fmt.Printf("namespace %q is still being deleted...\n", namespace)
			logrus.Debugf("namespace %q is still being deleted...", namespace)
			return false, nil
		})
}
