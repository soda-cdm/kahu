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

package hooks

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/kubernetes"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
)

const (
	podRestoreHookContainerAnnotationKey   = "post.hook.restore.kahu.io/container"
	podRestoreHookCommandAnnotationKey     = "post.hook.restore.kahu.io/command"
	podRestoreHookOnErrorAnnotationKey     = "post.hook.restore.kahu.io/on-error"
	podRestoreHookTimeoutAnnotationKey     = "post.hook.restore.kahu.io/exec-timeout"
	podRestoreHookWaitTimeoutAnnotationKey = "post.hook.restore.kahu.io/wait-timeout"
)

const (
	deploymentsResources  = "deployments"
	replicasetsResources  = "replicasets"
	statefulsetsResources = "statefulsets"
	daemonsetsResources   = "daemonsets"
)

type WaitHandler interface {
	WaitResource(
		ctx context.Context,
		log logrus.FieldLogger,
		robj *unstructured.Unstructured,
		selectorName string,
		selectorNamespace string,
		resourceType string,
	) error
}

type WaitExecHookHandler interface {
	HandleHooks(
		ctx context.Context,
		log logrus.FieldLogger,
		pod *v1.Pod,
		byContainer map[string][]PodExecRestoreHook,
	) []error
}

type ListWatchFactory interface {
	NewListWatch(namespace string, selector fields.Selector) cache.ListerWatcher
}

type DefaultListWatchFactory struct {
	PodsGetter cache.Getter
}

func (d *DefaultListWatchFactory) NewListWatch(namespace string, selector fields.Selector) cache.ListerWatcher {
	return cache.NewListWatchFromClient(d.PodsGetter, "pods", namespace, selector)
}

var _ ListWatchFactory = &DefaultListWatchFactory{}

type RListWatchFactory interface {
	NewListWatch(resource, namespace string, selector fields.Selector) cache.ListerWatcher
}

type ResourceListWatchFactory struct {
	ResourceGetter cache.Getter
}

func (r *ResourceListWatchFactory) NewListWatch(resource, namespace string, selector fields.Selector) cache.ListerWatcher {

	return cache.NewListWatchFromClient(r.ResourceGetter, resource, namespace, selector)
}

type ResourceWaitHandler struct {
	ResourceListWatchFactory ResourceListWatchFactory
}

var _ WaitHandler = &ResourceWaitHandler{}

type DefaultWaitExecHookHandler struct {
	ListWatchFactory   ListWatchFactory
	PodCommandExecutor PodCommandExecutor
}

var _ WaitExecHookHandler = &DefaultWaitExecHookHandler{}

func (e *DefaultWaitExecHookHandler) HandleHooks(
	ctx context.Context,
	log logrus.FieldLogger,
	pod *v1.Pod,
	byContainer map[string][]PodExecRestoreHook,
) []error {
	if pod == nil {
		return nil
	}

	// If hooks are defined for a container that does not exist in the pod log a warning and discard
	// those hooks to avoid waiting for a container that will never become ready. After that if
	// there are no hooks left to be executed return immediately.
	for containerName := range byContainer {
		if !podHasContainer(pod, containerName) {
			log.Warningf("Pod %s does not have container %s: discarding post-restore exec hooks", utils.NamespaceAndName(pod), containerName)
			delete(byContainer, containerName)
		}
	}
	if len(byContainer) == 0 {
		return nil
	}

	// Every hook in every container can have its own wait timeout. Rather than setting up separate
	// contexts for each, find the largest wait timeout for any hook that should be executed in
	// the pod and watch the pod for up to that long. Before executing any hook in a container,
	// check if that hook has a timeout and skip execution if expired.
	ctx, cancel := context.WithCancel(ctx)
	maxWait := maxHookWait(byContainer)
	// If no hook has a wait timeout then this function will continue waiting for containers to
	// become ready until the shared hook context is canceled.
	if maxWait > 0 {
		ctx, cancel = context.WithTimeout(ctx, maxWait)
	}
	waitStart := time.Now()

	var errors []error

	// The first time this handler is called after a container starts running it will execute all
	// pending hooks for that container. Subsequent invocations of this handler will never execute
	// hooks in that container. It uses the byContainer map to keep track of which containers have
	// not yet been observed to be running. It relies on the Informer not to be called concurrently.
	// When a container is observed running and its hooks are executed, the container is deleted
	// from the byContainer map. When the map is empty the watch is ended.
	handler := func(newObj interface{}) {
		newPod, ok := newObj.(*v1.Pod)
		if !ok {
			return
		}

		podLog := log.WithFields(
			logrus.Fields{
				"pod": utils.NamespaceAndName(newPod),
			},
		)

		if newPod.Status.Phase == v1.PodSucceeded || newPod.Status.Phase == v1.PodFailed {
			err := fmt.Errorf("Pod entered phase %s before some post-restore exec hooks ran", newPod.Status.Phase)
			podLog.Warning(err)
			cancel()
			return
		}

		for containerName, hooks := range byContainer {
			if !isContainerRunning(newPod, containerName) {
				podLog.Infof("Container %s is not running: post-restore hooks will not yet be executed", containerName)
				continue
			}
			podMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(newPod)
			if err != nil {
				podLog.WithError(err).Error("error unstructuring pod")
				cancel()
				return
			}

			// Sequentially run all hooks for the ready container. The container's hooks are not
			// removed from the byContainer map until all have completed so that if one fails
			// remaining unexecuted hooks can be handled by the outer function.
			for i, hook := range hooks {
				// This indicates to the outer function not to handle this hook as unexecuted in
				// case of terminating before deleting this container's slice of hooks from the
				// byContainer map.
				byContainer[containerName][i].executed = true

				hookLog := podLog.WithFields(
					logrus.Fields{
						"hookSource": hook.HookSource,
						"hookType":   "exec",
						"hookPhase":  "post",
					},
				)
				// Check the individual hook's wait timeout is not expired
				if hook.Hook.WaitTimeout.Duration != 0 && time.Since(waitStart) > hook.Hook.WaitTimeout.Duration {
					err := fmt.Errorf("hook %s in container %s expired before executing", hook.HookName, hook.Hook.Container)
					hookLog.Error(err)
					if hook.Hook.OnError == kahuapi.HookErrorModeFail {
						errors = append(errors, err)
						cancel()
						return
					}
				}
				eh := &kahuapi.ExecHook{
					Container: hook.Hook.Container,
					Command:   hook.Hook.Command,
					OnError:   hook.Hook.OnError,
					Timeout:   hook.Hook.Timeout,
				}
				if err := e.PodCommandExecutor.ExecutePodCommand(hookLog, podMap, newPod.Namespace, newPod.Name, hook.HookName, eh); err != nil {
					hookLog.WithError(err).Error("Error executing hook")
					if hook.Hook.OnError == kahuapi.HookErrorModeFail {
						errors = append(errors, err)
						cancel()
						return
					}
				}
			}
			delete(byContainer, containerName)
		}
		if len(byContainer) == 0 {
			cancel()
		}
	}

	selector := fields.OneTermEqualSelector("metadata.name", pod.Name)
	lw := e.ListWatchFactory.NewListWatch(pod.Namespace, selector)

	_, podWatcher := cache.NewInformer(lw, pod, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: handler,
		UpdateFunc: func(_, newObj interface{}) {
			handler(newObj)
		},
		DeleteFunc: func(obj interface{}) {
			err := fmt.Errorf("Pod %s deleted before all hooks were executed", utils.NamespaceAndName(pod))
			log.Error(err)
			cancel()
		},
	})

	podWatcher.Run(ctx.Done())

	// There are some cases where this function could return with unexecuted hooks: the pod may
	// be deleted, a hook with OnError mode Fail could fail, or it may timeout waiting for
	// containers to become ready.
	// Each unexecuted hook is logged as an error but only hooks with OnError mode Fail return
	// an error from this function.
	for _, hooks := range byContainer {
		for _, hook := range hooks {
			if hook.executed {
				continue
			}
			err := fmt.Errorf("Hook %s in container %s in pod %s not executed: %v", hook.HookName, hook.Hook.Container, utils.NamespaceAndName(pod), ctx.Err())
			hookLog := log.WithFields(
				logrus.Fields{
					"hookSource": hook.HookSource,
					"hookType":   "exec",
					"hookPhase":  "post",
				},
			)
			hookLog.Error(err)
			if hook.Hook.OnError == kahuapi.HookErrorModeFail {
				errors = append(errors, err)
			}
		}
	}

	return errors
}

func podHasContainer(pod *v1.Pod, containerName string) bool {
	if pod == nil {
		return false
	}
	for _, c := range pod.Spec.Containers {
		if c.Name == containerName {
			return true
		}
	}

	return false
}

func isContainerRunning(pod *v1.Pod, containerName string) bool {
	if pod == nil {
		return false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != containerName {
			continue
		}
		return cs.State.Running != nil
	}

	return false
}

// maxHookWait returns 0 to mean wait indefinitely. Any hook without a wait timeout will cause this
// function to return 0.
func maxHookWait(byContainer map[string][]PodExecRestoreHook) time.Duration {
	var maxWait time.Duration
	for _, hooks := range byContainer {
		for _, hook := range hooks {
			if hook.Hook.WaitTimeout.Duration <= 0 {
				return 0
			}
			if hook.Hook.WaitTimeout.Duration > maxWait {
				maxWait = hook.Hook.WaitTimeout.Duration
			}
		}
	}
	return maxWait
}

// getPodExecRestoreHookFromAnnotations returns an RestoreExecHook based on restore annotations, as
// long as the 'command' annotation is present. If it is absent, this returns nil.
func getPodExecRestoreHookFromAnnotations(annotations map[string]string, log logrus.FieldLogger) *kahuapi.RestoreExecHook {
	commandValue := annotations[podRestoreHookCommandAnnotationKey]
	if commandValue == "" {
		return nil
	}

	container := annotations[podRestoreHookContainerAnnotationKey]

	onError := kahuapi.HookErrorMode(annotations[podRestoreHookOnErrorAnnotationKey])
	if onError != kahuapi.HookErrorModeContinue && onError != kahuapi.HookErrorModeFail {
		onError = ""
	}

	var execTimeout time.Duration
	execTimeoutString := annotations[podRestoreHookTimeoutAnnotationKey]
	if execTimeoutString != "" {
		if temp, err := time.ParseDuration(execTimeoutString); err == nil {
			execTimeout = temp
		} else {
			log.Warn(errors.Wrapf(err, "Unable to parse exec timeout %s, ignoring", execTimeoutString))
		}
	}

	var waitTimeout time.Duration
	waitTimeoutString := annotations[podRestoreHookWaitTimeoutAnnotationKey]
	if waitTimeoutString != "" {
		if temp, err := time.ParseDuration(waitTimeoutString); err == nil {
			waitTimeout = temp
		} else {
			log.Warn(errors.Wrapf(err, "Unable to parse wait timeout %s, ignoring", waitTimeoutString))
		}
	}

	return &kahuapi.RestoreExecHook{
		Container:   container,
		Command:     parseStringToCommand(commandValue),
		OnError:     onError,
		Timeout:     metav1.Duration{Duration: execTimeout},
		WaitTimeout: metav1.Duration{Duration: waitTimeout},
	}
}

type PodExecRestoreHook struct {
	HookName   string
	HookSource string
	Hook       kahuapi.RestoreExecHook
	executed   bool
}

// GroupRestoreExecHooks returns a list of hooks to be executed in a pod grouped by
// container name. If an exec hook is defined in annotation that is used, else applicable exec
// hooks from the restore resource are accumulated.
func GroupRestoreExecHooks(
	// resourceRestoreHooks []ResourceRestoreHook,
	resourceHooks []kahuapi.RestoreResourceHookSpec,
	client kubernetes.Interface,
	pod *v1.Pod,
	log logrus.FieldLogger,
) (map[string][]PodExecRestoreHook, error) {
	byContainer := map[string][]PodExecRestoreHook{}

	if pod == nil || len(pod.Spec.Containers) == 0 {
		return byContainer, nil
	}
	metadata, err := meta.Accessor(pod)
	if err != nil {
		log.Errorf("Failed to get metadata for pod (%s)", err.Error())
		return nil, err
	}

	hookFromAnnotation := getPodExecRestoreHookFromAnnotations(metadata.GetAnnotations(), log)
	if hookFromAnnotation != nil {
		// default to first container in pod if unset
		if hookFromAnnotation.Container == "" {
			hookFromAnnotation.Container = pod.Spec.Containers[0].Name
		}
		byContainer[hookFromAnnotation.Container] = []PodExecRestoreHook{
			{
				HookName:   "<from-annotation>",
				HookSource: "annotation",
				Hook:       *hookFromAnnotation,
			},
		}
		return byContainer, nil
	}

	// No hook found on pod's annotations so check for applicable hooks from the restore spec
	labels := metadata.GetLabels()
	namespace := metadata.GetNamespace()
	for _, resourceHook := range resourceHooks {
		hookSpec := CommonHookSpec{
			Name:              resourceHook.Name,
			IncludeNamespaces: resourceHook.IncludeNamespaces,
			ExcludeNamespaces: resourceHook.ExcludeNamespaces,
			LabelSelector:     resourceHook.LabelSelector,
			IncludeResources:  resourceHook.IncludeResources,
			ExcludeResources:  resourceHook.ExcludeResources,
		}
		if !validateHook(log, client, hookSpec, pod.Name, namespace, labels) {
			continue
		}
		for _, rh := range resourceHook.PostHooks {
			if rh.Exec == nil {
				continue
			}
			named := PodExecRestoreHook{
				HookName:   resourceHook.Name,
				Hook:       *rh.Exec,
				HookSource: "RestoreSpec",
			}
			// default to first container in pod if unset, without mutating resource restore hook
			if named.Hook.Container == "" {
				named.Hook.Container = pod.Spec.Containers[0].Name
			}
			byContainer[named.Hook.Container] = append(byContainer[named.Hook.Container], named)
		}
	}

	return byContainer, nil
}

func (r *ResourceWaitHandler) WaitResource(
	ctx context.Context,
	log logrus.FieldLogger,
	robj *unstructured.Unstructured,
	selectorName string,
	selectorNamespace string,
	resourceType string,
) error {
	if robj == nil {
		return nil
	}

	timeout := "2m"

	maxWait, err := time.ParseDuration(timeout)
	if err != nil {
		log.Infof("ParseDuration failed", timeout)
		maxWait = 0
	}

	ctx, cancel := context.WithCancel(ctx)
	if maxWait > 0 {
		ctx, cancel = context.WithTimeout(ctx, maxWait)
	}
	// waitStart := time.Now()

	// var errors []error

	handler := func(newObj interface{}) {
		switch resourceType {
		case deploymentsResources:
			newResource, ok := newObj.(*appsv1.Deployment)
			if !ok {
				log.Errorf("invalid deployment (%s) object", newResource.Name)
				return
			}
			log.Infof("Creation of deployment (%s) completed", newResource.Name)
		case daemonsetsResources:
			newResource, ok := newObj.(*appsv1.DaemonSet)
			if !ok {
				log.Errorf("invalid daemonsets (%s) object", newResource.Name)
				return
			}
			log.Infof("Creation of daemonsets (%s) completed", newResource.Name)
		case replicasetsResources:
			newResource, ok := newObj.(*appsv1.ReplicaSet)
			if !ok {
				log.Errorf("invalid replicaset (%s) object", newResource.Name)
				return
			}
			log.Infof("Creation of replicaset (%s) completed", newResource.Name)
		case statefulsetsResources:
			newResource, ok := newObj.(*appsv1.StatefulSet)
			if !ok {
				log.Errorf("invalid statefulset (%s) object", newResource.Name)
				return
			}
			log.Infof("Creation of statefulset (%s) completed", newResource.Name)
		default:
		}
		cancel()
	}

	selector := fields.OneTermEqualSelector("metadata.name", selectorName)
	lw := r.ResourceListWatchFactory.NewListWatch(resourceType, selectorNamespace, selector)

	var obj runtime.Object
	switch resourceType {
	case deploymentsResources:
		deployment := new(appsv1.Deployment)
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(robj.UnstructuredContent(), &deployment)
		if err != nil {
			log.Infof("failed to get deployment object")
			return err
		}
		obj = runtime.Object(deployment)

	case daemonsetsResources:
		daemonset := new(appsv1.DaemonSet)
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(robj.UnstructuredContent(), &daemonset)
		if err != nil {
			log.Infof("failed to get daemonset object")
			return err
		}
		obj = runtime.Object(daemonset)
	case replicasetsResources:
		replicaset := new(appsv1.Deployment)
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(robj.UnstructuredContent(), &replicaset)
		if err != nil {
			log.Infof("failed to get replicaset object")
			return err
		}
		obj = runtime.Object(replicaset)
	case statefulsetsResources:
		statefulset := new(appsv1.Deployment)
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(robj.UnstructuredContent(), &statefulset)
		if err != nil {
			log.Infof("failed to get statefulset object")
			return err
		}
		obj = runtime.Object(statefulset)
	default:
		log.Errorf("invalid resources type")
		return nil
	}

	_, resWatcher := cache.NewInformer(lw, obj, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: handler,
		UpdateFunc: func(_, newObj interface{}) {
			handler(newObj)
		},
		DeleteFunc: func(obj interface{}) {
			err := fmt.Errorf("Resource %s deleted before all hooks were executed", resourceType)
			log.Error(err)
			cancel()
		},
	})

	resWatcher.Run(ctx.Done())
	return nil
}
