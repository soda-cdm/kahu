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
