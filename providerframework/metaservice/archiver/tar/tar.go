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

package tar

import (
	"archive/tar"
	"io"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/soda-cdm/kahu/providerframework/metaservice/archiver"
)

type tarArchiver struct {
	writer archiver.Writer
	tar    *tar.Writer
}

type tarArchiveReader struct {
	reader archiver.Reader
	tar    *tar.Reader
}

func NewArchiver(writer archiver.Writer) archiver.Archiver {
	return &tarArchiver{
		writer: writer,
		tar:    tar.NewWriter(writer),
	}
}

func NewArchiveReader(reader archiver.Reader) archiver.ArchiveReader {
	return &tarArchiveReader{
		reader: reader,
		tar:    tar.NewReader(reader),
	}
}

func (archiver *tarArchiver) WriteFile(file string, data []byte) error {
	hdr := &tar.Header{
		Name:     file,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
		Mode:     0755,
		ModTime:  time.Now(),
	}

	log.Infof("writing header %+v", hdr)
	if err := archiver.tar.WriteHeader(hdr); err != nil {
		return err
	}

	log.Infof("writing data %s", data)
	if _, err := archiver.tar.Write(data); err != nil {
		return err
	}
	return nil
}

func (archiver *tarArchiver) Close() error {
	err := archiver.tar.Close()
	if err != nil {
		return err
	}

	err = archiver.writer.Close()
	if err != nil {
		return err
	}

	return nil
}

func (archiver *tarArchiveReader) ReadNext() (*tar.Header, io.Reader, error) {
	header, err := archiver.tar.Next()
	if err != nil {
		return nil, nil, err
	}
	return header, archiver.tar, err
}

func (archiver *tarArchiveReader) Close() error {
	err := archiver.reader.Close()
	if err != nil {
		return err
	}

	return nil
}
