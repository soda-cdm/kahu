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

package framework

import (
	"context"
	"github.com/soda-cdm/kahu/framework/executor"
	"github.com/soda-cdm/kahu/utils/k8sresource"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	emptyPluginName = "empty.plugin.kahu.io"
)

type Plugin interface {
	For() (schema.GroupVersionKind, Stages)
	Name() string
	Execute(ctx context.Context,
		stage Stage,
		resource k8sresource.Resource) (updatedResource k8sresource.Resource,
		dep []k8sresource.Resource, err error)
}

type Interface interface {
	PluginRegistry() Registry
	Executors() executor.Interface
}

type Registry interface {
	Register(plugin Plugin) error
	GetPlugins(stage Stage, obj schema.GroupVersionKind) []Plugin
}

type Stage int16

// bitmap of stage
type Stages int16

const (
	Invalid Stage = 0
)
const (
	PreBackup Stage = 1 << iota
	PostBackup
	PreRestore
	PostRestore
)

func (s Stage) String() string {
	switch s {
	case PreBackup:
		return "PreBackup"
	case PostBackup:
		return "PostBackup"
	case PreRestore:
		return "PreRestore"
	case PostRestore:
		return "PostRestore"
	case Invalid:
		return "Invalid"
	}

	return "Invalid"
}

func (s Stages) Add(stage Stage) Stages {
	return s | Stages(stage)
}

func (s Stages) Remove(stage Stage) Stages {
	return s ^ Stages(stage)
}

func (s Stages) Exist(stage Stage) bool {
	return (s & Stages(stage)) == Stages(stage)
}

type emptyPlugin struct{}

var _ Plugin = &emptyPlugin{}

func (plugin *emptyPlugin) For() (schema.GroupVersionKind, Stages) {
	return schema.GroupVersionKind{
		Group:   "kahu.io",
		Version: "v1beta1",
		Kind:    "Custom",
	}, Stages(PreBackup)
}

func (plugin *emptyPlugin) Name() string {
	return emptyPluginName
}

func (plugin *emptyPlugin) Execute(_ context.Context, _ Stage,
	resource k8sresource.Resource) (updatedResource k8sresource.Resource,
	dep []k8sresource.Resource, err error) {
	return resource, nil, nil
}

func WithStages(stages ...Stage) Stages {
	var rStages Stages
	for _, stage := range stages {
		rStages = rStages.Add(stage)
	}

	return rStages
}
