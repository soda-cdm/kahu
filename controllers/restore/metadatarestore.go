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

package restore

import (
	"context"
	"fmt"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
)

const (
	deploymentsResources  = "deployments"
	replicasetsResources  = "replicasets"
	statefulsetsResources = "statefulsets"
	daemonsetsResources   = "daemonsets"
)

type backupInfo struct {
	backup         *kahuapi.Backup
	backupLocation *kahuapi.BackupLocation
	backupProvider *kahuapi.Provider
}

func (ctx *restoreContext) syncMetadataRestore(restore *kahuapi.Restore,
	indexer cache.Indexer) (*kahuapi.Restore, error) {
	// metadata restore should be last step for restore
	if restore.Status.Stage == kahuapi.RestoreStageFinished &&
		restore.Status.State == kahuapi.RestoreStateCompleted {
		ctx.logger.Infof("Restore is finished already")
		return restore, nil
	}

	// process CRD resource first
	err := ctx.applyCRD(indexer, restore)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to apply CRD resources. %s", err)
		return ctx.updateRestoreStatus(restore)
	}

	// process resources
	err = ctx.applyIndexedResource(indexer, restore)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to apply resources. %s", err)
		return ctx.updateRestoreStatus(restore)
	}

	return ctx.updateRestoreState(restore, kahuapi.RestoreStateCompleted)
}

func (ctx *restoreContext) applyCRD(indexer cache.Indexer, restore *kahuapi.Restore) error {
	crds, err := indexer.ByIndex(backupObjectResourceIndex, crdName)
	if err != nil {
		ctx.logger.Errorf("error fetching CRDs from indexer %s", err)
		return err
	}

	unstructuredCRDs := make([]*unstructured.Unstructured, 0)
	for _, crd := range crds {
		ubstructure, ok := crd.(*unstructured.Unstructured)
		if !ok {
			ctx.logger.Warningf("Restore index cache has invalid object type. %v",
				reflect.TypeOf(crd))
			continue
		}
		unstructuredCRDs = append(unstructuredCRDs, ubstructure)
	}

	for _, unstructuredCRD := range unstructuredCRDs {
		err := ctx.applyResource(unstructuredCRD, restore)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ctx *restoreContext) applyIndexedResource(indexer cache.Indexer, restore *kahuapi.Restore) error {
	indexedResources := indexer.List()
	unstructuredResources := make([]*unstructured.Unstructured, 0)

	for _, indexedResource := range indexedResources {
		unstructuredResource, ok := indexedResource.(*unstructured.Unstructured)
		if !ok {
			ctx.logger.Warningf("Restore index cache has invalid object type. %v",
				reflect.TypeOf(unstructuredResource))
			continue
		}

		// ignore exempted resource for restore
		if excludeRestoreResources.Has(unstructuredResource.GetObjectKind().GroupVersionKind().Kind) {
			continue
		}

		unstructuredResources = append(unstructuredResources, unstructuredResource)
	}

	for _, unstructuredResource := range unstructuredResources {
		ctx.logger.Infof("Processing %s/%s for restore",
			unstructuredResource.GroupVersionKind(),
			unstructuredResource.GetName())
		err := ctx.applyResource(unstructuredResource, restore)
		if err != nil {
			return err
		}
	}

	// Wait for all of the restore hook goroutines to be done, which is
	// only possible once all of their errors have been received by the loop
	// below, then close the hooksErrs channel so the loop terminates.
	go func() {
		ctx.logger.Info("Waiting for all post-restore-exec hooks to complete")

		ctx.hooksWaitGroup.Wait()
		close(ctx.hooksErrs)
	}()
	var errs []string
	var err error
	for err = range ctx.hooksErrs {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		ctx.logger.Errorf("Failure while executing post exec hooks: %+v", errs)
		return err
	}
	ctx.logger.Info("post hook stage finished")

	return nil
}

func (ctx *restoreContext) applyResource(resource *unstructured.Unstructured, restore *kahuapi.Restore) error {
	gvk := resource.GroupVersionKind()
	gvr, _, err := ctx.discoveryHelper.ByGroupVersionKind(gvk)
	if err != nil {
		ctx.logger.Errorf("unable to fetch GroupVersionResource for %s", gvk)
		return err
	}

	resourceClient := ctx.dynamicClient.Resource(gvr).Namespace(resource.GetNamespace())

	err = ctx.preProcessResource(resource)
	if err != nil {
		ctx.logger.Errorf("unable to preprocess resource %s. %s", resource.GetName(), err)
		return err
	}

	existingResource, err := resourceClient.Get(context.TODO(), resource.GetName(), metav1.GetOptions{})
	if err == nil && existingResource != nil {
		// resource already exist. ignore creating
		ctx.logger.Warningf("ignoring %s.%s/%s restore. Resource already exist",
			existingResource.GetKind(),
			existingResource.GetAPIVersion(),
			existingResource.GetName())
		return nil
	}

	kind := resource.GetKind()

	switch kind {
	case utils.Deployment, utils.DaemonSet, utils.StatefulSet, utils.Replicaset:
		err = ctx.applyWorkloadResources(resource, restore, resourceClient)
		if err != nil {
			return err
		}
	case utils.Pod:
		err = ctx.applyPodResources(resource, restore, resourceClient)
		if err != nil {
			return err
		}
	default:
		_, err = resourceClient.Create(context.TODO(), resource, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			// ignore if already exist
			return err
		}
		return nil
	}

	return nil
}

func (ctx *restoreContext) preProcessResource(resource *unstructured.Unstructured) error {
	// ensure namespace existence
	if err := ctx.ensureNamespace(resource.GetNamespace()); err != nil {
		return err
	}
	// remove resource version
	resource.SetResourceVersion("")

	// TODO(Amit Roushan): Add resource specific handling
	return nil
}

func (ctx *restoreContext) ensureNamespace(namespace string) error {
	// check if namespace exist
	if namespace == "" {
		// ignore if namespace is empty
		// possibly cluster scope resource
		return nil
	}

	// check if namespace exist
	n, err := ctx.kubeClient.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
	if err == nil && n != nil {
		// namespace already exist
		return nil
	}

	// create namespace
	_, err = ctx.kubeClient.CoreV1().
		Namespaces().
		Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}, metav1.CreateOptions{})
	if err != nil {
		ctx.logger.Errorf("Unable to ensure namespace. %s", err)
		return err
	}

	return nil
}

func (ctx *restoreContext) applyWorkloadResources(resource *unstructured.Unstructured,
	restore *kahuapi.Restore,
	resourceClient dynamic.ResourceInterface) error {
	ctx.logger.Infof("Start processing init hook for Pod resource (%s)", resource.GetName())
	var labelSelectors map[string]string
	var replicas int

	switch resource.GetKind() {
	case utils.Deployment:
		// get all pods for deployment
		var deployment *appsv1.Deployment
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &deployment)
		if err != nil {
			return err
		}

		deploy := deployment.DeepCopy()
		if deploy.Spec.Selector.MatchLabels != nil {
			labelSelectors = deploy.Spec.Selector.MatchLabels
		}
		replicas = int(*deploy.Spec.Replicas)
	case utils.DaemonSet:
		// get all pods for daemonset
		var daemonset *appsv1.DaemonSet
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &daemonset)
		if err != nil {
			return err
		}

		deploy := daemonset.DeepCopy()
		if deploy.Spec.Selector.MatchLabels != nil {
			labelSelectors = deploy.Spec.Selector.MatchLabels
		}
		replicas = 1 // One daemon per node
	case utils.StatefulSet:
		// get all pods for statefulset
		var statefulset *appsv1.StatefulSet
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &statefulset)
		if err != nil {
			return err
		}

		deploy := statefulset.DeepCopy()
		if deploy.Spec.Selector.MatchLabels != nil {
			labelSelectors = deploy.Spec.Selector.MatchLabels
		}
		replicas = int(*deploy.Spec.Replicas)
	case utils.Replicaset:
		// get all pods for replicaset
		var replicaset *appsv1.ReplicaSet
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &replicaset)
		if err != nil {
			return err
		}

		deploy := replicaset.DeepCopy()
		if deploy.Spec.Selector.MatchLabels != nil {
			labelSelectors = deploy.Spec.Selector.MatchLabels
		}
		replicas = int(*deploy.Spec.Replicas)
	default:
	}

	return ctx.applyWorkloadPodResources(resource, restore, resourceClient, labelSelectors, replicas)
}

func (ctx *restoreContext) applyWorkloadPodResources(resource *unstructured.Unstructured,
	restore *kahuapi.Restore,
	resourceClient dynamic.ResourceInterface,
	labelSelectors map[string]string,
	replicas int) error {
	// Done is called when all pods for the resource is scheduled for hooks
	defer ctx.hooksWaitGroup.Done()

	_, err := resourceClient.Create(context.TODO(), resource, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		// ignore if already exist
		ctx.logger.Infof("failed to create %s", resource.GetName())
		return err
	}

	namespace := resource.GetNamespace()
	time.Sleep(2 * time.Second)

	pods, err := ctx.kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.Set(labelSelectors).String(),
	})
	if err != nil {
		ctx.logger.Errorf("unable to list pod for resource %s-%s", namespace, resource.GetName())
		return err
	}

	if len(pods.Items) < replicas {
		time.Sleep(10 * time.Second)
		pods, err = ctx.kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.Set(labelSelectors).String(),
		})
		if err != nil {
			ctx.logger.Errorf("unable to list pod for resource %s-%s", namespace, resource.GetName())
			return err
		}
	}

	return nil
}

func (ctx *restoreContext) applyPodResources(resource *unstructured.Unstructured,
	restore *kahuapi.Restore,
	resourceClient dynamic.ResourceInterface) error {
	ctx.logger.Infof("Start processing init hook for Pod resource (%s)", resource.GetName())

	_, err := resourceClient.Create(context.TODO(), resource, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		// ignore if already exist
		return err
	}

	return nil
}
