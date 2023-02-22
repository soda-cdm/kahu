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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	utilscache "github.com/soda-cdm/kahu/utils/cache"
	"github.com/soda-cdm/kahu/utils/k8sresource"
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

//type RestorePodSet struct {
//	pods sets.String
//}

//type RestoreNamespaceSet struct {
//	namespaces sets.String
//	Pods       []RestorePodSet
//}

//type RestoreHookSet struct {
//	Hooks      []hooks.CommonHookSpec
//	Namespaces []RestoreNamespaceSet
//}

func needRestore(resource k8sresource.Resource, restore *kahuapi.Restore) bool {
	for _, backupResource := range restore.Status.Resources {
		if backupResource.ResourceID() == resource.ResourceID() &&
			backupResource.Status == kahuapi.Completed {
			return false
		}
	}

	return true
}

func (ctrl *controller) prioritiseResources(
	resources []k8sresource.Resource) []k8sresource.Resource {
	prioritiseResources := make([]k8sresource.Resource, 0)

	// sort resources based on priority
	sortedResources := sets.NewString()
	for _, kind := range defaultRestorePriorities {
		for _, resource := range resources {
			if resource.GetKind() == kind {
				sortedResources.Insert(resource.ResourceID())
				prioritiseResources = append(prioritiseResources, resource)
			}
		}
	}

	// append left out resources
	for _, resource := range resources {
		if sortedResources.Has(resource.ResourceID()) {
			continue
		}
		prioritiseResources = append(prioritiseResources, resource)
	}

	return prioritiseResources
}

func (ctrl *controller) restoreResources(restore *kahuapi.Restore,
	restoreResources []k8sresource.Resource,
	resourceCache utilscache.Interface,
	depTree map[string]sets.String) (*kahuapi.Restore, error) {
	var err error
	for _, resource := range restoreResources {
		if restore, err = ctrl.restoreResource(restore, resource, resourceCache, depTree); err != nil {
			return restore, err
		}
	}
	return restore, nil
}

func (ctrl *controller) restoreResource(restore *kahuapi.Restore,
	restoreResource k8sresource.Resource,
	resourceCache utilscache.Interface,
	depTree map[string]sets.String) (*kahuapi.Restore, error) {
	if !needRestore(restoreResource, restore) {
		return restore, nil
	}

	ctrl.logger.Infof("Restoring resource [%s] for %s", restoreResource.ResourceID(), restore.Name)
	var err error
	restore, err = ctrl.updateRestoreResourceState(restoreResource, kahuapi.Processing, restore)
	if err != nil {
		return restore, err
	}

	// get all dependencies
	deps, err := ctrl.getDependencies(restoreResource, resourceCache, depTree)
	if err != nil {
		return restore, err
	}

	// restore dependencies first
	restore, err = ctrl.restoreResources(restore, deps, resourceCache, depTree)
	if err != nil {
		return restore, err
	}

	// restore resources
	err = ctrl.applyRestore(restoreResource)
	if err != nil {
		return restore, err
	}

	ctrl.logger.Infof("Restoring resource [%s] for %s completed", restoreResource.ResourceID(), restore.Name)
	return ctrl.updateRestoreResourceState(restoreResource, kahuapi.Completed, restore)
}

func (ctrl *controller) getDependencies(restoreResource k8sresource.Resource,
	resourceCache utilscache.Interface,
	depTree map[string]sets.String) ([]k8sresource.Resource, error) {
	deps := make([]k8sresource.Resource, 0)
	resourceKey, err := resourceCache.GetKey(restoreResource)
	if err != nil {
		return deps, err
	}

	depKeys, ok := depTree[resourceKey]
	if !ok {
		return deps, nil
	}

	for _, depKey := range depKeys.List() {
		resource, exist, err := resourceCache.GetByKey(depKey)
		if err != nil {
			return deps, err
		}
		if !exist {
			return deps, fmt.Errorf("dependent resource donot exist for %s",
				restoreResource.ResourceID())
		}
		deps = append(deps, resource)
	}

	return deps, nil
}

func (ctrl *controller) applyRestore(resource k8sresource.Resource) error {
	gvk := resource.GroupVersionKind()
	gvr, _, err := ctrl.discoveryHelper.ByGroupVersionKind(gvk)
	if err != nil {
		ctrl.logger.Errorf("unable to fetch GroupVersionResource for %s", gvk)
		return err
	}

	resourceClient := ctrl.dynamicClient.Resource(gvr).Namespace(resource.GetNamespace())
	err = ctrl.preProcessResource(resource)
	if err != nil {
		ctrl.logger.Errorf("unable to preprocess resource %s. %s", resource.GetName(), err)
		return err
	}

	existingResource, err := resourceClient.Get(context.TODO(), resource.GetName(), metav1.GetOptions{})
	if err == nil && existingResource != nil {
		// resource already exist. ignore creating
		ctrl.logger.Warningf("ignoring %s.%s/%s restore. Resource already exist",
			existingResource.GetKind(),
			existingResource.GetAPIVersion(),
			existingResource.GetName())
		return nil
	}

	_, err = resourceClient.Create(context.TODO(), &resource.Unstructured, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		// ignore if already exist
		return err
	}

	return nil
}

//func (ctrl *controller) collectAllRestoreHooksPods(restore *kahuapi.Restore) (*RestoreHookSet, error) {
//	ctrl.logger.Infof("Collecting all pods for restore hook execution %s", restore.Name)
//	restoreHookSet := RestoreHookSet{
//		Hooks:      make([]hooks.CommonHookSpec, len(restore.Spec.Hooks.Resources)),
//		Namespaces: make([]RestoreNamespaceSet, len(restore.Spec.Hooks.Resources)),
//	}
//
//	namespaces, err := ctrl.kubeClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
//	if err != nil {
//		ctrl.logger.Errorf("unable to list namespaces for restore hooks %s", err.Error())
//		return nil, err
//	}
//	allNamespaces := sets.NewString()
//	for _, namespace := range namespaces.Items {
//		allNamespaces.Insert(namespace.Name)
//	}
//
//	for index, hookSpec := range restore.Spec.Hooks.Resources {
//		ctrl.logger.Infof("Collecting all pods for restore post hook (%s)", hookSpec.Name)
//		if len(hookSpec.PostHooks) == 0 {
//			continue
//		}
//		commonHooks := make([]kahuapi.ResourceHook, 0)
//		for _, postHook := range hookSpec.PostHooks {
//			commonHook := kahuapi.ResourceHook{
//				Exec: &kahuapi.ExecHook{
//					Container: postHook.Exec.Container,
//					Command:   postHook.Exec.Command,
//					OnError:   postHook.Exec.OnError,
//					Timeout:   postHook.Exec.Timeout,
//				},
//			}
//			commonHooks = append(commonHooks, commonHook)
//		}
//		hookSpec := hooks.CommonHookSpec{
//			Name:              hookSpec.Name,
//			IncludeNamespaces: hookSpec.IncludeNamespaces,
//			ExcludeNamespaces: hookSpec.ExcludeNamespaces,
//			LabelSelector:     hookSpec.LabelSelector,
//			IncludeResources:  hookSpec.IncludeResources,
//			ExcludeResources:  hookSpec.ExcludeResources,
//			ContinueFlag:      true,
//			Hooks:             commonHooks,
//		}
//		hookNamespaces := hooks.FilterHookNamespaces(allNamespaces,
//			hookSpec.IncludeNamespaces,
//			hookSpec.ExcludeNamespaces)
//		restoreHookSet.Hooks[index] = hookSpec
//		restoreHookSet.Namespaces[index] = RestoreNamespaceSet{
//			namespaces: hookNamespaces,
//			Pods:       make([]RestorePodSet, hookNamespaces.Len()),
//		}
//		for nsIndex, namespace := range hookNamespaces.UnsortedList() {
//			ctrl.logger.Infof("Collecting all pods for restore post hook namespace (%s)", namespace)
//			filteredPods, err := hooks.GetAllPodsForNamespace(ctrl.logger,
//				ctrl.kubeClient, namespace, hookSpec.LabelSelector,
//				hookSpec.IncludeResources, hookSpec.ExcludeResources)
//			if err != nil {
//				ctrl.logger.Errorf("unable to list pod for namespace %s", namespace)
//				continue
//			}
//			ctrl.logger.Infof("Collected pods: %+v", filteredPods.UnsortedList())
//			restoreHookSet.Namespaces[index].Pods[nsIndex] = RestorePodSet{
//				pods: filteredPods,
//			}
//		}
//
//	}
//
//	return &restoreHookSet, nil
//}

//func (ctrl *controller) syncMetadataRestore(restore *kahuapi.Restore,
//	indexer cache.Indexer) (*kahuapi.Restore, error) {
//	// metadata restore should be last step for restore
//	if restore.Status.State == kahuapi.RestoreStateCompleted {
//		ctrl.logger.Infof("Restore is finished already")
//		return restore, nil
//	}
//
//	// process CRD resource first
//	err := ctrl.applyCRD(indexer, restore)
//	if err != nil {
//		restore.Status.State = kahuapi.RestoreStateFailed
//		restore.Status.FailureReason = fmt.Sprintf("Failed to apply CRD resources. %s", err)
//		return ctrl.updateRestoreStatus(restore)
//	}
//
//	// process resources
//	restore, err = ctrl.applyIndexedResource(indexer, restore)
//	if err != nil {
//		restore.Status.State = kahuapi.RestoreStateFailed
//		restore.Status.FailureReason = fmt.Sprintf("Failed to apply resources. %s", err)
//		return ctrl.updateRestoreStatus(restore)
//	}
//
//	return ctrl.updateRestoreState(restore, kahuapi.RestoreStateCompleted)
//}
//
//func (ctrl *controller) applyCRD(indexer cache.Indexer, restore *kahuapi.Restore) error {
//	crds, err := indexer.ByIndex(backupObjectResourceIndex, crdName)
//	if err != nil {
//		ctrl.logger.Errorf("error fetching CRDs from indexer %s", err)
//		return err
//	}
//
//	unstructuredCRDs := make([]k8sresource.Resource, 0)
//	for _, crd := range crds {
//		ubstructure, ok := crd.(k8sresource.Resource)
//		if !ok {
//			ctrl.logger.Warningf("Restore index cache has invalid object type. %v",
//				reflect.TypeOf(crd))
//			continue
//		}
//		unstructuredCRDs = append(unstructuredCRDs, ubstructure)
//	}
//
//	for _, unstructuredCRD := range unstructuredCRDs {
//		err := ctrl.applyResource(unstructuredCRD, restore)
//		if err != nil {
//			return err
//		}
//	}
//	return nil
//}
//
//func (ctrl *controller) applyIndexedResource(indexer cache.Indexer, restore *kahuapi.Restore) (*kahuapi.Restore, error) {
//	restoreHookSet, err := ctrl.collectAllRestoreHooksPods(restore)
//	if err != nil {
//		ctrl.logger.Errorf("failed to collect all resources for %v", restore.Name)
//		return restore, err
//	}
//	ctrl.logger.Infof("Restore metadata for %v started", restore.Name)
//	indexedResources := indexer.List()
//	unstructuredResources := make([]k8sresource.Resource, 0)
//
//	for _, indexedResource := range indexedResources {
//		unstructuredResource, ok := indexedResource.(k8sresource.Resource)
//		if !ok {
//			ctrl.logger.Warningf("Restore index cache has invalid object type. %v",
//				reflect.TypeOf(unstructuredResource))
//			continue
//		}
//
//		// ignore exempted resource for restore
//		if excludeRestoreResources.Has(unstructuredResource.GetObjectKind().GroupVersionKind().Kind) {
//			continue
//		}
//
//		unstructuredResources = append(unstructuredResources, unstructuredResource)
//	}
//
//	// prioritise the resources
//	unstructuredResources = ctrl.prioritiseResources(unstructuredResources)
//
//	for _, unstructuredResource := range unstructuredResources {
//		ctrl.logger.Infof("Processing %s/%s for restore", unstructuredResource.GroupVersionKind(),
//			unstructuredResource.GetName())
//		if err := ctrl.applyResource(unstructuredResource, restore); err != nil {
//			return restore, err
//		}
//	}
//
//	restore, err = ctrl.updateRestoreState(restore, kahuapi.RestoreStateCompleted)
//	if err != nil {
//		ctrl.logger.Errorf("failed to update metadata state: %s", err.Error())
//	}
//	restore.Status.State = kahuapi.RestoreStateProcessing
//	if restore, err = ctrl.updateRestoreStage(restore, kahuapi.RestoreStagePostHook); err != nil {
//		ctrl.logger.Errorf("failed to update post hook stage: %s", err.Error())
//	}
//	// Wait for all of the restore hook goroutines to be done, which is
//	// only possible once all of their errors have been received by the loop
//	// below, then close the hooksErrs channel so the loop terminates.
//	go func() {
//		ctrl.logger.Info("Waiting for all post-restore-exec hooks to complete")
//
//		ctrl.hooksWaitGroup.Wait()
//		close(ctrl.hooksErrs)
//	}()
//	var errs []string
//	for err := range ctrl.hooksErrs {
//		errs = append(errs, err.Error())
//	}
//	if len(errs) > 0 {
//		ctrl.logger.Errorf("Failure while executing post exec hooks: %+v", errs)
//		return restore, errors.New("failure while executing post exec hooks")
//	}
//
//	// handle external hooks sets
//	for hookIndex, hookSpec := range restoreHookSet.Hooks {
//		ctrl.logger.Infof("Processing external restore post hook (%s)", hookSpec.Name)
//		for nsIndex, namespace := range restoreHookSet.Namespaces[hookIndex].namespaces.UnsortedList() {
//			ctrl.logger.Infof("Processing namespace (%s) for hook %s", namespace, hookSpec.Name)
//			for _, pod := range restoreHookSet.Namespaces[hookIndex].Pods[nsIndex].pods.UnsortedList() {
//				ctrl.logger.Infof("Processing pod (%s) for hook %s", pod, hookSpec.Name)
//				if len(hookSpec.Hooks) == 0 {
//					continue
//				}
//				err := hooks.CommonExecuteHook(ctrl.logger, ctrl.kubeClient, ctrl.podCommandExecutor,
//					hookSpec, namespace, pod, hooks.PostHookPhase)
//				if err != nil {
//					ctrl.logger.Errorf("failed to execute hook on pod %s, err %s", pod, err.Error())
//					return restore, err
//				}
//			}
//		}
//	}
//	restore, err = ctrl.updateRestoreState(restore, kahuapi.RestoreStateCompleted)
//	if err != nil {
//		ctrl.logger.Errorf("failed to update posthook state: %s", err.Error())
//	}
//	ctrl.logger.Info("post hook stage finished")
//
//	return restore, nil
//}
//
//func canonicalName(resource k8sresource.Resource) string {
//	return resource.GetNamespace() + ":" +
//		resource.GetAPIVersion() + ":" +
//		resource.GetKind() + ":" +
//		resource.GetName()
//}
//
//func (ctrl *controller) applyResource(resource k8sresource.Resource, restore *kahuapi.Restore) error {
//	gvk := resource.GroupVersionKind()
//	gvr, _, err := ctrl.discoveryHelper.ByGroupVersionKind(gvk)
//	if err != nil {
//		ctrl.logger.Errorf("unable to fetch GroupVersionResource for %s", gvk)
//		return err
//	}
//
//	resourceClient := ctrl.dynamicClient.Resource(gvr).Namespace(resource.GetNamespace())
//
//	err = ctrl.preProcessResource(resource)
//	if err != nil {
//		ctrl.logger.Errorf("unable to preprocess resource %s. %s", resource.GetName(), err)
//		return err
//	}
//
//	existingResource, err := resourceClient.Get(context.TODO(), resource.GetName(), metav1.GetOptions{})
//	if err == nil && existingResource != nil {
//		// resource already exist. ignore creating
//		ctrl.logger.Warningf("ignoring %s.%s/%s restore. Resource already exist",
//			existingResource.GetKind(),
//			existingResource.GetAPIVersion(),
//			existingResource.GetName())
//		return nil
//	}
//
//	kind := resource.GetKind()
//
//	switch kind {
//	case utils.Deployment, utils.DaemonSet, utils.StatefulSet, utils.Replicaset:
//		err = ctrl.applyWorkloadResources(resource, restore, resourceClient)
//		if err != nil {
//			return err
//		}
//	case utils.Pod:
//		err = ctrl.applyPodResources(resource, restore, resourceClient)
//		if err != nil {
//			return err
//		}
//	default:
//		_, err = resourceClient.Create(context.TODO(), resource, metav1.CreateOptions{})
//		if err != nil && !apierrors.IsAlreadyExists(err) {
//			// ignore if already exist
//			return err
//		}
//		return nil
//	}
//
//	return nil
//}
//
//func (ctrl *controller) deleteRestoredResource(resource *kahuapi.RestoreResource, restore *kahuapi.Restore) error {
//	gvk := resource.GroupVersionKind()
//	gvr, _, err := ctrl.discoveryHelper.ByGroupVersionKind(gvk)
//	if err != nil {
//		ctrl.logger.Errorf("unable to fetch GroupVersionResource for %s", gvk)
//		return err
//	}
//
//	resourceClient := ctrl.dynamicClient.Resource(gvr).Namespace(resource.Namespace)
//	bNeedCleanup := ctrl.isCleanupNeeded(resourceClient, resource, restore.Name)
//	if bNeedCleanup != true {
//		return nil
//	}
//
//	err = resourceClient.Delete(context.TODO(), resource.ResourceName, metav1.DeleteOptions{})
//	if err != nil {
//		ctrl.logger.Errorf("Failed to delete resource %s, error: %s", resource.ResourceName, err)
//		return err
//	}
//
//	return nil
//}
//
func (ctrl *controller) preProcessResource(resource k8sresource.Resource) error {
	// ensure namespace existence
	if err := ctrl.ensureNamespace(resource.GetNamespace()); err != nil {
		return err
	}
	// remove resource version
	resource.SetResourceVersion("")

	return nil
}

//
//func (ctrl *controller) isCleanupNeeded(resourceClient dynamic.ResourceInterface,
//	resource *kahuapi.RestoreResource, restoreName string) bool {
//	existingResource, err := resourceClient.Get(context.TODO(), resource.ResourceName, metav1.GetOptions{})
//	if err != nil && !apierrors.IsNotFound(err) {
//		return false
//	} else if apierrors.IsNotFound(err) {
//		ctrl.logger.Infof("resource %s not found, skipping cleanup", resource.ResourceName)
//		return false
//	}
//
//	annots := existingResource.GetAnnotations()
//	if annots == nil || annots[annRestoreIdentifier] != restoreName {
//		ctrl.logger.Infof("restore identifier annotation not found for resource %s, skipping cleanup",
//			resource.ResourceName)
//		return false
//	}
//
//	if existingResource.GetKind() == "PersistentVolumeClaim" ||
//		existingResource.GetKind() == "PersistentVolume" {
//		return false
//	}
//
//	ctrl.logger.Infof("Resource %s, kind %s selected for cleanup", resource.ResourceName, existingResource.GetKind())
//	return true
//}
//
func (ctrl *controller) ensureNamespace(namespace string) error {
	// check if namespace exist
	if namespace == "" {
		// ignore if namespace is empty
		// possibly cluster scope resource
		return nil
	}

	// check if namespace exist
	n, err := ctrl.kubeClient.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
	if err == nil && n != nil {
		// namespace already exist
		return nil
	}

	// create namespace
	_, err = ctrl.kubeClient.CoreV1().
		Namespaces().
		Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}, metav1.CreateOptions{})
	if err != nil {
		ctrl.logger.Errorf("Unable to ensure namespace. %s", err)
		return err
	}

	return nil
}

//
//func (ctrl *controller) isMatchingReplicaSetExist(resource k8sresource.Resource, backedupResName string) bool {
//	var labelSelectors map[string]string
//	var replicaset *appsv1.ReplicaSet
//	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &replicaset)
//	if err != nil {
//		return false
//	}
//
//	rsObj := replicaset.DeepCopy()
//	if rsObj.Spec.Selector.MatchLabels != nil {
//		labelSelectors = rsObj.Spec.Selector.MatchLabels
//	}
//	if len(labelSelectors) == 0 {
//		return false
//	}
//
//	replicaSets, err := ctrl.kubeClient.AppsV1().ReplicaSets(resource.GetNamespace()).Get(context.TODO(),
//		backedupResName, metav1.GetOptions{})
//	if err != nil {
//		return false
//	}
//	if labels.Equals(labelSelectors, replicaSets.Spec.Selector.MatchLabels) {
//		return true
//	}
//	return false
//}
//
//func (ctrl *controller) isMatchingStatefulSetExist(resource k8sresource.Resource, backedupResName string) bool {
//	var labelSelectors map[string]string
//	var statefulset *appsv1.StatefulSet
//	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &statefulset)
//	if err != nil {
//		return false
//	}
//
//	statefulSetObj := statefulset.DeepCopy()
//	if statefulSetObj.Spec.Selector.MatchLabels != nil {
//		labelSelectors = statefulSetObj.Spec.Selector.MatchLabels
//	}
//	if len(labelSelectors) == 0 {
//		return false
//	}
//
//	statefulSets, err := ctrl.kubeClient.AppsV1().StatefulSets(resource.GetNamespace()).Get(context.TODO(),
//		backedupResName, metav1.GetOptions{})
//	if err != nil {
//		return false
//	}
//
//	if labels.Equals(labelSelectors, statefulSets.Spec.Selector.MatchLabels) {
//		return true
//	}
//	return false
//}
//
//func (ctrl *controller) isMatchingDaemonSetExist(resource k8sresource.Resource, backedupResName string) bool {
//	var labelSelectors map[string]string
//	var daemonset *appsv1.DaemonSet
//	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &daemonset)
//	if err != nil {
//		return false
//	}
//
//	daemonsetObj := daemonset.DeepCopy()
//	if daemonsetObj.Spec.Selector.MatchLabels != nil {
//		labelSelectors = daemonsetObj.Spec.Selector.MatchLabels
//	}
//	if len(labelSelectors) == 0 {
//		return false
//	}
//
//	daemonsets, err := ctrl.kubeClient.AppsV1().DaemonSets(resource.GetNamespace()).Get(context.TODO(),
//		backedupResName, metav1.GetOptions{})
//	if err != nil {
//		return false
//	}
//	if labels.Equals(labelSelectors, daemonsets.Spec.Selector.MatchLabels) {
//		return true
//	}
//	return false
//}
//
//func (ctrl *controller) isMatchingDeployExist(resource k8sresource.Resource, backedupResName string) bool {
//	var labelSelectors map[string]string
//	var deployment *appsv1.Deployment
//	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &deployment)
//	if err != nil {
//		return false
//	}
//
//	deploy := deployment.DeepCopy()
//	if deploy.Spec.Selector.MatchLabels != nil {
//		labelSelectors = deploy.Spec.Selector.MatchLabels
//	}
//	if len(labelSelectors) == 0 {
//		return false
//	}
//
//	deployments, err := ctrl.kubeClient.AppsV1().Deployments(resource.GetNamespace()).Get(context.TODO(),
//		backedupResName, metav1.GetOptions{})
//	if err != nil {
//		return false
//	}
//
//	if labels.Equals(labelSelectors, deployments.Spec.Selector.MatchLabels) {
//		return true
//	}
//	return false
//}
//
//func (ctrl *controller) isMatchingPodExist(resource k8sresource.Resource, backedupResName string) bool {
//	var labelSelectors map[string]string
//	var pod *corev1.Pod
//	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &pod)
//	if err != nil {
//		return false
//	}
//
//	podObj := pod.DeepCopy()
//	if podObj.Labels != nil {
//		labelSelectors = podObj.Labels
//	}
//	if len(labelSelectors) == 0 {
//		return false
//	}
//
//	pods, err := ctrl.kubeClient.CoreV1().Pods(resource.GetNamespace()).Get(context.TODO(), backedupResName,
//		metav1.GetOptions{})
//	if err != nil {
//		return false
//	}
//
//	if labels.Equals(labelSelectors, pods.Labels) {
//		return true
//	}
//	return false
//}
//
//func (ctrl *controller) checkWorkloadConflict(restore *kahuapi.Restore, resource k8sresource.Resource) bool {
//	resName := resource.GetName()
//	resPrefix := restore.Spec.ResourcePrefix
//
//	backedupResName := string(bytes.TrimPrefix([]byte(resName), []byte(resPrefix)))
//
//	switch resource.GetKind() {
//	case utils.Pod:
//		return ctrl.isMatchingPodExist(resource, backedupResName)
//	case utils.Deployment:
//		return ctrl.isMatchingDeployExist(resource, backedupResName)
//	case utils.DaemonSet:
//		return ctrl.isMatchingDaemonSetExist(resource, backedupResName)
//	case utils.StatefulSet:
//		return ctrl.isMatchingStatefulSetExist(resource, backedupResName)
//	case utils.Replicaset:
//		return ctrl.isMatchingReplicaSetExist(resource, backedupResName)
//	default:
//		return false
//	}
//}
//
//func (ctrl *controller) applyWorkloadResources(resource k8sresource.Resource,
//	restore *kahuapi.Restore,
//	resourceClient dynamic.ResourceInterface) error {
//	ctrl.logger.Infof("Start processing init hook for Pod resource (%s)", resource.GetName())
//	var resourceType string
//	var labelSelectors map[string]string
//	var replicas int
//
//	switch resource.GetKind() {
//	case utils.Deployment:
//		// get all pods for deployment
//		resourceType = deploymentsResources
//		var deployment *appsv1.Deployment
//		err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &deployment)
//		if err != nil {
//			return err
//		}
//
//		deploy := deployment.DeepCopy()
//		if deploy.Spec.Selector.MatchLabels != nil {
//			labelSelectors = deploy.Spec.Selector.MatchLabels
//		}
//		replicas = int(*deploy.Spec.Replicas)
//	case utils.DaemonSet:
//		// get all pods for daemonset
//		resourceType = daemonsetsResources
//		var daemonset *appsv1.DaemonSet
//		err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &daemonset)
//		if err != nil {
//			return err
//		}
//
//		deploy := daemonset.DeepCopy()
//		if deploy.Spec.Selector.MatchLabels != nil {
//			labelSelectors = deploy.Spec.Selector.MatchLabels
//		}
//		replicas = 1 // One daemon per node
//	case utils.StatefulSet:
//		// get all pods for statefulset
//		resourceType = statefulsetsResources
//		var statefulset *appsv1.StatefulSet
//		err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &statefulset)
//		if err != nil {
//			return err
//		}
//
//		deploy := statefulset.DeepCopy()
//		if deploy.Spec.Selector.MatchLabels != nil {
//			labelSelectors = deploy.Spec.Selector.MatchLabels
//		}
//		replicas = int(*deploy.Spec.Replicas)
//	case utils.Replicaset:
//		// get all pods for replicaset
//		resourceType = replicasetsResources
//		var replicaset *appsv1.ReplicaSet
//		err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &replicaset)
//		if err != nil {
//			return err
//		}
//
//		deploy := replicaset.DeepCopy()
//		if deploy.Spec.Selector.MatchLabels != nil {
//			labelSelectors = deploy.Spec.Selector.MatchLabels
//		}
//		replicas = int(*deploy.Spec.Replicas)
//	default:
//	}
//
//	ctrl.hooksWaitGroup.Add(1)
//	var wg sync.WaitGroup
//	wg.Add(1)
//
//	go func() {
//		defer wg.Done()
//
//		wh := &hooks.ResourceWaitHandler{
//			ResourceListWatchFactory: hooks.ResourceListWatchFactory{
//				ResourceGetter: ctrl.kubeClient.AppsV1().RESTClient(),
//			},
//		}
//		err := wh.WaitResource(ctrl.hooksContext, ctrl.logger, resource,
//			resource.GetName(), resource.GetNamespace(), resourceType)
//		if err != nil {
//			ctrl.logger.Errorf("error executing wait for %s-%s ", resourceType, resource.GetName())
//		}
//	}()
//
//	return ctrl.applyWorkloadPodResources(resource, restore, resourceClient, &wg, labelSelectors, replicas)
//}
//
//func (ctrl *controller) applyWorkloadPodResources(resource k8sresource.Resource,
//	restore *kahuapi.Restore,
//	resourceClient dynamic.ResourceInterface,
//	wg *sync.WaitGroup,
//	labelSelectors map[string]string,
//	replicas int) error {
//	// Done is called when all pods for the resource is scheduled for hooks
//	defer ctrl.hooksWaitGroup.Done()
//
//	_, err := resourceClient.Create(context.TODO(), resource, metav1.CreateOptions{})
//	if err != nil && !apierrors.IsAlreadyExists(err) {
//		// ignore if already exist
//		ctrl.logger.Infof("failed to create %s", resource.GetName())
//		return err
//	}
//
//	// wait for workload resource to ready
//	wg.Wait()
//
//	namespace := resource.GetNamespace()
//	time.Sleep(2 * time.Second)
//
//	pods, err := ctrl.kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
//		LabelSelector: labels.Set(labelSelectors).String(),
//	})
//	if err != nil {
//		ctrl.logger.Errorf("unable to list pod for resource %s-%s", namespace, resource.GetName())
//		return err
//	}
//
//	if len(pods.Items) < replicas {
//		time.Sleep(10 * time.Second)
//		pods, err = ctrl.kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
//			LabelSelector: labels.Set(labelSelectors).String(),
//		})
//		if err != nil {
//			ctrl.logger.Errorf("unable to list pod for resource %s-%s", namespace, resource.GetName())
//			return err
//		}
//	}
//
//	for _, pod := range pods.Items {
//		podMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pod)
//		if err != nil {
//			ctrl.logger.Errorf("failed to convert pod (%s) to unstructured, %+v", pod.Name, err.Error())
//			return err
//		}
//
//		podUns := &unstructured.Unstructured{Object: podMap}
//		// post exec hooks
//		ctrl.waitExec(restore.Spec.Hooks.Resources, podUns)
//	}
//	return nil
//}
//
//func (ctrl *controller) applyPodResources(resource k8sresource.Resource,
//	restore *kahuapi.Restore,
//	resourceClient dynamic.ResourceInterface) error {
//	ctrl.logger.Infof("Start processing init hook for Pod resource (%s)", resource.GetName())
//	hookHandler := hooks.InitHooksHandler{}
//	var initRes k8sresource.Resource
//
//	initRes, err := hookHandler.HandleInitHook(ctrl.logger, ctrl.kubeClient, hooks.Pods, resource, &restore.Spec.Hooks)
//	if err != nil {
//		ctrl.logger.Errorf("Failed to process init hook for Pod resource (%s)", resource.GetName())
//		return err
//	}
//	if initRes != nil {
//		resource = initRes
//	}
//
//	_, err = resourceClient.Create(context.TODO(), resource, metav1.CreateOptions{})
//	if err != nil && !apierrors.IsAlreadyExists(err) {
//		// ignore if already exist
//		return err
//	}
//
//	// post hook
//	ctrl.waitExec(restore.Spec.Hooks.Resources, resource)
//	return nil
//}
//
////// waitExec executes hooks in a restored pod's containers when they become ready.
////func (ctrl *controller) waitExec(resourceHooks []kahuapi.RestoreResourceHookSpec, createdObj k8sresource.Resource) {
////	ctrl.hooksWaitGroup.Add(1)
////	go func() {
////		// Done() will only be called after all errors have been successfully sent
////		// on the ctrl.resticErrs channel.
////		defer ctrl.hooksWaitGroup.Done()
////
////		pod := new(corev1.Pod)
////		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(createdObj.UnstructuredContent(), &pod); err != nil {
////			ctrl.logger.Errorf("error converting unstructured pod : %s", err)
////			ctrl.hooksErrs <- err
////			return
////		}
////		execHooksByContainer, err := hooks.GroupRestoreExecHooks(
////			resourceHooks,
////			ctrl.kubeClient,
////			pod,
////			ctrl.logger,
////		)
////		if err != nil {
////			ctrl.logger.Errorf("error getting exec hooks for pod %s/%s", pod.Namespace, pod.Name)
////			ctrl.hooksErrs <- err
////			return
////		}
////
////		if errs := ctrl.waitExecHookHandler.HandleHooks(ctrl.hooksContext, ctrl.logger, pod, execHooksByContainer); len(errs) > 0 {
////			ctrl.hooksCancelFunc()
////
////			for _, err := range errs {
////				// Errors are already logged in the HandleHooks method.
////				ctrl.hooksErrs <- err
////			}
////		}
////	}()
////}
