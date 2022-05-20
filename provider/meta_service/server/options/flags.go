package options

import (
	"fmt"
	"github.com/spf13/pflag"
	"net"
	"os"
)

type CompressionType string

const (
	Tar CompressionType = "tar"
)

type MetaServiceFlags struct {
	Port           uint
	Address        string
	ArchivalFormat string
	ArchivalYard   string
}

func NewMetaServiceFlags() *MetaServiceFlags {
	return &MetaServiceFlags{
		Port:           443,
		Address:        "127.0.0.1",
		ArchivalFormat: string(Tar),
		ArchivalYard:   "/tmp",
	}
}

// AddFlags exposes available command line options
func (options *MetaServiceFlags) AddFlags(fs *pflag.FlagSet) {
	fs.UintVarP(&options.Port, "port", "p", options.Port,
		"Server port")
	fs.StringVarP(&options.Address, "address", "a",
		options.Address, "Server Address")
	fs.StringVarP(&options.ArchivalFormat, "archival-format", "f",
		options.ArchivalFormat, "Archival format")
	fs.StringVarP(&options.ArchivalYard, "compression-dir", "d",
		options.ArchivalYard, "A directory for temporarily maintaining archived file")
}

// ValidateFlags checks validity of available command line options
func (options *MetaServiceFlags) ValidateFlags() error {
	if options.Port <= 0 {
		return fmt.Errorf("invalid port %d", options.Port)
	}

	if net.ParseIP(options.Address) == nil {
		return fmt.Errorf("invalid address %s", options.Address)
	}

	switch options.ArchivalFormat {
	case string(Tar):
	default:
		return fmt.Errorf("invalid compression type %s", options.ArchivalFormat)
	}

	if _, err := os.Stat(options.ArchivalYard); os.IsNotExist(err) {
		return fmt.Errorf("archival temporary directory(%s) does not exist", options.ArchivalYard)
	}

	return nil
}
