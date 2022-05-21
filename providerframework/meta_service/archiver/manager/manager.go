package manager

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"sync"

	"github.com/soda-cdm/kahu/providerframework/meta_service/archiver"
	"github.com/soda-cdm/kahu/providerframework/meta_service/archiver/tar"
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
	for plugin, _ := range cpm.compressionPlugins {
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
	archiveFile string) (archiver.Archiver, error) {

	cpm.Lock()
	compressorFunc, ok := cpm.compressionPlugins[typ]
	cpm.Unlock()
	if !ok {
		return nil, fmt.Errorf("archival plugin[%s] not available", typ)
	}

	// check file existence
	if _, err := os.Stat(archiveFile); !os.IsNotExist(err) {
		return nil, fmt.Errorf("archival file(%s) already exist", archiveFile)
	}

	log.Debugf("File created --- %s", archiveFile)

	file, err := os.Create(filepath.Join(mgr.archiveYard, archiveFile))
	if err != nil {
		return nil, err
	}

	return tar.NewArchiver(compressorFunc(file)), nil
}
