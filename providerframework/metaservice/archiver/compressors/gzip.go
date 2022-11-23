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

package compressors

import (
	"compress/gzip"

	"github.com/soda-cdm/kahu/providerframework/metaservice/archiver"
	"github.com/soda-cdm/kahu/providerframework/metaservice/archiver/manager"
)

const (
	GZipType archiver.CompressionType = "gzip"
)

type gzipWriter struct {
	writer archiver.Writer
	gzip   *gzip.Writer
}

type gzipReader struct {
	reader archiver.Reader
	gzip   *gzip.Reader
}

func init() {
	manager.RegisterCompressionWriterPlugins(GZipType, func(writer archiver.Writer) (archiver.Writer, error) {
		return &gzipWriter{
			writer: writer,
			gzip:   gzip.NewWriter(writer),
		}, nil
	})

	manager.RegisterCompressionReaderPlugins(GZipType, func(reader archiver.Reader) (archiver.Reader, error) {
		newReader, err := gzip.NewReader(reader)
		if err != nil {
			return nil, err
		}
		return &gzipReader{
			reader: reader,
			gzip:   newReader,
		}, nil
	})
}

func (compressor gzipWriter) Close() error {
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

func (compressor gzipWriter) Write(data []byte) (int, error) {
	return compressor.gzip.Write(data)
}

func (compressor gzipReader) Read(data []byte) (int, error) {
	return compressor.gzip.Read(data)
}

func (compressor gzipReader) Close() error {
	err := compressor.gzip.Close()
	if err != nil {
		return err
	}

	err = compressor.reader.Close()
	if err != nil {
		return err
	}

	return nil
}
