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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	waitutil "k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

func CreatePod(c clientset.Interface, ns, name string) (*corev1.Pod, error) {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    name,
					Image:   "nginx",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}
	return c.CoreV1().Pods(ns).Create(context.TODO(), p, metav1.CreateOptions{})
}

func CreatePodWithServiceAccount(c clientset.Interface, ns, name, serviceAccountName string) (*corev1.Pod, error) {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    name,
					Image:   "nginx",
					Command: []string{"sleep", "3600"},
				},
			},
			ServiceAccountName: serviceAccountName,
		},
	}
	return c.CoreV1().Pods(ns).Create(context.TODO(), p, metav1.CreateOptions{})
}

func GetPod(ctx context.Context, c clientset.Interface, namespace string, pod string) (*corev1.Pod, error) {
	return c.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
}

func AddAnnotationToPod(ctx context.Context, c clientset.Interface, namespace, podName string, ann map[string]string) (*corev1.Pod, error) {

	newPod, err := GetPod(ctx, c, namespace, podName)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("Fail to get pod %s in namespace %s", podName, namespace))
	}
	newAnn := newPod.ObjectMeta.Annotations
	if newAnn == nil {
		newAnn = make(map[string]string)
	}
	for k, v := range ann {
		fmt.Println(k, v)
		newAnn[k] = v
	}
	newPod.Annotations = newAnn
	fmt.Println(newPod.Annotations)

	return c.CoreV1().Pods(namespace).Update(ctx, newPod, metav1.UpdateOptions{})
}

func DeletePod(ctx context.Context, c clientset.Interface, namespace, podName string) error {
	return c.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
}

// WaitForReadyPod waits for number of ready replicas to equal number of replicas.
func WaitForPodComplete(c clientset.Interface, ns, name string) error {
	return wait.Poll(PollInterval, PollTimeout, func() (bool, error) {
		_, err := c.CoreV1().Pods(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		logrus.Infof("pod %q in namespace %q is still being created...", name, ns)
		return true, nil
	})
}

func WaitForPodDelete(c clientset.Interface, ns, name string) error {
	if err := c.CoreV1().ConfigMaps(ns).Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to delete  configmap in namespace %q", ns))
	}
	return waitutil.PollImmediateInfinite(5*time.Second,
		func() (bool, error) {
			if _, err := c.CoreV1().Pods(ns).Get(context.TODO(), ns, metav1.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			}
			logrus.Debugf("pod %q in namespace %q is still being deleted...", name, ns)
			return false, nil
		})
}
