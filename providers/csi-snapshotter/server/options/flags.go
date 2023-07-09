/*
Copyright 2023 The SODA Authors.

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

// Package options defines csi snapshot provider flag options
package options

import (
	"errors"

	"github.com/spf13/pflag"
)

const (
	// CSISnapshotterProvider component name
	unixSocketPath         = "/tmp/volumeservice.sock"
	defaultProviderName    = "snapshotter.backup.csi.io"
	defaultProviderVersion = "v1"
)

// CSISnapshotterProviderFlags defines flags for csi-snapshotter provider
type CSISnapshotterProviderFlags struct {
	UnixSocketPath  string
	ProviderName    string
	ProviderVersion string
}

// NewCSISnapshotterProviderFlags creates new csi-snapshotter provider flags
func NewCSISnapshotterProviderFlags() *CSISnapshotterProviderFlags {
	return &CSISnapshotterProviderFlags{
		UnixSocketPath: unixSocketPath,
	}
}

// AddFlags exposes available command line options
func (options *CSISnapshotterProviderFlags) AddFlags(fs *pflag.FlagSet) {
	fs.StringVarP(&options.UnixSocketPath, "socket", "s",
		options.UnixSocketPath, "Unix socket path")
	fs.StringVarP(&options.ProviderName, "provider", "p", defaultProviderName, "Provider name")
	fs.StringVarP(&options.ProviderVersion, "version", "V",
		defaultProviderVersion, "Volume backup provider version")
}

// Apply checks validity of available command line options
func (options *CSISnapshotterProviderFlags) Apply() error {
	if options.ProviderName == "" {
		return errors.New("empty provider name")
	}
	return nil
}
