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

// Package hooks implement hook execution in backup and restore scenario
package hooks

import (
	"context"
	"encoding/json"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
)

type CommonHookSpec struct {
	Name              string
	IncludeNamespaces []string
	ExcludeNamespaces []string
	IncludeResources  []kahuapi.ResourceSpec
	ExcludeResources  []kahuapi.ResourceSpec
	LabelSelector     *metav1.LabelSelector
	ContinueFlag      bool
	Hooks             []kahuapi.ResourceHook
}

func FilterHookNamespaces(allNamespaces sets.String, IncludeNamespaces, ExcludeNamespaces []string) sets.String {
	// Filter namespaces for hook
	hooksNsIncludes := sets.NewString()
	hooksNsExcludes := sets.NewString()
	hooksNsIncludes.Insert(IncludeNamespaces...)
	hooksNsExcludes.Insert(ExcludeNamespaces...)
	filteredHookNs := filterIncludesExcludes(allNamespaces,
		hooksNsIncludes.UnsortedList(),
		hooksNsExcludes.UnsortedList())
	return filteredHookNs
}

func filterIncludesExcludes(rawItems sets.String, includes, excludes []string) sets.String {
	// perform include/exclude item on cache
	excludeItems := sets.NewString()

	// process include item
	includeItems := sets.NewString(includes...)
	if len(includeItems) != 0 {
		for _, item := range rawItems.UnsortedList() {
			// if available item are not included exclude them
			if !includeItems.Has(item) {
				excludeItems.Insert(item)
			}
		}
		for _, item := range includeItems.UnsortedList() {
			if !rawItems.Has(item) {
				excludeItems.Insert(item)
			}
		}
	} else {
		// if not specified include all namespaces
		includeItems = rawItems
	}
	excludeItems.Insert(excludes...)

	for _, excludeItem := range excludeItems.UnsortedList() {
		includeItems.Delete(excludeItem)
	}
	return includeItems
}

func checkInclude(in []string, ex []string, key string) bool {
	setIn := sets.NewString().Insert(in...)
	setEx := sets.NewString().Insert(ex...)
	if setEx.Has(key) {
		return false
	}
	return len(in) == 0 || setIn.Has(key)
}

func validateHook(log log.FieldLogger,
	client kubernetes.Interface,
	hookSpec CommonHookSpec,
	name, namespace string,
	slabels labels.Set) bool {
	// Check namespace
	namespacesIn := hookSpec.IncludeNamespaces
	namespacesEx := hookSpec.ExcludeNamespaces

	if !checkInclude(namespacesIn, namespacesEx, namespace) {
		log.Infof("hook (%s), not for ns (%s) skipping execution", hookSpec.Name, namespace)
		return false
	}

	// Check resource
	pods, err := GetAllPodsForNamespace(log, client, namespace, hookSpec.LabelSelector,
		hookSpec.IncludeResources, hookSpec.ExcludeResources)
	if err != nil {
		log.Warningf("hook (%s), failed to get pods for ns (%s) skipping execution", hookSpec.Name, namespace)
		return false
	}

	if !pods.Has(name) {
		log.Infof("hook (%s), not for pod (%s) skipping execution", hookSpec.Name, name)
		return false
	}
	// Check label
	var selector labels.Selector
	if hookSpec.LabelSelector != nil {
		labelSelector, err := metav1.LabelSelectorAsSelector(hookSpec.LabelSelector)
		if err != nil {
			log.Infof("error getting label selector(%s), skipping hook execution", err.Error())
			return false
		}
		selector = labelSelector
	}
	if selector != nil && !selector.Matches(slabels) {
		log.Infof("invalid label for hook execution, skipping hook execution")
		return false
	}
	return true
}

func parseStringToCommand(commandValue string) []string {
	var command []string
	// check for json array
	if commandValue[0] == '[' {
		if err := json.Unmarshal([]byte(commandValue), &command); err != nil {
			command = []string{commandValue}
		}
	} else {
		command = append(command, commandValue)
	}
	return command
}

func GetPodsFromDeployment(logger log.FieldLogger, client kubernetes.Interface, namespace string,
	includeResources, excludeResources []kahuapi.ResourceSpec) ([]*v1.PodList, error) {
	// Get all deployments
	listOptions := metav1.ListOptions{}
	deployments, err := client.AppsV1().Deployments(namespace).List(context.TODO(), listOptions)
	if err != nil {
		logger.Errorf("unable to list deployment for namespace %s", namespace)
		return nil, err
	}

	// Filter deployments for resource spec
	var allResources []string
	for _, deployment := range deployments.Items {
		allResources = append(allResources, deployment.Name)
	}
	filteredDeployments := utils.FindMatchedStrings(utils.Deployment,
		allResources,
		includeResources,
		excludeResources)

	resourceSet := sets.NewString(filteredDeployments...)
	var allPodList []*v1.PodList
	for _, deployment := range deployments.Items {
		if resourceSet.Has(deployment.Name) {
			listOptions := metav1.ListOptions{LabelSelector: labels.Set(deployment.Spec.Selector.MatchLabels).String()}
			pods, err := client.CoreV1().Pods(namespace).List(context.TODO(), listOptions)
			if err != nil {
				logger.Errorf("unable to list pod for deployment %s", deployment)
				return allPodList, err
			}
			allPodList = append(allPodList, pods)
		}
	}
	return allPodList, nil
}

func GetPodsFromDaemonset(logger log.FieldLogger, client kubernetes.Interface, namespace string,
	includeResources, excludeResources []kahuapi.ResourceSpec) ([]*v1.PodList, error) {
	// Get all daemonsets
	listOptions := metav1.ListOptions{}
	daemonsets, err := client.AppsV1().DaemonSets(namespace).List(context.TODO(), listOptions)
	if err != nil {
		logger.Errorf("unable to list daemonset for namespace %s", namespace)
		return nil, err
	}

	// Filter daemonsets for ResourceSpec
	var allResources []string
	for _, daemonset := range daemonsets.Items {
		allResources = append(allResources, daemonset.Name)
	}
	filteredDeployments := utils.FindMatchedStrings(utils.DaemonSet,
		allResources,
		includeResources,
		excludeResources)

	resourceSet := sets.NewString(filteredDeployments...)
	var allPodList []*v1.PodList
	for _, daemonset := range daemonsets.Items {
		if resourceSet.Has(daemonset.Name) {
			listOptions := metav1.ListOptions{LabelSelector: labels.Set(daemonset.Spec.Selector.MatchLabels).String()}
			pods, err := client.CoreV1().Pods(namespace).List(context.TODO(), listOptions)
			if err != nil {
				logger.Errorf("unable to list pod for daemonset %s", daemonset)
				return allPodList, err
			}
			allPodList = append(allPodList, pods)
		}
	}
	return allPodList, nil
}

func GetPodsFromStatefulset(logger log.FieldLogger, client kubernetes.Interface, namespace string,
	includeResources, excludeResources []kahuapi.ResourceSpec) ([]*v1.PodList, error) {
	// Get all statefulsets
	listOptions := metav1.ListOptions{}
	statefulsets, err := client.AppsV1().StatefulSets(namespace).List(context.TODO(), listOptions)
	if err != nil {
		logger.Errorf("unable to list statefulset for namespace %s", namespace)
		return nil, err
	}

	// Filter statefulsets for ResourceSpec
	var allResources []string
	for _, statefulset := range statefulsets.Items {
		allResources = append(allResources, statefulset.Name)
	}
	filteredDeployments := utils.FindMatchedStrings(utils.StatefulSet,
		allResources,
		includeResources,
		excludeResources)

	resourceSet := sets.NewString(filteredDeployments...)
	var allPodList []*v1.PodList
	for _, statefulset := range statefulsets.Items {
		if resourceSet.Has(statefulset.Name) {
			listOptions := metav1.ListOptions{LabelSelector: labels.Set(statefulset.Spec.Selector.MatchLabels).String()}
			pods, err := client.CoreV1().Pods(namespace).List(context.TODO(), listOptions)
			if err != nil {
				logger.Errorf("unable to list pod for statefulset %s", statefulset)
				return allPodList, err
			}
			allPodList = append(allPodList, pods)
		}
	}
	return allPodList, nil
}

func GetPodsFromReplicaset(logger log.FieldLogger, client kubernetes.Interface, namespace string,
	includeResources, excludeResources []kahuapi.ResourceSpec) ([]*v1.PodList, error) {
	// Get all replicasets
	listOptions := metav1.ListOptions{}
	replicasets, err := client.AppsV1().ReplicaSets(namespace).List(context.TODO(), listOptions)
	if err != nil {
		logger.Errorf("unable to list replicaset for namespace %s", namespace)
		return nil, err
	}

	// Filter replicasets for ResourceSpec
	var allResources []string
	for _, replicaset := range replicasets.Items {
		allResources = append(allResources, replicaset.Name)
	}
	filteredDeployments := utils.FindMatchedStrings(utils.Replicaset,
		allResources,
		includeResources,
		excludeResources)

	resourceSet := sets.NewString(filteredDeployments...)
	var allPodList []*v1.PodList
	for _, replicaset := range replicasets.Items {
		if resourceSet.Has(replicaset.Name) {
			listOptions := metav1.ListOptions{LabelSelector: labels.Set(replicaset.Spec.Selector.MatchLabels).String()}
			pods, err := client.CoreV1().Pods(namespace).List(context.TODO(), listOptions)
			if err != nil {
				logger.Errorf("unable to list pod for replicaset %s", replicaset)
				return allPodList, err
			}
			allPodList = append(allPodList, pods)
		}
	}
	return allPodList, nil
}
