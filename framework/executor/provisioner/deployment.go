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
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	CoreV1Client "k8s.io/client-go/kubernetes/typed/core/v1"

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
	ctx    context.Context
	logger *log.Entry
	// kubeClient   kubernetes.Interface
	appsClient   appsv1.AppsV1Interface
	coreClient   CoreV1Client.CoreV1Interface
	replicaCount int32
}

func newDeploymentProvider(ctx context.Context, kubeClient kubernetes.Interface) Interface {
	provider := &deploymentProvider{
		ctx:          ctx,
		logger:       log.WithField("module", moduleName),
		appsClient:   kubeClient.AppsV1(),
		coreClient:   kubeClient.CoreV1(),
		replicaCount: defaultReplicaCount,
	}

	return provider
}

func (provider *deploymentProvider) Start(workloadIndex string, namespace string, podTemplate *corev1.PodTemplateSpec) error {
	provider.Lock()
	defer provider.Unlock()
	provider.logger.Infof("Trying to start deployment workload[%s]", workloadIndex)
	// check if deploy already exist
	workload, err := provider.appsClient.Deployments(namespace).Get(context.TODO(), workloadIndex, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// if not available, create one
	if apierrors.IsNotFound(err) {
		provider.logger.Infof("Creating deployment workload[%s]", workloadIndex)
		deployment, err := provider.deployment(workloadIndex, namespace, podTemplate)
		if err != nil {
			return err
		}

		workload, err = provider.appsClient.Deployments(namespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}

	if *workload.Spec.Replicas == 0 {
		err = provider.scaleDeployment(workloadIndex, namespace, provider.replicaCount)
		if err != nil {
			return err
		}
	}

	return nil
}

func (provider *deploymentProvider) Stop(workloadIndex string, namespace string) error {
	provider.Lock()
	defer provider.Unlock()
	provider.logger.Infof("Stopping deployment workload for %s", workloadIndex)

	// scale down to zero
	return provider.scaleDeployment(workloadIndex, namespace, 0)
}

func (provider *deploymentProvider) scaleDeployment(workloadIndex string, namespace string, replicas int32) error {
	deploymentName := workloadIndex
	provider.logger.Infof("Scaling deployment[%s] to replica %d", deploymentName, replicas)
	deploy, err := provider.appsClient.Deployments(namespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
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

	return provider.appsClient.Deployments(cur.Namespace).Patch(context.TODO(),
		cur.Name,
		types.StrategicMergePatchType,
		patch,
		metav1.PatchOptions{})
}

func (provider *deploymentProvider) Remove(workloadIndex string, namespace string) error {
	provider.Lock()
	defer provider.Unlock()

	// remove service
	err := provider.coreClient.Services(namespace).Delete(context.TODO(), workloadIndex, metav1.DeleteOptions{})
	if !apierrors.IsNotFound(err) {
		return err
	}

	// remove deployment
	err = provider.appsClient.Deployments(namespace).Delete(context.TODO(), workloadIndex, metav1.DeleteOptions{})
	if !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

func (provider *deploymentProvider) deployment(workloadIndex string, namespace string,
	template *corev1.PodTemplateSpec) (*appv1.Deployment, error) {
	// add pod labels same as pod selector
	podSelector := provider.podSelector(workloadIndex)
	template.ObjectMeta.Labels = podSelector
	deployment := &appv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workloadIndex,
			Namespace: namespace,
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

func (provider *deploymentProvider) AddService(workloadIndex string, namespace string,
	servicePorts []corev1.ServicePort) (k8sresource.ResourceReference, error) {
	provider.Lock()
	defer provider.Unlock()

	serviceName := workloadIndex
	// check if service already exist
	service, err := provider.coreClient.
		Services(namespace).
		Get(context.TODO(), serviceName, metav1.GetOptions{})
	if err == nil {
		return k8sresource.ResourceReference{
			Namespace: service.Namespace,
			Name:      service.Name,
		}, nil
	} else if !apierrors.IsNotFound(err) {
		return k8sresource.ResourceReference{}, err
	}

	service, err = provider.coreClient.Services(namespace).
		Create(context.TODO(), &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: namespace,
			},
			Spec: corev1.ServiceSpec{
				Selector: provider.podSelector(workloadIndex),
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

func (provider *deploymentProvider) removeService(key string, namespace string) error {
	// remove service
	err := provider.coreClient.Services(namespace).
		Delete(context.TODO(), key, metav1.DeleteOptions{})
	if !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
