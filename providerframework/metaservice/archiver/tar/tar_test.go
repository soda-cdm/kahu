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
	"os"
	"testing"

	"github.com/soda-cdm/kahu/providerframework/metaservice/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type TarTestSuite struct {
	suite.Suite
	fakestr       mocks.FakeStruct
	file          string
	data          []byte
	archiver      *tarArchiver
	tarArchReader *tarArchiveReader
}

func (suite *TarTestSuite) BeforeTest(suiteName, testName string) {
	suite.file = "testFile"
	str := "hello world!!!"
	suite.data = []byte(str)
	suite.fakestr.On("Write", mock.Anything).Return(1, nil)
	suite.fakestr.On("Close").Return(nil)
	suite.fakestr.On("Read", mock.Anything).Return(1, nil)

	switch testName {
	case "TestWriteFile", "TestCloseTarArchiver":
		fakeTarWriter := tar.NewWriter(&suite.fakestr)
		suite.archiver = &tarArchiver{
			writer: &suite.fakestr,
			tar:    fakeTarWriter,
		}
	case "TestCloseTarArchiveReader":
		fakeTarReader := tar.NewReader(&suite.fakestr)
		suite.tarArchReader = &tarArchiveReader{
			reader: &suite.fakestr,
			tar:    fakeTarReader,
		}
	case "TestReadNext":

		file, err := os.Create(suite.file)
		assert.Nil(suite.T(), err)
		defer file.Close()
		tw := tar.NewWriter(file)
		defer tw.Close()

		hdr := &tar.Header{
			Name: suite.file,
			Size: int64(len(suite.data)),
		}
		err = tw.WriteHeader(hdr)
		assert.Nil(suite.T(), err)

		_, err = tw.Write(suite.data)
		assert.Nil(suite.T(), err)

		file, err = os.Open(suite.file)
		assert.Nil(suite.T(), err)
		fakeTarReader := tar.NewReader(file)
		suite.tarArchReader = &tarArchiveReader{
			reader: file,
			tar:    fakeTarReader,
		}
	}
}

func (suite *TarTestSuite) AfterTest(suiteName, testName string) {
	switch testName {
	case "TestWriteFile", "TestReadNext":
		os.Remove(suite.file)
	}
}

func (suite *TarTestSuite) TestWriteFile() {
	err := suite.archiver.WriteFile(suite.file, suite.data)
	assert.Nil(suite.T(), err)
	suite.fakestr.AssertCalled(suite.T(), "Write", mock.Anything)
}

func (suite *TarTestSuite) TestCloseTarArchiver() {
	err := suite.archiver.Close()
	assert.Nil(suite.T(), err)
	suite.fakestr.AssertCalled(suite.T(), "Close", mock.Anything)
}

func (suite *TarTestSuite) TestCloseTarArchiveReader() {
	err := suite.tarArchReader.Close()
	assert.Nil(suite.T(), err)
	suite.fakestr.AssertCalled(suite.T(), "Close", mock.Anything)
}

func (suite *TarTestSuite) TestReadNext() {
	_, _, err := suite.tarArchReader.ReadNext()
	assert.Nil(suite.T(), err)
}

func TestTarTestSuite(t *testing.T) {
	suite.Run(t, new(TarTestSuite))
}
