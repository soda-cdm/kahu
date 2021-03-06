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
	"github.com/spf13/pflag"

	"github.com/soda-cdm/kahu/client"
)

type kahuClientOptions struct {
	*client.Config
}

func NewKahuClientOptions() *kahuClientOptions {
	return &kahuClientOptions{
		Config: client.NewFactoryConfig(),
	}
}

func (opt *kahuClientOptions) AddFlags(fs *pflag.FlagSet) {
	opt.Config.AddFlags(fs)
}

func (opt *kahuClientOptions) ApplyTo(cfg *client.Config) error {
	cfg.KubeConfig = opt.KubeConfig
	cfg.ClientQPS = opt.ClientQPS
	cfg.ClientBurst = opt.ClientBurst
	return nil
}

func (opt *kahuClientOptions) Validate() []error {
	if err := opt.Config.Validate(); err != nil {
		return []error{err}
	}

	return nil
}
