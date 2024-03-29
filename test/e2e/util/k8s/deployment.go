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

	"github.com/soda-cdm/kahu/test/e2e/util/kahu"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

// newDeployment returns a RollingUpdate Deployment with a fake container image
func NewDeployment(name, ns string, replicas int32, labels map[string]string, image string) *apps.Deployment {
	return &apps.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
		Spec: apps.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Strategy: apps.DeploymentStrategy{
				Type:          apps.RollingUpdateDeploymentStrategyType,
				RollingUpdate: new(apps.RollingUpdateDeployment),
			},
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
	}
}

func CreateDeployment(c clientset.Interface, ns string, deployment *apps.Deployment) (*apps.Deployment, error) {
	return c.AppsV1().Deployments(ns).Create(context.TODO(), deployment, metav1.CreateOptions{})
}

func DeleteDeployment(c clientset.Interface, name, ns string) error {
	return c.AppsV1().Deployments(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func GetDeployment(c clientset.Interface, ns, name string) (*apps.Deployment, error) {
	return c.AppsV1().Deployments(ns).Get(context.TODO(), name, metav1.GetOptions{})
}

// WaitForReadyDeployment waits for number of ready replicas to equal number of replicas.
func WaitForReadyDeployment(c clientset.Interface, ns, name string) error {
	if err := wait.PollImmediate(kahu.PollInterval, kahu.PollTimeout, func() (bool, error) {
		deployment, err := c.AppsV1().Deployments(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to get deployment %q: %v", name, err)
		}
		return deployment.Status.ReadyReplicas == *deployment.Spec.Replicas, nil
	}); err != nil {
		return fmt.Errorf("failed to wait for .readyReplicas to equal .replicas: %v", err)
	}
	return nil
}

//deployment with configmap
func NewDeploymentWithEnvFromConfigmap(name, ns string, replicas int32, labels map[string]string, image string, configName string) *apps.Deployment {
	return &apps.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
		Spec: apps.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Strategy: apps.DeploymentStrategy{
				Type:          apps.RollingUpdateDeploymentStrategyType,
				RollingUpdate: new(apps.RollingUpdateDeployment),
			},
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
	}
}

//deployment with secret
func NewDeploymentWithEnvFromsecret(name, ns string, replicas int32, labels map[string]string, image string, secretName string) *apps.Deployment {
	return &apps.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
		Spec: apps.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Strategy: apps.DeploymentStrategy{
				Type:          apps.RollingUpdateDeploymentStrategyType,
				RollingUpdate: new(apps.RollingUpdateDeployment),
			},
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
	}
}

//NewDeploymentWithEnvValConfigmap
func NewDeploymentWithEnvValConfigmap(name, ns string, replicas int32, labels map[string]string, image string, configName string) *apps.Deployment {
	return &apps.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
		Spec: apps.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Strategy: apps.DeploymentStrategy{
				Type:          apps.RollingUpdateDeploymentStrategyType,
				RollingUpdate: new(apps.RollingUpdateDeployment),
			},
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
	}
}

func NewDeploymentWithEnvValsecret(name, ns string, replicas int32, labels map[string]string, image string, secretName string) *apps.Deployment {
	return &apps.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
		Spec: apps.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Strategy: apps.DeploymentStrategy{
				Type:          apps.RollingUpdateDeploymentStrategyType,
				RollingUpdate: new(apps.RollingUpdateDeployment),
			},
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
	}
}
