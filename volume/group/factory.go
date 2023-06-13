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

package group

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/utils"
	"github.com/soda-cdm/kahu/utils/k8sresource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/client"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
)

const (
	GroupNotProcessed = "GroupNotProcessed"
)

type Factory interface {
	ByPV([]k8sresource.Resource, ...groupFunc) ([]Interface, error)
	BySnapshot(snapshots []*kahuapi.VolumeSnapshot, groupings ...groupFunc) ([]Interface, error)
}

type factory struct {
	logger     *log.Entry
	kubeClient kubernetes.Interface
	kahuClient clientset.Interface
}

func NewFactory(clientFactory client.Factory) (Factory, error) {
	kubeClient, err := clientFactory.KubeClient()
	if err != nil {
		return nil, err
	}
	kahuClient, err := clientFactory.KahuClient()
	if err != nil {
		return nil, err
	}

	return &factory{
		logger:     log.WithField("module", "volume-group"),
		kubeClient: kubeClient,
		kahuClient: kahuClient,
	}, nil
}

type groupFunc func(pvs []k8sresource.Resource) []Interface

func WithProvider() groupFunc {
	return withProvider
}

func withProvider(resources []k8sresource.Resource) []Interface {
	groupByProvisioner := make(map[string][]k8sresource.Resource)

	// group volumes by provisioner
	for _, resource := range resources {
		provisioner, err := getProvisioner(resource)
		if err != nil {
			log.Warningf("unable to group. %s", err)
		}

		groupPVs, ok := groupByProvisioner[provisioner]
		if !ok {
			groupPVs = make([]k8sresource.Resource, 0)
		}
		groupPVs = append(groupPVs, resource)
		groupByProvisioner[provisioner] = groupPVs
	}

	// construct group
	groups := make([]Interface, 0)
	for provisioner, vols := range groupByProvisioner {
		groups = append(groups, newGroup(provisioner, vols))
	}

	return groups
}

func getProvisioner(resource k8sresource.Resource) (string, error) {
	switch resource.GetKind() {
	case k8sresource.PersistentVolumeGVK.Kind:
		pv := new(corev1.PersistentVolume)
		err := k8sresource.FromResource(resource, pv)
		if err != nil {
			return "", fmt.Errorf("unable to translate resource[%s] to PersistentVolume",
				resource.GetName())
		}
		return utils.VolumeProvisioner(pv), nil
	case k8sresource.KahuVolumeSnapshotGVK.Kind:
		snapshot := new(kahuapi.VolumeSnapshot)
		err := k8sresource.FromResource(resource, snapshot)
		if err != nil {
			return "", fmt.Errorf("unable to translate resource[%s] to kahu VolumeSnapshot",
				resource.GetName())
		}
		return *snapshot.Spec.SnapshotProvider, nil
	default:
		log.Warningf("Invalid kind[%s] for volume grouping", resource.GetKind())
		return "", fmt.Errorf("invalid kind[%s] for volume grouping", resource.GetKind())
	}
}

func (f *factory) ByPV(resources []k8sresource.Resource, groupings ...groupFunc) ([]Interface, error) {
	return f.group(resources, groupings...)
}

func (f *factory) BySnapshot(snapshots []*kahuapi.VolumeSnapshot, groupings ...groupFunc) ([]Interface, error) {
	resources := make([]k8sresource.Resource, 0)
	for _, snapshot := range snapshots {
		resource, err := k8sresource.ToResource(snapshot)
		if err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return f.group(resources, groupings...)
}

func (f *factory) group(resources []k8sresource.Resource, groupings ...groupFunc) ([]Interface, error) {
	groups := []Interface{
		newGroup(GroupNotProcessed, resources),
	}
	for _, grouping := range groupings {
		newGroup := make([]Interface, 0)
		for _, group := range groups {
			newGroup = append(newGroup, grouping(group.GetResources())...)
		}
		groups = newGroup
	}

	return groups, nil
}

func (f *factory) getPV(pvc *corev1.PersistentVolumeClaim) (*corev1.PersistentVolume, error) {
	f.logger.Infof("Fetching PersistentVolume for pvc[%s/%s]", pvc.Namespace, pvc.Name)

	pv, err := f.kubeClient.CoreV1().PersistentVolumes().Get(context.TODO(), pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		f.logger.Errorf("Failed to fetch PersistentVolume for claim[%s/%s]. %s", pvc.Namespace, pvc.Name, err)
		return nil, err
	}

	return pv, nil
}
