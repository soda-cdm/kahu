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
	"github.com/soda-cdm/kahu/volume/volumegroup"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Interface interface {
	Apply() error
	Delete() error
}

type restore struct {
	kubeClient    kubernetes.Interface
	dynamicClient dynamic.Interface
	volumeGroup   volumegroup.Interface
}

func newRestore(kubeClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	volumeGroup volumegroup.Interface) (Interface, error) {
	return &restore{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
		volumeGroup:   volumeGroup,
	}, nil
}

func (*restore) Apply() error {
	return nil
}
func (*restore) Delete() error {
	return nil
}
