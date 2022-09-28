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
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
)

const (
	LabelBackupName = "kahu.backup.vol.group"
)

type Interface interface {
	GetVolumes() (vols []kahuapi.ResourceReference, readyToUse bool)
	GetBackupName() string
	GetGroupName() string
}

type group struct {
	kubeClient kubernetes.Interface
	kahuClient clientset.Interface
	volGroup   *kahuapi.VolumeGroup
}

func newGroup(kubeClient kubernetes.Interface,
	kahuClient clientset.Interface,
	spec *kahuapi.VolumeGroupSpec) (Interface, error) {

	volGroupReq := &kahuapi.VolumeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: volGroupName(spec),
			Labels: map[string]string{
				LabelBackupName: spec.Backup.Name,
			},
		},
		Spec: *spec,
	}

	volGroupRes, err := kahuClient.
		KahuV1beta1().
		VolumeGroups().
		Create(context.TODO(), volGroupReq, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return &group{
		kubeClient: kubeClient,
		kahuClient: kahuClient,
		volGroup:   volGroupRes,
	}, nil
}

func volGroupName(spec *kahuapi.VolumeGroupSpec) string {
	return fmt.Sprintf("%s-%s", spec.Backup.Name, uuid.NewUUID())
}

func (g *group) GetVolumes() ([]kahuapi.ResourceReference, bool) {
	// always return true as volume selector not supported yet
	return g.volGroup.Spec.List, true
}

func (g *group) GetBackupName() string {
	return g.volGroup.Spec.Backup.Name
}

func (g *group) GetGroupName() string {
	return g.volGroup.Name
}
