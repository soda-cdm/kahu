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

	"github.com/soda-cdm/kahu/controllers"
	"github.com/spf13/pflag"
)

type BackupControllerFlags struct {
	MetaServicePort    uint
	MetaServiceAddress string
	KubeConfig         string
}

func NewBackupControllerFlags() *BackupControllerFlags {
	return &BackupControllerFlags{
		MetaServicePort:    controllers.DefaultMetaServicePort,
		MetaServiceAddress: controllers.DefaultMetaServiceAddress,
		KubeConfig:         "",
	}
}

// AddFlags exposes available command line options
func (options *BackupControllerFlags) AddFlags(fs *pflag.FlagSet) {
	fs.UintVarP(&options.MetaServicePort, "meta-port", "P", options.MetaServicePort,
		"Meta service port")
	fs.StringVarP(&options.MetaServiceAddress, "meta-address", "A",
		options.MetaServiceAddress, "meta service Address")
	fs.StringVarP(&options.KubeConfig, "kubeconfig", "k",
		options.KubeConfig, "kube config path")
}

// Apply checks validity of available command line options
func (options *BackupControllerFlags) Apply() error {
	if options.MetaServicePort <= 0 {
		return fmt.Errorf("invalid port %d", options.MetaServicePort)
	}

	if net.ParseIP(options.MetaServiceAddress) == nil {
		// if not IP, try dns
		options.MetaServiceAddress = "dns:///" + options.MetaServiceAddress
	}

	return nil
}

func (options *BackupControllerFlags) Print() string {
	return fmt.Sprintf("%+v", options)
}
