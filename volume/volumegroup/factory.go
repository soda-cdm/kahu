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

package volumegroup

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/utils"
)

type Factory interface {
	// ByPVs([]*corev1.PersistentVolume) (Interface, error)
	ByVolumeGroup(group *kahuapi.VolumeGroup) (Interface, error)
	ByPVCs(backupName string, pvcs []*corev1.PersistentVolumeClaim) (Interface, error)
}

type factory struct {
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
		kubeClient: kubeClient,
		kahuClient: kahuClient,
	}, nil
}

func (f *factory) volGroupSpecByPVCs(backupName string,
	pvcs []*corev1.PersistentVolumeClaim) *kahuapi.VolumeGroupSpec {
	// currently not support VolumeGroup creation on kubernetes
	volObjRef := make([]kahuapi.ResourceReference, 0)
	for _, pvc := range pvcs {
		volObjRef = append(volObjRef, kahuapi.ResourceReference{
			Kind:      utils.PVC,
			Namespace: pvc.Namespace,
			Name:      pvc.Name,
		})
	}

	return &kahuapi.VolumeGroupSpec{
		Backup: &kahuapi.ResourceReference{
			Name: backupName,
		},
		VolumeSource: kahuapi.VolumeSource{
			List: volObjRef,
		},
	}
}

func (f *factory) ByVolumeGroup(group *kahuapi.VolumeGroup) (Interface, error) {
	return newGroup(f.kubeClient, f.kahuClient, &group.Spec)
}

func (f *factory) ByPVCs(backupName string, pvcs []*corev1.PersistentVolumeClaim) (Interface, error) {
	return newGroup(f.kubeClient, f.kahuClient, f.volGroupSpecByPVCs(backupName, pvcs))
}
