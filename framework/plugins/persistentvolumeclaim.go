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

package plugins

import (
	"context"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

type PersistentVolumeClaimPlugin struct {
	logger  *log.Entry
	kubeCli kubernetes.Interface
}

func newPersistentVolumeClaimPlugin(kubeCli kubernetes.Interface) framework.Plugin {
	return &PersistentVolumeClaimPlugin{
		logger:  log.WithField("module", "PersistentVolumeClaimPlugin"),
		kubeCli: kubeCli,
	}
}

func (_ *PersistentVolumeClaimPlugin) For() (schema.GroupVersionKind, framework.Stages) {
	return k8sresource.PersistentVolumeClaimGVK, framework.WithStages(framework.PreBackup)
}

func (_ *PersistentVolumeClaimPlugin) Name() string {
	return k8sresource.PersistentVolumeClaimGVK.String()
}

// get all dependencies of pvc
func (plugin *PersistentVolumeClaimPlugin) Execute(ctx context.Context,
	stage framework.Stage,
	resource k8sresource.Resource) (k8sresource.Resource, []k8sresource.Resource, error) {
	var pvc corev1.PersistentVolumeClaim
	err := k8sresource.FromResource(resource, &pvc)
	if err != nil {
		return resource, nil, err
	}
	if pvc.Spec.VolumeName == "" {
		// ignore unbounded PVC
		return resource, nil, nil
	}

	pv, err := plugin.getPV(ctx, &pvc)
	if err != nil {
		return resource, nil, nil
	}

	return resource, []k8sresource.Resource{pv}, nil
}

func (plugin *PersistentVolumeClaimPlugin) getPV(ctx context.Context,
	pvc *corev1.PersistentVolumeClaim) (k8sresource.Resource, error) {
	plugin.logger.Infof("Fetching PersistentVolume for pvc[%s/%s]", pvc.Namespace, pvc.Name)

	pv, err := plugin.kubeCli.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		plugin.logger.Errorf("Failed to fetch PersistentVolume for claim[%s/%s]. %s", pvc.Namespace, pvc.Name, err)
		return k8sresource.Resource{}, err
	}

	k8sResource, err := k8sresource.ToResource(&pv)
	if err != nil {
		plugin.logger.Errorf("Failed to translate PersistentVolume into k8s resources. %s", err)
		return k8sresource.Resource{}, err
	}

	k8sResource.SetAPIVersion(schema.GroupVersion{
		Group:   k8sresource.PersistentVolumeGVK.Group,
		Version: k8sresource.PersistentVolumeGVK.Version,
	}.String())
	k8sResource.SetKind(k8sresource.PersistentVolumeGVK.Kind)
	return k8sResource, nil
}
