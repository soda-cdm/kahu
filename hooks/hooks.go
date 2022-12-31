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
	for _, hookSpec := range backupSpec.Hook.Resources {
		logger.Infof("Processing backup %s hook (%s)", phase, hookSpec.Name)
		hooks := hookSpec.PreHooks
		if phase == PostHookPhase {
			hooks = hookSpec.PostHooks
		}
		if len(hooks) == 0 {
			continue
		}
		hookSpec := CommonHookSpec{
			Name:              hookSpec.Name,
			IncludeNamespaces: hookSpec.IncludeNamespaces,
			ExcludeNamespaces: hookSpec.ExcludeNamespaces,
			LabelSelector:     hookSpec.LabelSelector,
			IncludeResources:  hookSpec.IncludeResources,
			ExcludeResources:  hookSpec.ExcludeResources,
			ContinueFlag:      hookSpec.ContinueHookIfContainerNotFound,
			Hooks:             hooks,
		}
		hookNamespaces := FilterHookNamespaces(allNamespaces,
			hookSpec.IncludeNamespaces,
			hookSpec.ExcludeNamespaces)

		for _, namespace := range hookNamespaces.UnsortedList() {
			logger.Infof("Processing namespace (%s) for hook %s", namespace, hookSpec.Name)
			filteredPods, err := GetAllPodsForNamespace(logger, h.kubeClient, namespace, hookSpec.LabelSelector,
				hookSpec.IncludeResources, hookSpec.ExcludeResources)
			if err != nil {
				logger.Errorf("unable to list pod for namespace %s", namespace)
				return err
			}
			for _, pod := range filteredPods.UnsortedList() {
				logger.Infof("Processing pod (%s) for hook %s", pod, hookSpec.Name)
				err := CommonExecuteHook(logger, h.kubeClient, h.PodCommandExecutor, hookSpec, namespace, pod, phase)
				if err != nil {
					logger.Errorf("failed to execute hook on pod %s, err %s", pod, err.Error())
					return err
				}
			}
		}
	}

	logger.Infof("%s hook stage finished", phase)
	return nil
}

// ExecuteHook executes the hooks in a container
func CommonExecuteHook(
	logger log.FieldLogger,
	client kubernetes.Interface,
	podCommandExecutor PodCommandExecutor,
	hookSpec CommonHookSpec,
	namespace string,
	name string,
	phase string,
) error {
	pod, err := client.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
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
		err := podCommandExecutor.ExecutePodCommand(logger, podMap,
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
	for _, hook := range hookSpec.Hooks {
		if hook.Exec != nil {
			// Check if we need to ignore container not found error
			if hookSpec.ContinueFlag && hook.Exec.Container != "" {
				err := CheckContainerExists(podMap, hook.Exec.Container)
				if err != nil {
					logger.Warningf("Container (%s) not found for hook (%s) in pod (%s), skipping execution",
						hook.Exec.Container, hookSpec.Name, name)
					continue
				}
			}
			err := podCommandExecutor.ExecutePodCommand(logger, podMap, namespace, name,
				hookSpec.Name, hook.Exec)
			if err != nil {
				logger.Errorf("hook failed on %s (%s) with %s", pod.Name, hookSpec.Name, err.Error())
				if hook.Exec.OnError == kahuapi.HookErrorModeFail {
					return err
				}
			}
		}
	}

	return nil
}

func GetAllPodsForNamespace(logger log.FieldLogger, client kubernetes.Interface, namespace string,
	selector *metav1.LabelSelector,
	includeResources, excludeResources []kahuapi.ResourceSpec) (sets.String, error) {

	podsForNamespace, err := getPodsFromPodResourceSpec(logger, client, namespace, selector, includeResources, excludeResources)
	if err != nil {
		logger.Infoln("failed to list pods", err.Error())
	}

	// Get all deployments
	deployPodLists, err := getPodsFromDeploymentResourceSpec(logger, client, namespace, includeResources, excludeResources)
	if err != nil {
		logger.Infoln("failed to list deployment pods", err.Error())
	}
	podsForNamespace = podsForNamespace.Union(deployPodLists)

	// Get all statefulsets
	statefulsetPodLists, err := getPodsFromStatefulSetResourceSpec(logger, client, namespace, includeResources, excludeResources)
	if err != nil {
		logger.Infoln("failed to list statefulsets pods", err.Error())
	}
	podsForNamespace = podsForNamespace.Union(statefulsetPodLists)

	// Get all replicasets
	replicasetPodLists, err := getPodsFromReplicaSetResourceSpec(logger, client, namespace, includeResources, excludeResources)
	if err != nil {
		logger.Infoln("failed to list replicasets pods", err.Error())
	}
	podsForNamespace = podsForNamespace.Union(replicasetPodLists)

	// Get all daemonset
	daemonsetPodLists, err := getPodsFromDaemonSetResourceSpec(logger, client, namespace, includeResources, excludeResources)
	if err != nil {
		logger.Infoln("failed to list daemonset pods", err.Error())
	}
	podsForNamespace = podsForNamespace.Union(daemonsetPodLists)

	return podsForNamespace, nil
}

func getPodsFromPodResourceSpec(logger log.FieldLogger, client kubernetes.Interface, namespace string,
	selector *metav1.LabelSelector,
	includeResources, excludeResources []kahuapi.ResourceSpec) (sets.String, error) {
	podsForNamespace := sets.NewString()
	var labelSelectors map[string]string
	if selector != nil {
		labelSelectors = selector.MatchLabels
	}
	pods, err := client.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.Set(labelSelectors).String(),
	})
	if err != nil {
		logger.Errorf("unable to list pod for namespace %+v", namespace)
		return podsForNamespace, err
	}

	// Filter pods for backup
	var allPods []string
	for _, pod := range pods.Items {
		allPods = append(allPods, pod.Name)
	}
	filteredPods := utils.FindMatchedStrings(utils.Pod,
		allPods,
		includeResources,
		excludeResources)

	podsForNamespace.Insert(filteredPods...)
	return podsForNamespace, nil
}

func getPodsFromDeploymentResourceSpec(logger log.FieldLogger, client kubernetes.Interface, namespace string,
	includeResources, excludeResources []kahuapi.ResourceSpec) (sets.String, error) {
	podsForNamespace := sets.NewString()
	// Get all deployment
	podLists, err := GetPodsFromDeployment(logger, client, namespace,
		includeResources, excludeResources)
	if err != nil {
		logger.Infof("failed to list deployment pods", err.Error())
		return podsForNamespace, nil
	}

	for _, pods := range podLists {
		for _, pod := range pods.Items {
			podsForNamespace.Insert(pod.Name)
		}
	}
	return podsForNamespace, nil
}

func getPodsFromStatefulSetResourceSpec(logger log.FieldLogger, client kubernetes.Interface, namespace string,
	includeResources, excludeResources []kahuapi.ResourceSpec) (sets.String, error) {
	podsForNamespace := sets.NewString()
	// Get all statefulset
	podLists, err := GetPodsFromStatefulset(logger, client, namespace,
		includeResources, excludeResources)
	if err != nil {
		logger.Infof("failed to list statefulsets pods", err.Error())
		return podsForNamespace, nil
	}

	for _, pods := range podLists {
		for _, pod := range pods.Items {
			podsForNamespace.Insert(pod.Name)
		}
	}
	return podsForNamespace, nil
}

func getPodsFromReplicaSetResourceSpec(logger log.FieldLogger, client kubernetes.Interface, namespace string,
	includeResources, excludeResources []kahuapi.ResourceSpec) (sets.String, error) {
	podsForNamespace := sets.NewString()
	// Get all replicaset
	podLists, err := GetPodsFromReplicaset(logger, client, namespace,
		includeResources, excludeResources)
	if err != nil {
		logger.Infof("failed to list replicasets pods", err.Error())
		return podsForNamespace, nil
	}

	for _, pods := range podLists {
		for _, pod := range pods.Items {
			podsForNamespace.Insert(pod.Name)
		}
	}
	return podsForNamespace, nil
}

func getPodsFromDaemonSetResourceSpec(logger log.FieldLogger, client kubernetes.Interface, namespace string,
	includeResources, excludeResources []kahuapi.ResourceSpec) (sets.String, error) {
	podsForNamespace := sets.NewString()
	// Get all daemonset
	podLists, err := GetPodsFromDaemonset(logger, client, namespace,
		includeResources, excludeResources)
	if err != nil {
		logger.Infof("failed to list daemonset pods", err.Error())
		return podsForNamespace, nil
	}

	for _, pods := range podLists {
		for _, pod := range pods.Items {
			podsForNamespace.Insert(pod.Name)
		}
	}
	return podsForNamespace, nil
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
			log.Warningf("Unable to parse provided timeout %s, using default", timeout)
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
