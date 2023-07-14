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
)

const (
	DefaultPort            = 443
	DefaultAddress         = "0.0.0.0"
	DefaultServiceName     = "metadata-service"
	DefaultDriverSocketDir = "/tmp"
)

type CompressionType string

type MetaServiceFlags struct {
	Port            uint
	Address         string
	DriverSocketDir string
}

func NewMetaServiceFlags() *MetaServiceFlags {
	return &MetaServiceFlags{
		Port:            DefaultPort,
		Address:         DefaultAddress,
		DriverSocketDir: DefaultDriverSocketDir,
	}
}

// AddFlags exposes available command line options
func (options *MetaServiceFlags) AddFlags(fs *pflag.FlagSet) {
	fs.UintVarP(&options.Port, "port", "p", options.Port,
		"Server port")
	fs.StringVarP(&options.Address, "address", "a",
		options.Address, "Server Address")
	fs.StringVarP(&options.DriverSocketDir, "socket-dir", "D",
		options.DriverSocketDir, "The unix socket directory")
}

// Apply checks validity of available command line options
func (options *MetaServiceFlags) Validate() error {
	if options.Port <= 0 {
		return fmt.Errorf("invalid port %d", options.Port)
	}

	if net.ParseIP(options.Address) == nil {
		return fmt.Errorf("invalid address %s", options.Address)
	}

	if options.DriverSocketDir == "" {
		return errors.New("backup driver directory can not be empty")
	}

	return nil
}
