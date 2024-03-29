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
	"github.com/soda-cdm/kahu/test/e2e/util/kahu"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	waitutil "k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

func NewStatefulset(name, ns string, replicas int32, labels map[string]string, image string) (*apps.StatefulSet, error) {
	return &apps.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
		Spec: apps.StatefulSetSpec{
			Replicas: &replicas,
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

//NewStatefulsetWithConfigmap
func NewStatefulsetWithEnvFromConfigmap(name, ns string, replicas int32, labels map[string]string, image string, configName string) (*apps.StatefulSet, error) {
	return &apps.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
		Spec: apps.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:    name,
							Image:   image,
							Command: []string{"sleep", "1000"},
							EnvFrom: []corev1.EnvFromSource{{
								ConfigMapRef: &corev1.ConfigMapEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configName,
									},
								},
							}},
						},
					},
				},
			},
		},
	}, nil
}

func NewStatefulsetWithEnvFromSecret(name, ns string, replicas int32, labels map[string]string, image string, secretName string) (*apps.StatefulSet, error) {
	return &apps.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
		Spec: apps.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:    name,
							Image:   image,
							Command: []string{"sleep", "1000"},
							EnvFrom: []corev1.EnvFromSource{{
								SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: secretName,
									},
								},
							}},
						},
					},
				},
			},
		},
	}, nil
}

func NewStatefulsetWithEnvValConfigmap(name, ns string, replicas int32, labels map[string]string, image string, configName string) (*apps.StatefulSet, error) {
	return &apps.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
		Spec: apps.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:    name,
							Image:   image,
							Command: []string{"sleep", "1000"},
							Env: []corev1.EnvVar{{
								Name: "SPECIAL_LEVEL_KEY",
								ValueFrom: &corev1.EnvVarSource{
									ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: configName,
										},
										Key: "SPECIAL_LEVEL",
									},
								},
							},
								{
									Name: "SPECIAL_TYPE_KEY",
									ValueFrom: &corev1.EnvVarSource{
										ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: configName,
											},
											Key: "SPECIAL_TYPE",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func NewStatefulsetWithEnvValSecret(name, ns string, replicas int32, labels map[string]string, image string, secretName string) (*apps.StatefulSet, error) {
	return &apps.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
		Spec: apps.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:    name,
							Image:   image,
							Command: []string{"sleep", "1000"},
							Env: []corev1.EnvVar{{
								Name: "SPECIAL_LEVEL_KEY",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: secretName,
										},
										Key: "SPECIAL_LEVEL",
									},
								},
							},
								{
									Name: "SPECIAL_TYPE_KEY",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: secretName,
											},
											Key: "SPECIAL_TYPE",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func CreateStatefulset(c clientset.Interface, ns string, statefulSet *apps.StatefulSet) (*apps.StatefulSet, error) {
	return c.AppsV1().StatefulSets(ns).Create(context.TODO(), statefulSet, metav1.CreateOptions{})
}
func WaitForStatefulsetDelete(c clientset.Interface, ns, name string) error {
	if err := c.AppsV1().StatefulSets(ns).Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to delete  secret in namespace %q", ns))
	}
	return waitutil.PollImmediateInfinite(5*time.Second,
		func() (bool, error) {
			if _, err := c.AppsV1().StatefulSets(ns).Get(context.TODO(), ns, metav1.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			}
			logrus.Debugf("statefulset %q in namespace %q is still being deleted...", name, ns)
			return false, nil
		})
}

// WaitForSecretsComplete uses c to wait for completions to complete for the Job jobName in namespace ns.
func WaitForStatefulsetComplete(c clientset.Interface, ns, name string) error {
	return wait.Poll(kahu.PollInterval, kahu.PollTimeout, func() (bool, error) {
		_, err := c.AppsV1().StatefulSets(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})
}

func GetStatefulset(c clientset.Interface, ns, name string) (*apps.StatefulSet, error) {
	return c.AppsV1().StatefulSets(ns).Get(context.TODO(), name, metav1.GetOptions{})
}

func DeleteStatefulSet(c clientset.Interface, name, ns string) error {
	return c.AppsV1().StatefulSets(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
}
