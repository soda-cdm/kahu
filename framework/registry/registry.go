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

package registry

import (
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/soda-cdm/kahu/framework"
)

type registration struct {
	plugin framework.Plugin
	stages framework.Stages
}

func (reg *registration) String() string {
	return reg.plugin.Name()
}

type registrations map[string]registration

func (regs registrations) Insert(stages framework.Stages, plugin framework.Plugin) error {
	pluginName := plugin.Name()

	_, ok := regs[pluginName]
	if ok {
		return fmt.Errorf("duplication plugin[%s] registration", pluginName)
	}

	regs[pluginName] = registration{
		plugin: plugin,
		stages: stages,
	}
	return nil
}

type registry struct {
	sync.RWMutex
	logger  *log.Entry
	plugins map[schema.GroupVersionKind]registrations
}

func NewRegistry() framework.Registry {
	return &registry{
		logger:  log.WithField("module", "plugin-registry"),
		plugins: make(map[schema.GroupVersionKind]registrations, 0),
	}
}

func (store *registry) Register(plugin framework.Plugin) error {
	store.Lock()
	defer store.Unlock()

	obj, stages := plugin.For()
	store.logger.Infof("Register plugin[%s] for %s ", plugin.Name(), obj.String())
	// register plugin
	regPlugins, ok := store.plugins[obj]
	if !ok {
		regPlugins = make(registrations)
	}

	err := regPlugins.Insert(stages, plugin)
	if err != nil {
		store.logger.Errorf("Failed to register plugin[%s]", plugin.Name())
		return err
	}
	store.plugins[obj] = regPlugins
	return nil
}

func (store *registry) GetPlugins(stage framework.Stage, obj schema.GroupVersionKind) []framework.Plugin {
	store.RLock()
	defer store.RUnlock()

	plugins := make([]framework.Plugin, 0)
	regPlugins, ok := store.plugins[obj]
	if !ok {
		return plugins
	}

	for _, regPlugin := range regPlugins {
		if regPlugin.stages.Exist(stage) {
			plugins = append(plugins, regPlugin.plugin)
		}
	}

	return plugins
}
