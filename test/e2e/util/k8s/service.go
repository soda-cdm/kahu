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

	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
)

func CreateService(c clientset.Interface, ns, name string) (*corev1.Service, error) {
	s := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.IntOrString{IntVal: int32(9376)},
				},
			},
		},
	}
	return c.CoreV1().Services(ns).Create(context.TODO(), s, metav1.CreateOptions{})
}

func GetService(c clientset.Interface, name, namespace string) (*corev1.Service, error) {
	return c.CoreV1().Services(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}

func DeleteService(c clientset.Interface, namespace, name string) error {
	return c.CoreV1().Services(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func WaitUntilServiceCreated(c clientset.Interface, ns, name string) error {
	return wait.Poll(PollInterval, PollTimeout, func() (bool, error) {
		_, err := c.CoreV1().Services(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})
}
