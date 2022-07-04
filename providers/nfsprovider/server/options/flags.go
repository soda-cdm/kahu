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

// Package options defines nfs provider flag options
package options

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"
)

const (
	// NFSService component name
	unixSocketPath = "/tmp/nfs.sock"
	DataPath       = "/data"
)

// CompressionType defines type of compression for archival
type CompressionType string

// NFSServiceFlags defines flags for nfs services
type NFSServiceFlags struct {
	UnixSocketPath string
	DataPath       string
}

// NFSServiceFlags creats new nfs services
func NewNFSServiceFlags() *NFSServiceFlags {
	return &NFSServiceFlags{
		UnixSocketPath: unixSocketPath,
		// DataPath defines directory of backup files
		DataPath:       DataPath,
	}
}

// AddFlags exposes available command line options
func (options *NFSServiceFlags) AddFlags(fs *pflag.FlagSet) {
	fs.StringVarP(&options.UnixSocketPath, "socket", "s",
		options.UnixSocketPath, "Unix socket path")
	fs.StringVarP(&options.DataPath, "data", "d",
		options.DataPath, "NFS mount directory for storage")
}

// Apply checks validity of available command line options
func (options *NFSServiceFlags) Apply() error {
	if _, err := os.Stat(options.DataPath); os.IsNotExist(err) {
		return fmt.Errorf("nfs mount directory(%s) does not exist", options.DataPath)
	}

	return nil
}
