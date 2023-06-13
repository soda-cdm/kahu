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
	"errors"
	"fmt"
	"net"

	"github.com/spf13/pflag"

	"github.com/soda-cdm/kahu/providerframework/volumeservice/app/config"
)

const (
	defaultVolumeBackupDriverEndpoint = "/tmp/volumeservice.sock"
	defaultPort                       = 443
	defaultAddress                    = "0.0.0.0"
)

type serviceOptions struct {
	DriverEndpoint string
	Port           uint
	Address        string
}

func NewServiceOptions() *serviceOptions {
	return &serviceOptions{
		DriverEndpoint: defaultVolumeBackupDriverEndpoint,
		Port:           defaultPort,
		Address:        defaultAddress,
	}
}

func (opt *serviceOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&opt.DriverEndpoint, "driverEndpoint", opt.DriverEndpoint,
		"Volume backup driver endpoint")
	fs.UintVarP(&opt.Port, "port", "p", opt.Port,
		"Server port")
	fs.StringVarP(&opt.Address, "address", "a",
		opt.Address, "Server Address")
}

func (opt *serviceOptions) ApplyTo(cfg *config.Config) error {
	cfg.DriverEndpoint = opt.DriverEndpoint
	cfg.Port = opt.Port
	cfg.Address = opt.Address
	return nil
}

func (opt *serviceOptions) Validate() []error {
	errs := make([]error, 0)

	if opt.Port <= 0 {
		errs = append(errs, fmt.Errorf("invalid port %d", opt.Port))
	}

	if net.ParseIP(opt.Address) == nil {
		errs = append(errs, fmt.Errorf("invalid address %s", opt.Address))
	}

	if opt.DriverEndpoint == "" {
		errs = append(errs, errors.New("backup driver address can not be empty"))
	}

	// TODO(Amit Roushan): Add validation for list of controllers
	return errs
}
