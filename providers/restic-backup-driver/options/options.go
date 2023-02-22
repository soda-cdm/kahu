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

// Package options defines Restic Provider flags
package options

import (
	"github.com/spf13/pflag"

	"github.com/soda-cdm/kahu/client"
	logOptions "github.com/soda-cdm/kahu/utils/logoptions"
)

// ResticProviderFlags defines flag options
type ResticProviderFlags struct {
	ResticFlags      *resticServiceFlags
	KubeClientConfig *client.Config
	LoggingFlags     *logOptions.LogOptions
	// TODO: Add configuration file options here
}

func NewOptions() *ResticProviderFlags {
	return &ResticProviderFlags{
		ResticFlags:      newResticServiceFlags(),
		KubeClientConfig: client.NewFactoryConfig(),
		LoggingFlags:     logOptions.NewLogOptions(),
	}
}

// AddFlags exposes available command line options
func (flags *ResticProviderFlags) AddFlags(fs *pflag.FlagSet) {
	// add restic driver flags
	flags.ResticFlags.addFlags(fs)
	// add kube client flags
	flags.KubeClientConfig.AddFlags(fs)
	// add logging flags
	flags.LoggingFlags.AddFlags(fs)
}

// Apply validate flags and apply
func (flags *ResticProviderFlags) ValidateAndApply() error {
	err := flags.ResticFlags.apply()
	if err != nil {
		return err
	}

	err = flags.LoggingFlags.Apply()
	if err != nil {
		return err
	}

	err = flags.KubeClientConfig.Validate()
	if err != nil {
		return err
	}

	return nil
}
