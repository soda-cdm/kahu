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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
)

const (
	defaultTimeout = 60 * time.Second
	PreHookPhase   = "pre"
	PostHookPhase  = "post"
	hookResource   = "Pod"
)

const (
	podHookContainerAnnotationKey = "hook.kahu.io/container"
	podHookCommandAnnotationKey   = "hook.kahu.io/command"
	podHookOnErrorAnnotationKey   = "hook.kahu.io/on-error"
	podHookTimeoutAnnotationKey   = "hook.kahu.io/timeout"
)

// ResourceHook is a hook for a given resource.
type ResourceHook struct {
	Name         string
	Pre          []v1beta1.ResourceHook
	Post         []v1beta1.ResourceHook
	namespacesIn []string
	namespacesEx []string
	resourcesIn  []v1beta1.ResourceIncluder
	resourcesEx  []v1beta1.ResourceIncluder
	selector     labels.Selector
}

// Hooks is the handler implementing hooks execution interfaces
type Hooks struct {
	config        *rest.Config
	resourceHooks []ResourceHook
}

// NewHooks creates hooks exection handler
func NewHooks(config *rest.Config, hookSpecs []v1beta1.ResourceHookSpec) (*Hooks, error) {
	resHooks, err := getHooksSpec(hookSpecs)
	if err != nil {
		log.Error("fail to parse hook specs")
		return nil, err
	}
	h := &Hooks{
		config:        config,
		resourceHooks: resHooks,
	}

	return h, err
}

// UpdateHooksSpec is to re-configure hooks
func (h *Hooks) UpdateHooksSpec(hookSpecs []v1beta1.ResourceHookSpec) error {
	var err error
	h.resourceHooks, err = getHooksSpec(hookSpecs)
	return err
}

func getHooksSpec(hookSpecs []v1beta1.ResourceHookSpec) ([]ResourceHook, error) {
	hooks := make([]ResourceHook, 0, len(hookSpecs))

	for _, spec := range hookSpecs {
		hook := ResourceHook{
			Name:         spec.Name,
			Pre:          spec.PreHooks,
			Post:         spec.PostHooks,
			namespacesIn: spec.IncludedNamespaces,
			namespacesEx: spec.ExcludedNamespaces,
			resourcesIn:  spec.IncludedResources,
			resourcesEx:  spec.ExcludedResources,
		}
		if spec.LabelSelector != nil {
			labelSelector, err := metav1.LabelSelectorAsSelector(spec.LabelSelector)
			if err != nil {
				log.Error("error getting label selector for hook")
				continue
			}
			hook.selector = labelSelector
		}
		hooks = append(hooks, hook)
	}
	log.Info("updated hooks execution spec")
	return hooks, nil
}

// ExecuteHook executes the hooks in a container
func (h *Hooks) ExecuteHook(
	pod *v1.Pod,
	stage string,
) error {
	namespace := pod.GetNamespace()
	name := pod.GetName()

	// Handle hooks from annotations
	hookExec := getHooksSpecFromAnnotations(pod.GetAnnotations(), stage)
	if hookExec != nil {
		err := h.executePodCommand(pod, namespace, name, "annotationHook", hookExec)
		if err != nil {
			log.Errorf("error %v, while executing annotation hook %v", err, hookExec)
			if hookExec.OnError == v1beta1.HookErrorModeFail {
				return err
			}
		}
		// Skip normal hooks
		return nil
	}

	labels := labels.Set(pod.GetLabels())
	for _, resourceHook := range h.resourceHooks {
		if !h.checkResource(resourceHook, name, namespace, labels) {
			continue
		}

		hooks := resourceHook.Pre
		if stage == PostHookPhase {
			hooks = resourceHook.Post
		}

		for _, hook := range hooks {
			if hook.Exec != nil {
				err := h.executePodCommand(pod, namespace, name, resourceHook.Name, hook.Exec)
				if err != nil {
					if hook.Exec.OnError == v1beta1.HookErrorModeFail {
						return err
					}
				}
			}
		}
	}

	return nil
}

func getHooksSpecFromAnnotations(annotations map[string]string, stage string) *v1beta1.ExecHook {
	commands := annotations[fmt.Sprintf("%v.%v", stage, podHookCommandAnnotationKey)]
	if commands == "" {
		return nil
	}
	var command []string

	if commands[0] == '[' {
		if err := json.Unmarshal([]byte(commands), &command); err != nil {
			command = []string{commands}
		}
	} else {
		command = append(command, commands)
	}
	container := annotations[fmt.Sprintf("%v.%v", stage, podHookContainerAnnotationKey)]
	onError := annotations[fmt.Sprintf("%v.%v", stage, podHookOnErrorAnnotationKey)]
	timeout := annotations[fmt.Sprintf("%v.%v", stage, podHookTimeoutAnnotationKey)]
	var duration time.Duration
	if timeout != "" {
		if temp, err := time.ParseDuration(timeout); err == nil {
			duration = temp
		} else {
			log.Warnf("Unable to parse provided timeout %s, using default", timeout)
		}
	}
	execSpec := v1beta1.ExecHook{
		Command:   command,
		Container: container,
		OnError:   v1beta1.HookErrorMode(onError),
		Timeout:   metav1.Duration{Duration: duration},
	}

	return &execSpec
}

func (h *Hooks) executePodCommand(pod *v1.Pod, namespace, name, hookName string, hook *v1beta1.ExecHook) error {
	if pod == nil {
		return errors.New("pod is required")
	}
	if namespace == "" {
		return errors.New("namespace is required")
	}
	if name == "" {
		return errors.New("name is required")
	}
	if hookName == "" {
		return errors.New("hookName is required")
	}
	if hook == nil {
		return errors.New("hook is required")
	}

	localHook := *hook
	if localHook.Container == "" {
		if err := setDefaultHookContainer(pod, &localHook); err != nil {
			return err
		}
	} else if err := ensureContainerExists(pod, localHook.Container); err != nil {
		return err
	}

	if len(localHook.Command) == 0 {
		return errors.New("command is required")
	}

	switch localHook.OnError {
	case v1beta1.HookErrorModeFail, v1beta1.HookErrorModeContinue:
		// use the specified value
	default:
		// default to fail
		localHook.OnError = v1beta1.HookErrorModeFail
	}

	if localHook.Timeout.Duration == 0 {
		localHook.Timeout.Duration = defaultTimeout
	}

	log := log.WithFields(
		log.Fields{
			"hookName":      hookName,
			"hookNamespace": namespace,
			"hookContainer": localHook.Container,
			"hookCommand":   localHook.Command,
			"hookOnError":   localHook.OnError,
			"hookTimeout":   localHook.Timeout,
		},
	)

	log.Info("running exec hook")
	executor := HookContainerExecuter{}
	err := executor.ContainerExecute(
		h.config,
		localHook.Container,
		localHook.Command,
		namespace,
		name,
		localHook.Timeout,
	)

	return err
}

func (h *Hooks) checkInclude(in []string, ex []string, key string) bool {
	setIn := sets.NewString().Insert(in...)
	setEx := sets.NewString().Insert(ex...)
	if setEx.Has(key) {
		return false
	}
	return len(in) == 0 || setIn.Has(key)
}

func (h *Hooks) checkResource(rg ResourceHook, name, namespace string, labels labels.Set) bool {
	// Check namespace
	if !h.checkInclude(rg.namespacesIn, rg.namespacesEx, namespace) {
		log.Errorf("invalid namespace (%s) for hook execution", namespace)
		return false
	}
	// Check resource
	var resourceNames []string
	resourceNames = append(resourceNames, name)
	names := utils.FindMatchedStrings(hookResource, resourceNames, rg.resourcesIn, rg.resourcesEx)
	setNames := sets.NewString().Insert(names...)
	if !setNames.Has(name) {
		log.Errorf("invalid resource (%s) for hook execution", name)
		return false
	}
	// Check label
	if rg.selector != nil && !rg.selector.Matches(labels) {
		log.Errorf("invalid label for hook execution")
		return false
	}
	return true
}

func ensureContainerExists(pod *v1.Pod, container string) error {
	for _, c := range pod.Spec.Containers {
		if c.Name == container {
			return nil
		}
	}
	log.Infof("container %s not in available", container)
	return errors.New("no such container")
}

func setDefaultHookContainer(pod *v1.Pod, hook *v1beta1.ExecHook) error {
	if len(pod.Spec.Containers) < 1 {
		return errors.New("need at least 1 container")
	}

	hook.Container = pod.Spec.Containers[0].Name
	return nil
}

// ContainerExecuter defines interface for executor in container
type ContainerExecuter interface {
	ContainerExecute()
}

// HookContainerExecuter implements ContainerExecute interface
type HookContainerExecuter struct{}

// ContainerExecute implements execution in a named container
func (ce *HookContainerExecuter) ContainerExecute(
	config *rest.Config,
	containerName string,
	commands []string,
	namespace string,
	podName string,
	timeout metav1.Duration,
) error {
	log := log.WithField("container", containerName)

	client, err := utils.GetK8sClient(config)
	if err != nil {
		log.Errorf("failed to get k8s client")
		return err
	}
	req := client.CoreV1().RESTClient().Post().Resource("pods").Name(podName).Namespace(namespace).SubResource("exec")
	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		log.Errorf("failed to add functions to scheme")
		return err
	}
	parameterCodec := runtime.NewParameterCodec(scheme)
	req.VersionedParams(&v1.PodExecOptions{
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
		Container: containerName,
		Command:   commands,
	}, parameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		log.Errorf("failed to create remove executor")
		return err
	}

	errCh := make(chan error)
	var stdout, stderr bytes.Buffer

	go func() {
		err := exec.Stream(remotecommand.StreamOptions{
			Stdin:  nil,
			Stdout: &stdout,
			Stderr: &stderr,
			Tty:    false,
		})
		errCh <- err
	}()
	var timeoutCh <-chan time.Time
	if timeout.Duration > 0 {
		timer := time.NewTimer(timeout.Duration)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	select {
	case err = <-errCh:
	case <-timeoutCh:
		return errors.New("timed out while executing hook")
	}

	log.Infof("STDOUT from container: %v", stdout.String())
	log.Infof("STDERR from container: %v", stderr.String())
	return nil
}
