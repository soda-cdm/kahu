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

package provisioner

import (
	"context"
	"encoding/json"
	"sync"

	log "github.com/sirupsen/logrus"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/utils/k8sresource"
)

const (
	moduleName             = "deployment-executor"
	labelDeployExecutor    = "deployment.executor.kahu.io"
	labelDeployExecutorKey = "key.deployment.executor.kahu.io"
	defaultReplicaCount    = 1
)

type deploymentProvider struct {
	sync.Mutex
	logger       *log.Entry
	kubeClient   kubernetes.Interface
	namespace    string
	replicaCount int32
}

func NewDeploymentProvider(_ context.Context, namespace string, kubeClient kubernetes.Interface) Interface {
	provider := &deploymentProvider{
		logger:       log.WithField("module", moduleName),
		kubeClient:   kubeClient,
		namespace:    namespace,
		replicaCount: defaultReplicaCount,
	}

	return provider
}

func (provider *deploymentProvider) Start(key string,
	templateFunc podTemplateFunc) error {
	provider.Lock()
	defer provider.Unlock()
	deployName := provider.deployName(key)
	provider.logger.Infof("Trying to start deployment workload[%s]", deployName)
	// check if deploy already exist
	workload, err := provider.kubeClient.AppsV1().
		Deployments(provider.namespace).
		Get(context.TODO(), deployName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// if not available, create one
	if apierrors.IsNotFound(err) {
		provider.logger.Infof("Creating deployment workload[%s]", deployName)
		podTemplate, err := templateFunc()
		if err != nil {
			return err
		}

		deployment, err := provider.deployment(key, podTemplate)
		if err != nil {
			return err
		}

		workload, err = provider.kubeClient.AppsV1().
			Deployments(provider.namespace).
			Create(context.TODO(), deployment, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		return nil
	}

	if *workload.Spec.Replicas == 0 {
		err = provider.scaleDeployment(key, provider.replicaCount)
		if err != nil {
			return err
		}
	}

	return nil
}

func (provider *deploymentProvider) Stop(key string) error {
	provider.Lock()
	defer provider.Unlock()
	provider.logger.Infof("Stopping deployment workload for %s", key)

	// scale down to zero
	return provider.scaleDeployment(key, 0)
}

func (provider *deploymentProvider) scaleDeployment(key string, replicas int32) error {
	deploymentName := provider.deployName(key)
	provider.logger.Infof("Scaling deployment[%s] to replica %d", deploymentName, replicas)
	deploy, err := provider.kubeClient.AppsV1().
		Deployments(provider.namespace).
		Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if *deploy.Spec.Replicas == replicas {
		return nil
	}

	scaleDeploy := deploy.DeepCopy()
	scaleDeploy.Spec.Replicas = &replicas

	_, err = provider.patchDeploymentObject(deploy, scaleDeploy)
	return err
}

func (provider *deploymentProvider) patchDeploymentObject(cur, mod *appv1.Deployment) (*appv1.Deployment, error) {
	curJson, err := json.Marshal(cur)
	if err != nil {
		return nil, err
	}

	modJson, err := json.Marshal(mod)
	if err != nil {
		return nil, err
	}

	patch, err := strategicpatch.CreateTwoWayMergePatch(curJson, modJson, appv1.Deployment{})
	if err != nil {
		return nil, err
	}

	if len(patch) == 0 || string(patch) == "{}" {
		return cur, nil
	}

	return provider.kubeClient.AppsV1().Deployments(cur.Namespace).Patch(context.TODO(),
		cur.Name,
		types.StrategicMergePatchType,
		patch,
		metav1.PatchOptions{})
}

func (provider *deploymentProvider) Remove(key string) error {
	provider.Lock()
	defer provider.Unlock()

	// remove service
	err := provider.kubeClient.CoreV1().
		Services(provider.namespace).
		Delete(context.TODO(), provider.serviceName(key), metav1.DeleteOptions{})
	if !apierrors.IsNotFound(err) {
		return err
	}

	// remove deployment
	err = provider.kubeClient.AppsV1().
		Deployments(provider.namespace).
		Delete(context.TODO(), provider.deployName(key), metav1.DeleteOptions{})
	if !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

func (provider *deploymentProvider) deployment(key string,
	template *corev1.PodTemplateSpec) (*appv1.Deployment, error) {
	// add pod labels same as pod selector
	podSelector := provider.podSelector(key)
	template.ObjectMeta.Labels = podSelector
	deployment := &appv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      provider.deployName(key),
			Namespace: provider.namespace,
		},
		Spec: appv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: podSelector,
			},
			Template: *template,
			Replicas: &provider.replicaCount,
		},
	}

	return deployment, nil
}

func (provider *deploymentProvider) podSelector(key string) labels.Set {
	return labels.Set{
		labelDeployExecutor:    "true",
		labelDeployExecutorKey: key,
	}
}

func (provider *deploymentProvider) serviceName(key string) string {
	return "meta-service-" + key
}

func (provider *deploymentProvider) deployName(key string) string {
	return "meta-service-" + key
}

func (provider *deploymentProvider) AddService(key string,
	servicePorts []corev1.ServicePort) (k8sresource.ResourceReference, error) {
	provider.Lock()
	defer provider.Unlock()

	serviceName := provider.serviceName(key)
	// check if service already exist
	service, err := provider.kubeClient.CoreV1().
		Services(provider.namespace).
		Get(context.TODO(), serviceName, metav1.GetOptions{})
	if err == nil {
		return k8sresource.ResourceReference{
			Namespace: service.Namespace,
			Name:      service.Name,
		}, nil
	} else if !apierrors.IsNotFound(err) {
		return k8sresource.ResourceReference{}, err
	}

	service, err = provider.kubeClient.CoreV1().
		Services(provider.namespace).
		Create(context.TODO(), &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: provider.namespace,
			},
			Spec: corev1.ServiceSpec{
				Selector: provider.podSelector(key),
				Ports:    servicePorts,
			},
		}, metav1.CreateOptions{})
	if err != nil {
		return k8sresource.ResourceReference{}, err
	}

	return k8sresource.ResourceReference{
		Namespace: service.Namespace,
		Name:      service.Name,
	}, err
}

func (provider *deploymentProvider) removeService(key string) error {
	// remove service
	err := provider.kubeClient.CoreV1().
		Services(provider.namespace).
		Delete(context.TODO(), key, metav1.DeleteOptions{})
	if !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
