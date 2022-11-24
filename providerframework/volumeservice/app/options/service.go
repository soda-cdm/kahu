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
	"os"
	"time"

	"github.com/spf13/pflag"

	"github.com/soda-cdm/kahu/providerframework/volumeservice/app/config"
)

const (
	defaultControllerWorkers          = 4
	defaultLeaderElection             = false
	defaultVolumeBackupDriverEndpoint = "/tmp/volumeservice.sock"
	envLeaderLockNamespace            = "NAMESPACE"
	leaseDuration                     = 8 * time.Second
	renewDeadline                     = 6 * time.Second
	retryPeriod                       = 2 * time.Second
)

type serviceOptions struct {
	ControllerWorkers    int
	EnableLeaderElection bool
	DriverEndpoint       string
	DisableControllers   []string
	LeaderLockNamespace  string
	LeaderLeaseDuration  time.Duration
	LeaderRenewDeadline  time.Duration
	LeaderRetryPeriod    time.Duration
}

func NewServiceOptions() *serviceOptions {
	return &serviceOptions{
		ControllerWorkers:    defaultControllerWorkers,
		EnableLeaderElection: defaultLeaderElection,
		DriverEndpoint:       defaultVolumeBackupDriverEndpoint,
		DisableControllers:   make([]string, 0),
	}
}

func (opt *serviceOptions) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&opt.ControllerWorkers, "controllerWorkers", opt.ControllerWorkers,
		"Number of worker for each controller")
	fs.BoolVar(&opt.EnableLeaderElection, "enableLeaderElection", opt.EnableLeaderElection,
		"Start a leader election client and gain leadership for controller-manager")
	fs.StringVar(&opt.DriverEndpoint, "driverEndpoint", opt.DriverEndpoint,
		"Volume backup driver endpoint")
	fs.StringArrayVar(&opt.DisableControllers, "disableControllers", opt.DisableControllers,
		"Disable list of controller")
	fs.StringVar(&opt.LeaderLockNamespace, "leaderLockNamespace", os.Getenv(envLeaderLockNamespace),
		"Configure leader election lock namespace")
	fs.DurationVar(&opt.LeaderLeaseDuration, "leaderLeaseDuration", leaseDuration,
		"Configure leader election lease duration")
	fs.DurationVar(&opt.LeaderRenewDeadline, "leaderRenewDeadline", renewDeadline,
		"Configure leader election lease renew deadline")
	fs.DurationVar(&opt.LeaderRetryPeriod, "leaderRetryPeriod", retryPeriod,
		"Configure leader election lease retry period")
}

func (opt *serviceOptions) ApplyTo(cfg *config.Config) error {
	cfg.ControllerWorkers = opt.ControllerWorkers
	cfg.EnableLeaderElection = opt.EnableLeaderElection
	cfg.DriverEndpoint = opt.DriverEndpoint
	cfg.LeaderLockNamespace = opt.LeaderLockNamespace
	cfg.LeaderLeaseDuration = opt.LeaderLeaseDuration
	cfg.LeaderRenewDeadline = opt.LeaderRenewDeadline
	cfg.LeaderRetryPeriod = opt.LeaderRetryPeriod
	return nil
}

func (opt *serviceOptions) Validate() []error {
	errs := make([]error, 0)

	if opt.ControllerWorkers < 1 {
		errs = append(errs, fmt.Errorf("invalid controller worker count [%d]", opt.ControllerWorkers))
	}

	// TODO(Amit Roushan): Add validation for list of controllers
	return errs
}
