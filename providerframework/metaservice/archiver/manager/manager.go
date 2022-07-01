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

package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/soda-cdm/kahu/providerframework/metaservice/archiver"
	"github.com/soda-cdm/kahu/providerframework/metaservice/archiver/tar"
)

type archivalManager struct {
	archiveYard string
}

type compressorPluginManager struct {
	sync.Mutex
	compressionPlugins map[archiver.CompressionType]func(archiver.Writer) archiver.Writer
}

var cpm *compressorPluginManager
var dbConnOnce sync.Once

func init() {
	initManager()
}

func initManager() {
	dbConnOnce.Do(func() {
		cpm = &compressorPluginManager{
			compressionPlugins: make(map[archiver.CompressionType]func(archiver.Writer) archiver.Writer),
		}
	})
}

func RegisterCompressionPlugins(name archiver.CompressionType,
	invoke func(archiver.Writer) archiver.Writer) {
	cpm.Lock()
	defer cpm.Unlock()

	cpm.compressionPlugins[name] = invoke
}

func GetCompressionPluginsNames() []string {
	cpm.Lock()
	defer cpm.Unlock()

	var plugins []string
	for plugin := range cpm.compressionPlugins {
		plugins = append(plugins, string(plugin))
	}

	return plugins
}

func CheckCompressor(compressor string) bool {
	_, ok := cpm.compressionPlugins[archiver.CompressionType(compressor)]
	return ok
}

func NewArchiveManager(archiveYard string) archiver.ArchivalManager {
	return &archivalManager{
		archiveYard: archiveYard,
	}
}

func (mgr *archivalManager) GetArchiver(typ archiver.CompressionType,
	archiveFileName string) (archiver.Archiver, string, error) {

	cpm.Lock()
	compressorFunc, ok := cpm.compressionPlugins[typ]
	cpm.Unlock()
	if !ok {
		return nil, "", fmt.Errorf("archival plugin[%s] not available", typ)
	}

	archiveFile := filepath.Join(mgr.archiveYard, archiveFileName)
	// check file existence
	if _, err := os.Stat(archiveFile); !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("archival file(%s) already exist", archiveFile)
	}

	file, err := os.Create(archiveFile)
	if err != nil {
		return nil, "", err
	}

	return tar.NewArchiver(compressorFunc(file)), archiveFile, nil
}
