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
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	waitutil "k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

func NewDaemonset(name, ns string, replicas int32, labels map[string]string, image string) (*apps.DaemonSet, error) {
	return &apps.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
		Spec: apps.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  name,
							Image: image,
							//Command: []string{"sleep", "1000000"},
						},
					},
				},
			},
		},
	}, nil
}

func CreateDaemonset(c clientset.Interface, ns string, DaemonSet *apps.DaemonSet) (*apps.DaemonSet, error) {
	return c.AppsV1().DaemonSets(ns).Create(context.TODO(), DaemonSet, metav1.CreateOptions{})
}
func WaitForDaemonsetDelete(c clientset.Interface, ns, name string) error {
	if err := c.AppsV1().DaemonSets(ns).Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to delete  secret in namespace %q", ns))
	}
	return waitutil.PollImmediateInfinite(5*time.Second,
		func() (bool, error) {
			if _, err := c.AppsV1().DaemonSets(ns).Get(context.TODO(), ns, metav1.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			}
			logrus.Debugf("secret %q in namespace %q is still being deleted...", name, ns)
			return false, nil
		})
}

// WaitForSecretsComplete uses c to wait for completions to complete for the Job jobName in namespace ns.
func WaitForDaemonsetComplete(c clientset.Interface, ns, name string) error {
	return wait.Poll(PollInterval, PollTimeout, func() (bool, error) {
		_, err := c.AppsV1().DaemonSets(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})
}

func GetDaemonset(c clientset.Interface, ns, name string) (*apps.DaemonSet, error) {
	return c.AppsV1().DaemonSets(ns).Get(context.TODO(), name, metav1.GetOptions{})
}

func DeleteDaemonSet(c clientset.Interface, name, ns string) error {
	return c.AppsV1().DaemonSets(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
}
