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

package backup

import (
	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/utils"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/client"
	"github.com/soda-cdm/kahu/volume/snapshot"
	"github.com/soda-cdm/kahu/volume/volumegroup"
)

type Factory interface {
	BySnapshot(snapshot.Interface) (Interface, error)
	ByVolumes(vg volumegroup.Interface) (Interface, error)
}

type factory struct {
	kubeClient    kubernetes.Interface
	dynamicClient dynamic.Interface
	kahuClient    clientset.Interface
}

func NewFactory(clientFactory client.Factory) (Factory, error) {
	kubeClient, err := clientFactory.KubeClient()
	if err != nil {
		return nil, err
	}

	dynamicClient, err := clientFactory.DynamicClient()
	if err != nil {
		return nil, err
	}

	kahuClient, err := clientFactory.KahuClient()
	if err != nil {
		return nil, err
	}

	return &factory{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
		kahuClient:    kahuClient,
	}, nil
}

func (f *factory) BySnapshot(s snapshot.Interface) (Interface, error) {
	kahuVolSnapshots, err := s.GetSnapshots()
	if err != nil {
		return nil, err
	}

	snapshotRefs := make([]kahuapi.ResourceReference, 0)
	for _, kahuVolSnapshot := range kahuVolSnapshots {
		snapshotRefs = append(snapshotRefs, kahuapi.ResourceReference{
			Kind:      utils.KahuSnapshot,
			Name:      kahuVolSnapshot.Name,
			Namespace: kahuVolSnapshot.Namespace,
		})
	}

	return newBackup(f.kubeClient, f.kahuClient, f.dynamicClient, snapshotRefs...)
}

func (f *factory) ByVolumes(vg volumegroup.Interface) (Interface, error) {
	volumes, _ := vg.GetVolumes()

	// TODO: group the volume based on provisioner
	volRefs := make([]kahuapi.ResourceReference, 0)
	for _, vol := range volumes {
		volRefs = append(volRefs, kahuapi.ResourceReference{
			Kind:      utils.PVC,
			Name:      vol.Name,
			Namespace: vol.Namespace,
		})
	}
	return newBackup(f.kubeClient, f.kahuClient, f.dynamicClient, volRefs...)
}
