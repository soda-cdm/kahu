package compressors

import (
	"compress/gzip"
	"github.com/soda-cdm/kahu/providerframework/meta_service/archiver"
	"github.com/soda-cdm/kahu/providerframework/meta_service/archiver/manager"
)

const (
	GZipType archiver.CompressionType = "gzip"
)

type gzipCompressor struct {
	writer archiver.Writer
	gzip   *gzip.Writer
}

func init() {
	manager.RegisterCompressionPlugins(GZipType, func(writer archiver.Writer) archiver.Writer {
		return &gzipCompressor{
			writer: writer,
			gzip:   gzip.NewWriter(writer),
		}
	})
}

func (compressor gzipCompressor) Close() error {
	err := compressor.gzip.Close()
	if err != nil {
		return err
	}

	err = compressor.writer.Close()
	if err != nil {
		return err
	}

	return nil
}

func (compressor gzipCompressor) Write(data []byte) (int, error) {
	return compressor.gzip.Write(data)
}
