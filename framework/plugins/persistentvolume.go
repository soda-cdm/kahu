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

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/framework"
	"github.com/soda-cdm/kahu/utils/k8sresource"
)

type PersistentVolumePlugin struct {
	kubeCli kubernetes.Interface
}

func newPersistentVolumePlugin(kubeCli kubernetes.Interface) framework.Plugin {
	return &PersistentVolumePlugin{
		kubeCli: kubeCli,
	}
}

func (_ *PersistentVolumePlugin) For() (schema.GroupVersionKind, framework.Stages) {
	return k8sresource.PersistentVolumeGVK, framework.WithStages(framework.PreBackup)
}

func (_ *PersistentVolumePlugin) Name() string {
	return k8sresource.PersistentVolumeGVK.String()
}

// get all dependencies of pv
func (_ *PersistentVolumePlugin) Execute(ctx context.Context,
	stage framework.Stage,
	resource k8sresource.Resource) (updatedResource k8sresource.Resource,
	dep []k8sresource.Resource, err error) {

	return resource, nil, nil
}
