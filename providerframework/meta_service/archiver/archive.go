package archiver

import "io"

type Archiver interface {
	WriteFile(filePath string, data []byte) error
	Close() error
}

type CompressionType string

type ArchivalManager interface {
	GetArchiver(typ CompressionType,
		file string) (archiver Archiver, filePath string, err error)
}

type Writer interface {
	io.Writer
	io.Closer
}
