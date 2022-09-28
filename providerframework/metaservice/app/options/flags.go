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
	"os"
	"strings"

	"github.com/spf13/pflag"

	"github.com/soda-cdm/kahu/providerframework/metaservice/archiver/compressors"
	"github.com/soda-cdm/kahu/providerframework/metaservice/archiver/manager"
)

const (
	DefaultPort                = 443
	DefaultAddress             = "0.0.0.0"
	DefaultCompressionFormat   = string(compressors.GZipType)
	DefaultArchivalYard        = "/tmp"
	DefaultBackupDriverAddress = "/tmp/nfs.sock"
	DefaultDeployNamespace     = "default"
	DefaultServiceName         = "metadata-service"
	namespaceEnv               = "NAMESPACE"
	serviceNamesEnv            = "NAME"
)

type CompressionType string

type MetaServiceFlags struct {
	Port                uint
	Address             string
	CompressionFormat   string
	ArchivalYard        string
	BackupDriverAddress string
	DeployedNamespace   string
	ServiceName         string
}

func NewMetaServiceFlags() *MetaServiceFlags {
	return &MetaServiceFlags{
		Port:                DefaultPort,
		Address:             DefaultAddress,
		CompressionFormat:   DefaultCompressionFormat,
		ArchivalYard:        DefaultArchivalYard,
		BackupDriverAddress: DefaultBackupDriverAddress,
		DeployedNamespace:   DefaultDeployNamespace,
		ServiceName:         DefaultServiceName,
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
		options.ArchivalYard, "A directory for temporarily maintaining backup")
	fs.StringVarP(&options.BackupDriverAddress, "driver-address", "D",
		options.BackupDriverAddress, "The grpc address of target backup driver")
	fs.StringVarP(&options.DeployedNamespace, "namespace", "n",
		os.Getenv(namespaceEnv), "Namespace where metadata service is deployed")
	fs.StringVarP(&options.ServiceName, "name", "s",
		os.Getenv(serviceNamesEnv), "Name of the metadata service being deployed")
}

// Apply checks validity of available command line options
func (options *MetaServiceFlags) Apply() error {
	if options.Port <= 0 {
		return fmt.Errorf("invalid port %d", options.Port)
	}

	if net.ParseIP(options.Address) == nil {
		return fmt.Errorf("invalid address %s", options.Address)
	}

	if ok := manager.CheckWriterCompressor(options.CompressionFormat); !ok {
		return fmt.Errorf("invalid compression type %s", options.CompressionFormat)
	}

	if _, err := os.Stat(options.ArchivalYard); err != nil && os.IsNotExist(err) {
		return fmt.Errorf("archival temporary directory(%s) does not exist", options.ArchivalYard)
	}

	if options.BackupDriverAddress == "" {
		return errors.New("backup driver address can not be empty")
	}

	if options.DeployedNamespace == "" {
		return errors.New("namespace can not be empty")
	}

	if options.ServiceName == "" {
		return errors.New("name can not be empty")
	}

	return nil
}
