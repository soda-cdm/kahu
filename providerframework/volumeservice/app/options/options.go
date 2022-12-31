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

	"github.com/soda-cdm/kahu/providerframework/volumeservice/app/config"
	"github.com/soda-cdm/kahu/utils/logoptions"
)

type OptionManager interface {
	AddFlags(fs *pflag.FlagSet)
	Config() (*config.Config, error)
}

type optionsManager struct {
	service    *serviceOptions
	log        *logoptions.LogOptions
	kahuClient *kahuClientOptions
}

func NewOptionsManager() (OptionManager, error) {
	return &optionsManager{
		service:    NewServiceOptions(),
		log:        logoptions.NewLogOptions(),
		kahuClient: NewKahuClientOptions(),
	}, nil
}

func (opt *optionsManager) AddFlags(fs *pflag.FlagSet) {
	opt.service.AddFlags(fs)
	opt.log.AddFlags(fs)
	opt.kahuClient.AddFlags(fs)
}

func (opt *optionsManager) applyTo(cfg *config.Config) error {
	if err := opt.log.Apply(); err != nil {
		return err
	}
	if err := opt.service.ApplyTo(cfg); err != nil {
		return err
	}
	if err := opt.kahuClient.ApplyTo(&cfg.KahuClientConfig); err != nil {
		return err
	}
	return nil
}

func (opt *optionsManager) validate() error {
	errs := make([]error, 0)

	// validate all controllers options
	errs = append(errs, opt.service.Validate()...)
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
	if err := opt.validate(); err != nil {
		return nil, err
	}

	cfg := new(config.Config)
	// apply all options to configuration
	if err := opt.applyTo(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
