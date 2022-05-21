package archiver

import "io"

type Archiver interface {
	WriteFile(filePath string, data []byte) error
	Close() error
}

type CompressionType string

type ArchivalManager interface {
	GetArchiver(typ CompressionType, file string) (Archiver, error)
}

type Writer interface {
	io.Writer
	io.Closer
}
