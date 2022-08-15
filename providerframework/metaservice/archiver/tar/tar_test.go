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
	return 1, nil
}

type TarTestSuite struct {
	suite.Suite
	fakestr *fakeStruct
}

func (suite *TarTestSuite) TestWriteFile() {

	file := "testFile"
	data := make([]byte, 1024)
	fakeTarWriter := tar.NewWriter(suite.fakestr)
	archiver := &tarArchiver{
		tar: fakeTarWriter,
	}
	err := archiver.WriteFile(file, data)
	assert.Nil(suite.T(), err)

	//data is nil
	var data1 []byte
	file1 := "testfile1"
	err = archiver.WriteFile(file1, data1)
	assert.NotNil(suite.T(), err)

}

func (suite *TarTestSuite) TestCloseTarArchiver() {

	fakeTarWriter := tar.NewWriter(suite.fakestr)
	fakeArchiverWriter := suite.fakestr
	archiver := &tarArchiver{
		writer: fakeArchiverWriter,
		tar:    fakeTarWriter,
	}
	err := archiver.Close()
	assert.Nil(suite.T(), err)
}

func (suite *TarTestSuite) TestCloseTarArchiveReader() {

	fakeTarReader := tar.NewReader(suite.fakestr)
	tarArchReader := &tarArchiveReader{
		reader: suite.fakestr,
		tar:    fakeTarReader,
	}
	err := tarArchReader.Close()
	assert.Nil(suite.T(), err)
}

func TestTarTestSuite(t *testing.T) {
	suite.Run(t, new(TarTestSuite))
}
