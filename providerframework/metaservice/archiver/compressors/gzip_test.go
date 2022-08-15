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
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type fakeStruct struct{}

func (f *fakeStruct) Write(p []byte) (n int, err error) {
	return 1, nil
}

func (f *fakeStruct) Close() error {
	return nil
}

func (f *fakeStruct) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (f *fakeStruct) ReadByte() (byte, error) {
	return 1, nil
}

type GzipTestSuite struct {
	suite.Suite
}

func (suite *GzipTestSuite) TestCloseGzipWriter() {

	fakeStr := &fakeStruct{}
	fakeCompressor := &gzipWriter{
		writer: fakeStr,
		gzip:   gzip.NewWriter(fakeStr),
	}
	err := fakeCompressor.Close()
	assert.Nil(suite.T(), err)
}

func (suite *GzipTestSuite) TestWriteGzipWriter() {

	fakeStr := &fakeStruct{}
	fakeCompressor := &gzipWriter{
		writer: fakeStr,
		gzip:   gzip.NewWriter(fakeStr),
	}
	data := make([]byte, 1024)
	_, err := fakeCompressor.Write(data)
	assert.Nil(suite.T(), err)
}

func TestGzipTestSuite(t *testing.T) {
	suite.Run(t, new(GzipTestSuite))
}
