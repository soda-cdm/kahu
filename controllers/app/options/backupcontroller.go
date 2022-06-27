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
	"net"

	"github.com/spf13/pflag"

	"github.com/soda-cdm/kahu/controllers/backup"
)

const (
	defaultMetaServiceAddress = "127.0.0.1"
	defaultMetaServicePort    = 8181
)

type BackupControllerFlags struct {
	*backup.Config
}

func NewBackupControllerFlags() *BackupControllerFlags {
	return &BackupControllerFlags{
		&backup.Config{
			MetaServicePort:    defaultMetaServicePort,
			MetaServiceAddress: defaultMetaServiceAddress,
		},
	}
}

// AddFlags exposes available command line options
func (options *BackupControllerFlags) AddFlags(fs *pflag.FlagSet) {
	fs.UintVarP(&options.MetaServicePort, "meta-port", "P", options.MetaServicePort,
		"Meta service port")
	fs.StringVarP(&options.MetaServiceAddress, "meta-address", "A",
		options.MetaServiceAddress, "meta service Address")
}

// ApplyTo checks validity of available command line options
func (options *BackupControllerFlags) ApplyTo(cfg *backup.Config) error {
	cfg.MetaServicePort = options.MetaServicePort
	cfg.MetaServiceAddress = options.MetaServiceAddress
	if net.ParseIP(options.MetaServiceAddress) == nil {
		// if not IP, try dns
		cfg.MetaServiceAddress = "dns:///" + options.MetaServiceAddress
	}

	return nil
}

// Validate checks validity of available command line options
func (options *BackupControllerFlags) Validate() []error {
	errs := make([]error, 0)
	if options.MetaServicePort <= 0 {
		errs = append(errs, fmt.Errorf("invalid port %d", options.MetaServicePort))
	}

	return errs
}
