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
	"strings"

	"github.com/spf13/pflag"

	"github.com/soda-cdm/kahu/controllers/app/config"
	"github.com/soda-cdm/kahu/utils/logoptions"
)

type optionsManager struct {
	manager          *controllerManagerOptions
	log              *logoptions.LogOptions
	kahuClient       *kahuClientOptions
	backupController *BackupControllerFlags
}

func NewOptionsManager() (*optionsManager, error) {
	return &optionsManager{
		manager:          NewGenericControllerOptions(),
		log:              logoptions.NewLogOptions(),
		kahuClient:       NewKahuClientOptions(),
		backupController: NewBackupControllerFlags(),
	}, nil
}

func (opt *optionsManager) AddFlags(fs *pflag.FlagSet) {
	opt.manager.AddFlags(fs)
	opt.log.AddFlags(fs)
	opt.kahuClient.AddFlags(fs)
	opt.backupController.AddFlags(fs)
}

func (opt *optionsManager) ApplyTo(cfg *config.Config) error {
	if err := opt.log.Apply(); err != nil {
		return err
	}
	if err := opt.manager.ApplyTo(cfg); err != nil {
		return err
	}
	if err := opt.kahuClient.ApplyTo(&cfg.KahuClientConfig); err != nil {
		return err
	}
	if err := opt.backupController.ApplyTo(&cfg.BackupControllerConfig); err != nil {
		return err
	}
	return nil
}

func (opt *optionsManager) Validate() error {
	errs := make([]error, 0)

	// validate all controllers options
	errs = append(errs, opt.manager.Validate()...)
	errs = append(errs, opt.kahuClient.Validate()...)

	if len(errs) == 0 {
		return nil
	}
	// construct error msg
	errMsg := make([]string, 0)
	for _, err := range errs {
		errMsg = append(errMsg, err.Error())
	}
	return fmt.Errorf(strings.Join(errMsg, ","))
}

func (opt *optionsManager) Config() (*config.Config, error) {
	// validate options
	if err := opt.Validate(); err != nil {
		return nil, err
	}

	cfg := new(config.Config)
	// apply all options to configuration
	if err := opt.ApplyTo(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
