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
	"fmt"
	"strings"

	uuid "github.com/gofrs/uuid"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
)

const (
	podRestoreHookInitContainerImageAnnotationKey   = "init.hook.restore.kahu.io/container-image"
	podRestoreHookInitContainerNameAnnotationKey    = "init.hook.restore.kahu.io/container-name"
	podRestoreHookInitContainerCommandAnnotationKey = "init.hook.restore.kahu.io/command"
	podRestoreHookInitContainerTimeoutAnnotationKey = "init.hook.restore.kahu.io/timeout"
)

var (
	Pods = schema.GroupResource{Group: "", Resource: "pods"}
)

type InitHooks interface {
	HandleInitHook(hookSpec *kahuapi.HookSpec, phase string) error
	// IsHooksSpecified(resources []kahuapi.ResourceHookSpec, phase string) bool
}

type InitHooksHandler struct {
}

// HandleInitHook runs the restore hooks for an item.
// If the item is a pod, then hooks are chosen to be run as follows:
// If the pod has the appropriate annotations specifying the hook action, then hooks from the annotation are run
// Otherwise, the supplied ResourceRestoreHooks are applied.
func (i *InitHooksHandler) HandleInitHook(
	logger log.FieldLogger,
	client kubernetes.Interface,
	groupResource schema.GroupResource,
	obj runtime.Unstructured,
	hookSpec *kahuapi.RestoreHookSpec,
) (*unstructured.Unstructured, error) {
	// We only support hooks on pods right now
	if groupResource != Pods {
		return nil, nil
	}

	metadata, err := meta.Accessor(obj)
	if err != nil {
		log.Errorf("Failed to get accessor (%s)", err.Error())
		return nil, err
	}
	pod := new(v1.Pod)
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), pod); err != nil {
		log.Errorf("Failed to convert pod (%s) to podMap", pod.Name)
		return nil, err
	}

	initContainers := []v1.Container{}

	hooksFromAnnotations := getInitRestoreHookFromAnnotation(utils.NamespaceAndName(pod), metadata.GetAnnotations())
	if hooksFromAnnotations != nil {
		log.Infof("Handling InitRestoreHooks from pod annotations")
		initContainers = append(initContainers, hooksFromAnnotations.InitContainers...)
	} else {
		log.Infof("Handling InitRestoreHooks from RestoreSpec")
		// pod did not have the annotations appropriate for restore hooks
		// running applicable ResourceRestoreHooks supplied.
		namespace := metadata.GetNamespace()
		labels := labels.Set(metadata.GetLabels())

		for _, resources := range hookSpec.Resources {
			hSpec := CommonHookSpec{
				Name:              resources.Name,
				IncludeNamespaces: resources.IncludeNamespaces,
				ExcludeNamespaces: resources.ExcludeNamespaces,
				LabelSelector:     resources.LabelSelector,
				IncludeResources:  resources.IncludeResources,
				ExcludeResources:  resources.ExcludeResources,
			}
			if !validateHook(logger, client, hSpec, pod.Name, namespace, labels) {
				continue
			}
			for _, hook := range resources.PostHooks {
				if hook.Init != nil {
					initContainers = append(initContainers, hook.Init.InitContainers...)
				}
			}
		}
	}

	pod.Spec.InitContainers = append(initContainers, pod.Spec.InitContainers...)
	log.Infof("Returning pod %s/%s with %d init container(s)", pod.Namespace, pod.Name, len(pod.Spec.InitContainers))

	podMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pod)
	if err != nil {
		log.Errorf("Failed to convert pod (%s) to podMap", pod.Name)
		return nil, err
	}
	return &unstructured.Unstructured{Object: podMap}, nil
}

func getInitRestoreHookFromAnnotation(podName string, annotations map[string]string) *kahuapi.InitRestoreHook {
	containerImage := annotations[podRestoreHookInitContainerImageAnnotationKey]
	containerName := annotations[podRestoreHookInitContainerNameAnnotationKey]
	command := annotations[podRestoreHookInitContainerCommandAnnotationKey]
	if containerImage == "" {
		log.Infof("Pod %s has no %s annotation, no initRestoreHook in annotation", podName, podRestoreHookInitContainerImageAnnotationKey)
		return nil
	}
	if command == "" {
		log.Infof("RestoreHook init container for pod %s is using container's  %s default entrypoint", podName, containerImage)
	}
	if containerName == "" {
		uid, err := uuid.NewV4()
		uuidStr := "dummyuuid"
		if err != nil {
			log.Errorf("Failed to generate UUID for container name")
		} else {
			uuidStr = strings.Split(uid.String(), "-")[0]
		}
		containerName = fmt.Sprintf("restore-init-%s", uuidStr)
		log.Infof("Pod %s has no %s annotation, using generated name %s for initContainer", podName, podRestoreHookInitContainerNameAnnotationKey, containerName)
	}

	return &kahuapi.InitRestoreHook{
		InitContainers: []v1.Container{
			{
				Image:   containerImage,
				Name:    containerName,
				Command: parseStringToCommand(command),
			},
		},
	}
}
