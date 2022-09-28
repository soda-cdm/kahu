/*
Copyright 2022 The SODA Authors.
Copyright 2020 the Velero contributors.

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

// Package hooks implement pre and post hook execution
package hooks

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
)

const (
	PreHookPhase  = "pre"
	PostHookPhase = "post"
	PodResource   = utils.Pod
)

const (
	podHookContainerAnnotationKey = "hook.backup.kahu.io/container"
	podHookCommandAnnotationKey   = "hook.backup.kahu.io/command"
	podHookOnErrorAnnotationKey   = "hook.backup.kahu.io/on-error"
	podHookTimeoutAnnotationKey   = "hook.backup.kahu.io/timeout"
)

type Hooks interface {
	ExecuteHook(logger log.FieldLogger, hookSpec *kahuapi.BackupSpec, phase string) error
}

type hooksHandler struct {
	restConfig         *restclient.Config
	kubeClient         kubernetes.Interface
	PodCommandExecutor PodCommandExecutor
}

// NewHooks creates hooks exection handler
func NewHooks(kubeClient kubernetes.Interface, restConfig *restclient.Config,
	podCommandExecutor PodCommandExecutor) (Hooks, error) {

	h := &hooksHandler{
		restConfig:         restConfig,
		kubeClient:         kubeClient,
		PodCommandExecutor: podCommandExecutor,
	}

	return h, nil
}

// ExecuteHook will handle executions of backup hooks
func (h *hooksHandler) ExecuteHook(logger log.FieldLogger, backupSpec *kahuapi.BackupSpec, phase string) error {
	logger = logger.WithField("hook-phase", phase)
	logger.Infof("Check and start hook execution for %s hook", phase)
	namespaces, err := h.kubeClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		logger.Errorf("unable to list namespaces for hooks %s", err.Error())
		return err
	}
	allNamespaces := sets.NewString()
	for _, namespace := range namespaces.Items {
		allNamespaces.Insert(namespace.Name)
	}

	filteredHookNamespaces := filterHookNamespaces(allNamespaces,
		backupSpec.IncludeNamespaces,
		backupSpec.ExcludeNamespaces)

	for _, namespace := range filteredHookNamespaces.UnsortedList() {
		// Get label selector
		filteredPods, err := h.getAllPodsForNamespace(logger, namespace, backupSpec)
		if err != nil {
			logger.Errorf("unable to list pod for namespace %s", namespace)
			return err
		}
		for _, pod := range filteredPods {
			err := h.executeHook(logger, backupSpec.Hook.Resources, namespace, pod, phase)
			if err != nil {
				logger.Errorf("failed to execute hook on pod %s, err %s", pod, err.Error())
				return err
			}
		}
	}

	logger.Infof("%s hook stage finished", phase)
	return nil
}

// ExecuteHook executes the hooks in a container
func (h *hooksHandler) executeHook(
	logger log.FieldLogger,
	resourceHooks []kahuapi.ResourceHookSpec,
	namespace string,
	name string,
	phase string,
) error {
	pod, err := h.kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		logger.Errorf("Unable to get pod object for name %s", pod)
		return err
	}

	podMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		logger.Errorf("error unstructuring pod (%s)", pod.Name)
		return nil
	}

	// Handle hooks from annotations
	hookExec := getHooksSpecFromAnnotations(pod.GetAnnotations(), phase)
	if hookExec != nil {
		err := h.PodCommandExecutor.ExecutePodCommand(logger, podMap,
			namespace, name, "annotationHook", hookExec)
		if err != nil {
			logger.Errorf("error %s, while executing annotation hook", err.Error())
			if hookExec.OnError == kahuapi.HookErrorModeFail {
				return err
			}
		}
		// Skip normal hooks
		return nil
	}

	labels := labels.Set(pod.GetLabels())
	for _, resourceHook := range resourceHooks {

		hookSpec := commonHookSpec{
			Name:              resourceHook.Name,
			IncludeNamespaces: resourceHook.IncludeNamespaces,
			ExcludeNamespaces: resourceHook.ExcludeNamespaces,
			LabelSelector:     resourceHook.LabelSelector,
			IncludeResources:  resourceHook.IncludeResources,
			ExcludeResources:  resourceHook.ExcludeResources,
		}
		if !validateHook(logger, hookSpec, name, namespace, labels) {
			continue
		}
		hooks := resourceHook.PreHooks
		if phase == PostHookPhase {
			hooks = resourceHook.PostHooks
		}

		for _, hook := range hooks {
			if hook.Exec != nil {
				// Check if we need to ignore container not found error
				if resourceHook.ContinueHookIfContainerNotFound && hook.Exec.Container != "" {
					err := CheckContainerExists(podMap, hook.Exec.Container)
					if err != nil {
						logger.Warningf("Container (%s) not found for hook (%s) in pod (%s), skipping execution",
							hook.Exec.Container, resourceHook.Name, name)
						continue
					}
				}
				err := h.PodCommandExecutor.ExecutePodCommand(logger, podMap, namespace, name,
					resourceHook.Name, hook.Exec)
				if err != nil {
					logger.Errorf("hook failed on %s (%s) with %s", pod.Name, resourceHook.Name, err.Error())
					if hook.Exec.OnError == kahuapi.HookErrorModeFail {
						return err
					}
				}
			}
		}
	}

	return nil
}

func (h *hooksHandler) getAllPodsForNamespace(logger log.FieldLogger, namespace string, backupSpec *kahuapi.BackupSpec) ([]string, error) {
	var labelSelectors map[string]string
	if backupSpec.Label != nil {
		labelSelectors = backupSpec.Label.MatchLabels
	}
	pods, err := h.kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.Set(labelSelectors).String(),
	})
	if err != nil {
		logger.Errorf("unable to list pod for namespace %s", namespace)
		return nil, err
	}

	// Filter pods for backup
	var allPods []string
	for _, pod := range pods.Items {
		allPods = append(allPods, pod.Name)
	}
	filteredPods := utils.FindMatchedStrings(utils.Pod,
		allPods,
		backupSpec.IncludeResources,
		backupSpec.ExcludeResources)

	podsForNamespace := sets.NewString()
	podsForNamespace.Insert(filteredPods...)

	// Get all deployments
	podLists, err := GetPodsFromDeployment(logger, h.kubeClient, namespace,
		backupSpec.IncludeResources, backupSpec.ExcludeResources)
	if err != nil {
		logger.Infof("failed to list deployment pods", err.Error())
	}

	for _, pods := range podLists {
		for _, pod := range pods.Items {
			podsForNamespace.Insert(pod.Name)
		}
	}

	// Get all statefulsets
	podLists, err = GetPodsFromStatefulset(logger, h.kubeClient, namespace,
		backupSpec.IncludeResources, backupSpec.ExcludeResources)
	if err != nil {
		logger.Infof("failed to list statefulsets pods", err.Error())
	}

	for _, pods := range podLists {
		for _, pod := range pods.Items {
			podsForNamespace.Insert(pod.Name)
		}
	}
	// Get all replicasets
	podLists, err = GetPodsFromReplicaset(logger, h.kubeClient, namespace,
		backupSpec.IncludeResources, backupSpec.ExcludeResources)
	if err != nil {
		logger.Infof("failed to list replicasets pods", err.Error())
	}

	for _, pods := range podLists {
		for _, pod := range pods.Items {
			podsForNamespace.Insert(pod.Name)
		}
	}
	// Get all daemonset
	podLists, err = GetPodsFromDaemonset(logger, h.kubeClient, namespace,
		backupSpec.IncludeResources, backupSpec.ExcludeResources)
	if err != nil {
		logger.Infof("failed to list daemonset pods", err.Error())
	}

	for _, pods := range podLists {
		for _, pod := range pods.Items {
			podsForNamespace.Insert(pod.Name)
		}
	}

	return podsForNamespace.UnsortedList(), nil
}

func getHooksSpecFromAnnotations(annotations map[string]string, stage string) *kahuapi.ExecHook {
	commands := annotations[fmt.Sprintf("%v.%v", stage, podHookCommandAnnotationKey)]
	if commands == "" {
		return nil
	}

	container := annotations[fmt.Sprintf("%v.%v", stage, podHookContainerAnnotationKey)]
	onError := kahuapi.HookErrorMode(annotations[fmt.Sprintf("%v.%v", stage, podHookOnErrorAnnotationKey)])
	if onError != kahuapi.HookErrorModeContinue && onError != kahuapi.HookErrorModeFail {
		onError = ""
	}
	timeout := annotations[fmt.Sprintf("%v.%v", stage, podHookTimeoutAnnotationKey)]
	var duration time.Duration
	if timeout != "" {
		if temp, err := time.ParseDuration(timeout); err == nil {
			duration = temp
		} else {
			log.Warnf("Unable to parse provided timeout %s, using default", timeout)
		}
	}
	execSpec := kahuapi.ExecHook{
		Command:   parseStringToCommand(commands),
		Container: container,
		OnError:   onError,
		Timeout:   metav1.Duration{Duration: duration},
	}

	return &execSpec
}
