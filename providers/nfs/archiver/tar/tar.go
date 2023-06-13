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
	"fmt"
	"io"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/soda-cdm/kahu/providers/nfs/archiver"
)

const (
	tarFileNullSize = 1024
)

type tarArchiver struct {
	writer archiver.Writer
	tar    *tar.Writer
}

type tarArchiveReader struct {
	reader archiver.Reader
	tar    *tar.Reader
}

func NewArchiveWriter(filePath string) (archiver.Archiver, error) {
	var file *os.File
	if _, err := os.Stat(filePath); err == nil {
		file, err = os.OpenFile(filePath, os.O_RDWR, os.ModePerm)
		if err != nil {
			log.Errorf("Failed to open file for upload to NFS. %s", err)
			return nil, err
		}
		if _, err = file.Seek(-tarFileNullSize, io.SeekEnd); err != nil {
			log.Errorf("Failed to open file for upload to NFS. %s", err)
			return nil, err
		}
	} else {
		file, err = os.OpenFile(filePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.ModePerm)
		if err != nil {
			log.Errorf("Failed to open file for upload to NFS. %s", err)
			return nil, err
		}
	}

	return &tarArchiver{
		writer: file,
		tar:    tar.NewWriter(file),
	}, nil
}

func NewArchiveReader(filePath string) (archiver.ArchiveReader, error) {
	// check file existence
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("archival file(%s) do not exist", filePath)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	return &tarArchiveReader{
		reader: file,
		tar:    tar.NewReader(file),
	}, nil
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
