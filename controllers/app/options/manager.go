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

package options

import (
	"fmt"
	"github.com/soda-cdm/kahu/controllers/app/config"

	"github.com/spf13/pflag"
)

const (
	defaultControllerWorkers = 4
	defaultLeaderElection    = false
)

type controllerManagerOptions struct {
	ControllerWorkers    int
	EnableLeaderElection bool
	DisableControllers   []string
}

func NewGenericControllerOptions() *controllerManagerOptions {
	return &controllerManagerOptions{
		ControllerWorkers:    defaultControllerWorkers,
		EnableLeaderElection: defaultLeaderElection,
		DisableControllers:   make([]string, 0),
	}
}

func (opt *controllerManagerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&opt.ControllerWorkers, "controllerWorkers", opt.ControllerWorkers,
		"Number of worker for each controller")
	fs.BoolVar(&opt.EnableLeaderElection, "enableLeaderElection", opt.EnableLeaderElection,
		"Start a leader election client and gain leadership for controller-manager")
	fs.StringArrayVar(&opt.DisableControllers, "disableControllers", opt.DisableControllers,
		"Disable list of controller")
}

func (opt *controllerManagerOptions) ApplyTo(cfg *config.Config) error {
	cfg.ControllerWorkers = opt.ControllerWorkers
	cfg.EnableLeaderElection = opt.EnableLeaderElection
	cfg.DisableControllers = opt.DisableControllers
	return nil
}

func (opt *controllerManagerOptions) Validate() []error {
	errs := make([]error, 0)

	if opt.ControllerWorkers < 1 {
		errs = append(errs, fmt.Errorf("invalid controller worker count [%d]", opt.ControllerWorkers))
	}

	// TODO(Amit Roushan): Add validation for list of controllers
	return errs
}
