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

package snapshot

import (
	"context"
	"github.com/soda-cdm/kahu/client"
	clientset "github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/volume/volumegroup"
	"k8s.io/client-go/kubernetes"
)

type Factory interface {
	ByVolumeGroup(volGroup volumegroup.Interface) (Interface, error)
}

type factory struct {
	ctx        context.Context
	kubeClient kubernetes.Interface
	kahuClient clientset.Interface
}

func NewFactory(ctx context.Context, clientFactory client.Factory) (Factory, error) {
	kubeClient, err := clientFactory.KubeClient()
	if err != nil {
		return nil, err
	}

	kahuClient, err := clientFactory.KahuClient()
	if err != nil {
		return nil, err
	}

	return &factory{
		ctx:        ctx,
		kubeClient: kubeClient,
		kahuClient: kahuClient,
	}, nil
}

func (f *factory) ByVolumeGroup(volGroup volumegroup.Interface) (Interface, error) {
	return newSnapshot(f.ctx, f.kubeClient, f.kahuClient, volGroup)
}
