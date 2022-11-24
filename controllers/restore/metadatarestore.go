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
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/hooks"
	"github.com/soda-cdm/kahu/utils"
)

const (
	deploymentsResources  = "deployments"
	replicasetsResources  = "replicasets"
	statefulsetsResources = "statefulsets"
	daemonsetsResources   = "daemonsets"
)

var defaultRestorePriorities = []string{
	"CustomResourceDefinition",
	"MutatingWebhookConfiguration",
	"ValidatingWebhookConfiguration",
	"ClusterRole",
	"ClusterRoleBinding",
	"Namespace",
	"Role",
	"RoleBinding",
	"StorageClass",
	"VolumeSnapshotClass",
	"VolumeSnapshotContent",
	"VolumeSnapshot",
	"PersistentVolume",
	"PersistentVolumeClaim",
	"Secret",
	"ConfigMap",
	"ServiceAccount",
	"LimitRange",
	"Pod",
	"ReplicaSet",
	"DaemonSet",
	"Deployment",
	"StatefulSet",
	"Job",
	"Cronjob",
}

type backupInfo struct {
	backup         *kahuapi.Backup
	backupLocation *kahuapi.BackupLocation
	backupProvider *kahuapi.Provider
}

type RestorePodSet struct {
	pods sets.String
}
type RestoreNamespaceSet struct {
	namespaces sets.String
	Pods       []RestorePodSet
}

type RestoreHookSet struct {
	Hooks      []hooks.CommonHookSpec
	Namespaces []RestoreNamespaceSet
}

func (ctx *restoreContext) collectAllRestoreHooksPods(restore *kahuapi.Restore) (*RestoreHookSet, error) {
	ctx.logger.Infof("Collecting all pods for restore hook execution %s", restore.Name)
	restoreHookSet := RestoreHookSet{
		Hooks:      make([]hooks.CommonHookSpec, len(restore.Spec.Hooks.Resources)),
		Namespaces: make([]RestoreNamespaceSet, len(restore.Spec.Hooks.Resources)),
	}

	namespaces, err := ctx.kubeClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		ctx.logger.Errorf("unable to list namespaces for restore hooks %s", err.Error())
		return nil, err
	}
	allNamespaces := sets.NewString()
	for _, namespace := range namespaces.Items {
		allNamespaces.Insert(namespace.Name)
	}

	for index, hookSpec := range restore.Spec.Hooks.Resources {
		ctx.logger.Infof("Collecting all pods for restore post hook (%s)", hookSpec.Name)
		if len(hookSpec.PostHooks) == 0 {
			continue
		}
		commonHooks := make([]kahuapi.ResourceHook, 0)
		for _, postHook := range hookSpec.PostHooks {
			commonHook := kahuapi.ResourceHook{
				Exec: &kahuapi.ExecHook{
					Container: postHook.Exec.Container,
					Command:   postHook.Exec.Command,
					OnError:   postHook.Exec.OnError,
					Timeout:   postHook.Exec.Timeout,
				},
			}
			commonHooks = append(commonHooks, commonHook)
		}
		hookSpec := hooks.CommonHookSpec{
			Name:              hookSpec.Name,
			IncludeNamespaces: hookSpec.IncludeNamespaces,
			ExcludeNamespaces: hookSpec.ExcludeNamespaces,
			LabelSelector:     hookSpec.LabelSelector,
			IncludeResources:  hookSpec.IncludeResources,
			ExcludeResources:  hookSpec.ExcludeResources,
			ContinueFlag:      true,
			Hooks:             commonHooks,
		}
		hookNamespaces := hooks.FilterHookNamespaces(allNamespaces,
			hookSpec.IncludeNamespaces,
			hookSpec.ExcludeNamespaces)
		restoreHookSet.Hooks[index] = hookSpec
		restoreHookSet.Namespaces[index] = RestoreNamespaceSet{
			namespaces: hookNamespaces,
			Pods:       make([]RestorePodSet, hookNamespaces.Len()),
		}
		for nsIndex, namespace := range hookNamespaces.UnsortedList() {
			ctx.logger.Infof("Collecting all pods for restore post hook namespace (%s)", namespace)
			filteredPods, err := hooks.GetAllPodsForNamespace(ctx.logger,
				ctx.kubeClient, namespace, hookSpec.LabelSelector,
				hookSpec.IncludeResources, hookSpec.ExcludeResources)
			if err != nil {
				ctx.logger.Errorf("unable to list pod for namespace %s", namespace)
				continue
			}
			ctx.logger.Infof("Collected pods: %+v", filteredPods.UnsortedList())
			restoreHookSet.Namespaces[index].Pods[nsIndex] = RestorePodSet{
				pods: filteredPods,
			}
		}

	}

	return &restoreHookSet, nil
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
	restore, err = ctx.applyIndexedResource(indexer, restore)
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

func (ctx *restoreContext) applyIndexedResource(indexer cache.Indexer, restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	restoreHookSet, err := ctx.collectAllRestoreHooksPods(restore)
	if err != nil {
		ctx.logger.Errorf("failed to collect all resources for %v", restore.Name)
		return restore, err
	}
	ctx.logger.Infof("Restore metadata for %v started", restore.Name)
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

	// prioritise the resources
	unstructuredResources = ctx.prioritiseResources(unstructuredResources)

	for _, unstructuredResource := range unstructuredResources {
		ctx.logger.Infof("Processing %s/%s for restore", unstructuredResource.GroupVersionKind(),
			unstructuredResource.GetName())
		if err := ctx.applyResource(unstructuredResource, restore); err != nil {
			return restore, err
		}
	}

	restore, err = ctx.updateRestoreState(restore, kahuapi.RestoreStateCompleted)
	if err != nil {
		ctx.logger.Errorf("failed to update metadata state: %s", err.Error())
	}
	restore.Status.State = kahuapi.RestoreStateProcessing
	if restore, err = ctx.updateRestoreStage(restore, kahuapi.RestoreStagePostHook); err != nil {
		ctx.logger.Errorf("failed to update post hook stage: %s", err.Error())
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
	for err := range ctx.hooksErrs {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		ctx.logger.Errorf("Failure while executing post exec hooks: %+v", errs)
		return restore, errors.New("failure while executing post exec hooks")
	}

	// handle external hooks sets
	for hookIndex, hookSpec := range restoreHookSet.Hooks {
		ctx.logger.Infof("Processing external restore post hook (%s)", hookSpec.Name)
		for nsIndex, namespace := range restoreHookSet.Namespaces[hookIndex].namespaces.UnsortedList() {
			ctx.logger.Infof("Processing namespace (%s) for hook %s", namespace, hookSpec.Name)
			for _, pod := range restoreHookSet.Namespaces[hookIndex].Pods[nsIndex].pods.UnsortedList() {
				ctx.logger.Infof("Processing pod (%s) for hook %s", pod, hookSpec.Name)
				if len(hookSpec.Hooks) == 0 {
					continue
				}
				err := hooks.CommonExecuteHook(ctx.logger, ctx.kubeClient, ctx.podCommandExecutor,
					hookSpec, namespace, pod, hooks.PostHookPhase)
				if err != nil {
					ctx.logger.Errorf("failed to execute hook on pod %s, err %s", pod, err.Error())
					return restore, err
				}
			}
		}
	}
	restore, err = ctx.updateRestoreState(restore, kahuapi.RestoreStateCompleted)
	if err != nil {
		ctx.logger.Errorf("failed to update posthook state: %s", err.Error())
	}
	ctx.logger.Info("post hook stage finished")

	return restore, nil
}

func canonicalName(resource *unstructured.Unstructured) string {
	return resource.GetNamespace() + ":" +
		resource.GetAPIVersion() + ":" +
		resource.GetKind() + ":" +
		resource.GetName()
}

func (ctx *restoreContext) prioritiseResources(
	resources []*unstructured.Unstructured) []*unstructured.Unstructured {
	prioritiseResources := make([]*unstructured.Unstructured, 0)

	// sort resources based on priority
	sortedResources := sets.NewString()
	for _, kind := range defaultRestorePriorities {
		for _, resource := range resources {
			if resource.GetKind() == kind {
				sortedResources.Insert(canonicalName(resource))
				prioritiseResources = append(prioritiseResources, resource)
			}
		}
	}

	// append left out resources
	for _, resource := range resources {
		if sortedResources.Has(canonicalName(resource)) {
			continue
		}
		prioritiseResources = append(prioritiseResources, resource)
	}

	return prioritiseResources
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

func (ctx *restoreContext) deleteRestoredResource(resource *kahuapi.RestoreResource, restore *kahuapi.Restore) error {
	gvk := resource.GroupVersionKind()
	gvr, _, err := ctx.discoveryHelper.ByGroupVersionKind(gvk)
	if err != nil {
		ctx.logger.Errorf("unable to fetch GroupVersionResource for %s", gvk)
		return err
	}

	resourceClient := ctx.dynamicClient.Resource(gvr).Namespace(resource.Namespace)
	bNeedCleanup := ctx.isCleanupNeeded(resourceClient, resource, restore.Name)
	if bNeedCleanup != true {
		return nil
	}

	err = resourceClient.Delete(context.TODO(), resource.ResourceName, metav1.DeleteOptions{})
	if err != nil {
		ctx.logger.Errorf("Failed to delete resource %s, error: %s", resource.ResourceName, err)
		return err
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

func (ctx *restoreContext) isCleanupNeeded(resourceClient dynamic.ResourceInterface,
	resource *kahuapi.RestoreResource, restoreName string) bool {
	existingResource, err := resourceClient.Get(context.TODO(), resource.ResourceName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return false
	} else if apierrors.IsNotFound(err) {
		ctx.logger.Infof("resource %s not found, skipping cleanup", resource.ResourceName)
		return false
	}

	annots := existingResource.GetAnnotations()
	if annots == nil || annots[annRestoreIdentifier] != restoreName {
		ctx.logger.Infof("restore identifier annotation not found for resource %s, skipping cleanup",
			resource.ResourceName)
		return false
	}

	if existingResource.GetKind() == "PersistentVolumeClaim" ||
		existingResource.GetKind() == "PersistentVolume" {
		return false
	}

	ctx.logger.Infof("Resource %s, kind %s selected for cleanup", resource.ResourceName, existingResource.GetKind())
	return true
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

func (ctx *restoreContext) isMatchingReplicaSetExist(resource *unstructured.Unstructured, backedupResName string) bool {
	var labelSelectors map[string]string
	var replicaset *appsv1.ReplicaSet
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &replicaset)
	if err != nil {
		return false
	}

	rsObj := replicaset.DeepCopy()
	if rsObj.Spec.Selector.MatchLabels != nil {
		labelSelectors = rsObj.Spec.Selector.MatchLabels
	}
	if len(labelSelectors) == 0 {
		return false
	}

	replicaSets, err := ctx.kubeClient.AppsV1().ReplicaSets(resource.GetNamespace()).Get(context.TODO(),
		backedupResName, metav1.GetOptions{})
	if err != nil {
		return false
	}
	if labels.Equals(labelSelectors, replicaSets.Spec.Selector.MatchLabels) {
		return true
	}
	return false
}


func (ctx *restoreContext) isMatchingStatefulSetExist(resource *unstructured.Unstructured, backedupResName string) bool {
	var labelSelectors map[string]string
	var statefulset *appsv1.StatefulSet
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &statefulset)
	if err != nil {
		return false
	}

	statefulSetObj := statefulset.DeepCopy()
	if statefulSetObj.Spec.Selector.MatchLabels != nil {
		labelSelectors = statefulSetObj.Spec.Selector.MatchLabels
	}
	if len(labelSelectors) == 0 {
		return false
	}

	statefulSets, err := ctx.kubeClient.AppsV1().StatefulSets(resource.GetNamespace()).Get(context.TODO(),
		backedupResName, metav1.GetOptions{})
	if err != nil {
		return false
	}

	if labels.Equals(labelSelectors, statefulSets.Spec.Selector.MatchLabels) {
		return true
	}
	return false
}

func (ctx *restoreContext) isMatchingDaemonSetExist(resource *unstructured.Unstructured, backedupResName string) bool {
	var labelSelectors map[string]string
	var daemonset *appsv1.DaemonSet
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &daemonset)
	if err != nil {
		return false
	}

	daemonsetObj := daemonset.DeepCopy()
	if daemonsetObj.Spec.Selector.MatchLabels != nil {
		labelSelectors = daemonsetObj.Spec.Selector.MatchLabels
	}
	if len(labelSelectors) == 0 {
		return false
	}

	daemonsets, err := ctx.kubeClient.AppsV1().DaemonSets(resource.GetNamespace()).Get(context.TODO(),
		backedupResName, metav1.GetOptions{})
	if err != nil {
		return false
	}
	if labels.Equals(labelSelectors, daemonsets.Spec.Selector.MatchLabels) {
		return true
	}
	return false
}

func (ctx *restoreContext) isMatchingDeployExist(resource *unstructured.Unstructured, backedupResName string) bool {
	var labelSelectors map[string]string
	var deployment *appsv1.Deployment
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &deployment)
	if err != nil {
		return false
	}

	deploy := deployment.DeepCopy()
	if deploy.Spec.Selector.MatchLabels != nil {
		labelSelectors = deploy.Spec.Selector.MatchLabels
	}
	if len(labelSelectors) == 0 {
		return false
	}

	deployments, err := ctx.kubeClient.AppsV1().Deployments(resource.GetNamespace()).Get(context.TODO(),
		backedupResName, metav1.GetOptions{})
	if err != nil {
		return false
	}

	if labels.Equals(labelSelectors, deployments.Spec.Selector.MatchLabels) {
		return true
	}
	return false
}

func (ctx *restoreContext) isMatchingPodExist(resource *unstructured.Unstructured, backedupResName string) bool {
	var labelSelectors map[string]string
	var pod *corev1.Pod
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &pod)
	if err != nil {
		return false
	}

	podObj := pod.DeepCopy()
	if podObj.Labels != nil {
		labelSelectors = podObj.Labels
	}
	if len(labelSelectors) == 0 {
		return false
	}

	pods, err := ctx.kubeClient.CoreV1().Pods(resource.GetNamespace()).Get(context.TODO(), backedupResName,
		metav1.GetOptions{})
	if err != nil {
		return false
	}

	if labels.Equals(labelSelectors, pods.Labels) {
		return true
	}
	return false
}

func (ctx *restoreContext) checkWorkloadConflict(restore *kahuapi.Restore, resource *unstructured.Unstructured) bool {
	resName := resource.GetName()
	resPrefix := restore.Spec.ResourcePrefix

	backedupResName := string(bytes.TrimPrefix([]byte(resName), []byte(resPrefix)))

	switch resource.GetKind() {
	case utils.Pod:
		return ctx.isMatchingPodExist(resource, backedupResName)
	case utils.Deployment:
		return ctx.isMatchingDeployExist(resource, backedupResName)
	case utils.DaemonSet:
		return ctx.isMatchingDaemonSetExist(resource, backedupResName)
	case utils.StatefulSet:
		return ctx.isMatchingStatefulSetExist(resource, backedupResName)
	case utils.Replicaset:
		return ctx.isMatchingReplicaSetExist(resource, backedupResName)
	default:
		return false
	}
}

func (ctx *restoreContext) applyWorkloadResources(resource *unstructured.Unstructured,
	restore *kahuapi.Restore,
	resourceClient dynamic.ResourceInterface) error {
	ctx.logger.Infof("Start processing init hook for Pod resource (%s)", resource.GetName())
	var resourceType string
	var labelSelectors map[string]string
	var replicas int

	switch resource.GetKind() {
	case utils.Deployment:
		// get all pods for deployment
		resourceType = deploymentsResources
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
		resourceType = daemonsetsResources
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
		resourceType = statefulsetsResources
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
		resourceType = replicasetsResources
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

	ctx.hooksWaitGroup.Add(1)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		wh := &hooks.ResourceWaitHandler{
			ResourceListWatchFactory: hooks.ResourceListWatchFactory{
				ResourceGetter: ctx.kubeClient.AppsV1().RESTClient(),
			},
		}
		err := wh.WaitResource(ctx.hooksContext, ctx.logger, resource,
			resource.GetName(), resource.GetNamespace(), resourceType)
		if err != nil {
			ctx.logger.Errorf("error executing wait for %s-%s ", resourceType, resource.GetName())
		}
	}()

	return ctx.applyWorkloadPodResources(resource, restore, resourceClient, &wg, labelSelectors, replicas)
}

func (ctx *restoreContext) applyWorkloadPodResources(resource *unstructured.Unstructured,
	restore *kahuapi.Restore,
	resourceClient dynamic.ResourceInterface,
	wg *sync.WaitGroup,
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

	// wait for workload resource to ready
	wg.Wait()

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

	for _, pod := range pods.Items {
		podMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pod)
		if err != nil {
			ctx.logger.Errorf("failed to convert pod (%s) to unstructured, %+v", pod.Name, err.Error())
			return err
		}

		podUns := &unstructured.Unstructured{Object: podMap}
		// post exec hooks
		ctx.waitExec(restore.Spec.Hooks.Resources, podUns)
	}
	return nil
}

func (ctx *restoreContext) applyPodResources(resource *unstructured.Unstructured,
	restore *kahuapi.Restore,
	resourceClient dynamic.ResourceInterface) error {
	ctx.logger.Infof("Start processing init hook for Pod resource (%s)", resource.GetName())
	hookHandler := hooks.InitHooksHandler{}
	var initRes *unstructured.Unstructured

	initRes, err := hookHandler.HandleInitHook(ctx.logger, ctx.kubeClient, hooks.Pods, resource, &restore.Spec.Hooks)
	if err != nil {
		ctx.logger.Errorf("Failed to process init hook for Pod resource (%s)", resource.GetName())
		return err
	}
	if initRes != nil {
		resource = initRes
	}

	_, err = resourceClient.Create(context.TODO(), resource, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		// ignore if already exist
		return err
	}

	// post hook
	ctx.waitExec(restore.Spec.Hooks.Resources, resource)
	return nil
}

// waitExec executes hooks in a restored pod's containers when they become ready.
func (ctx *restoreContext) waitExec(resourceHooks []kahuapi.RestoreResourceHookSpec, createdObj *unstructured.Unstructured) {
	ctx.hooksWaitGroup.Add(1)
	go func() {
		// Done() will only be called after all errors have been successfully sent
		// on the ctx.resticErrs channel.
		defer ctx.hooksWaitGroup.Done()

		pod := new(corev1.Pod)
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(createdObj.UnstructuredContent(), &pod); err != nil {
			ctx.logger.Errorf("error converting unstructured pod : %s", err)
			ctx.hooksErrs <- err
			return
		}
		execHooksByContainer, err := hooks.GroupRestoreExecHooks(
			resourceHooks,
			ctx.kubeClient,
			pod,
			ctx.logger,
		)
		if err != nil {
			ctx.logger.Errorf("error getting exec hooks for pod %s/%s", pod.Namespace, pod.Name)
			ctx.hooksErrs <- err
			return
		}

		if errs := ctx.waitExecHookHandler.HandleHooks(ctx.hooksContext, ctx.logger, pod, execHooksByContainer); len(errs) > 0 {
			ctx.hooksCancelFunc()

			for _, err := range errs {
				// Errors are already logged in the HandleHooks method.
				ctx.hooksErrs <- err
			}
		}
	}()
}
