// Copyright 2022 The SODA Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package options

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/spf13/pflag"

	"github.com/soda-cdm/kahu/providerframework/meta_service/archiver/compressors"
	"github.com/soda-cdm/kahu/providerframework/meta_service/archiver/manager"
)

type CompressionType string

type MetaServiceFlags struct {
	Port              uint
	Address           string
	CompressionFormat string
	ArchivalYard      string
}

func NewMetaServiceFlags() *MetaServiceFlags {
	return &MetaServiceFlags{
		Port:              443,
		Address:           "127.0.0.1",
		CompressionFormat: string(compressors.GZipType),
		ArchivalYard:      "/tmp",
	}
}

// AddFlags exposes available command line options
func (options *MetaServiceFlags) AddFlags(fs *pflag.FlagSet) {
	fs.UintVarP(&options.Port, "port", "p", options.Port,
		"Server port")
	fs.StringVarP(&options.Address, "address", "a",
		options.Address, "Server Address")
	fs.StringVarP(&options.CompressionFormat, "compression-format", "f",
		options.CompressionFormat, fmt.Sprintf("Archival format. options(%s)",
			strings.Join(manager.GetCompressionPluginsNames(), ",")))
	fs.StringVarP(&options.ArchivalYard, "compression-dir", "d",
		options.ArchivalYard, "A directory for temporarily maintaining archived file")
}

// Apply checks validity of available command line options
func (options *MetaServiceFlags) Apply() error {
	if options.Port <= 0 {
		return fmt.Errorf("invalid port %d", options.Port)
	}

	if net.ParseIP(options.Address) == nil {
		return fmt.Errorf("invalid address %s", options.Address)
	}

	if ok := manager.CheckCompressor(options.CompressionFormat); !ok {
		return fmt.Errorf("invalid compression type %s", options.CompressionFormat)
	}

	if _, err := os.Stat(options.ArchivalYard); os.IsNotExist(err) {
		return fmt.Errorf("archival temporary directory(%s) does not exist", options.ArchivalYard)
	}

	return nil
}
