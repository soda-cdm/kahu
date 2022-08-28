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
	"os"
	"testing"

	"github.com/soda-cdm/kahu/providerframework/metaservice/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type GzipTestSuite struct {
	suite.Suite
	fakeGzipWriter *gzipWriter
	fakeGzipReader *gzipReader
	data           []byte
	fileName       string
	fakeStr        mocks.FakeStruct
}

func (suite *GzipTestSuite) BeforeTest(suiteName, testName string) {
	suite.fileName = "fakeFile"
	str := "Sample data to write in TestGzipReader"
	suite.data = []byte(str)
	switch testName {

	case "TestWriteGzipWriter", "TestCloseGzipWriter":
		suite.fakeStr.On("Write", mock.Anything).Return(1, nil)
		suite.fakeStr.On("Close").Return(nil)
		suite.fakeStr.On("Read", mock.Anything).Return(1, nil)
		suite.fakeGzipWriter = &gzipWriter{
			writer: &suite.fakeStr,
			gzip:   gzip.NewWriter(&suite.fakeStr),
		}

	case "TestCloseGzipReader", "TestReadGzipReader":

		out, err := os.Create(suite.fileName)
		assert.Nil(suite.T(), err)
		gzipWriter := gzip.NewWriter(out)
		_, err = gzipWriter.Write(suite.data)
		assert.Nil(suite.T(), err)
		gzipWriter.Close()
		out.Close()

		out, err = os.Open(suite.fileName)
		assert.Nil(suite.T(), err)

		gzip, _ := gzip.NewReader(out)
		suite.fakeGzipReader = &gzipReader{
			reader: out,
			gzip:   gzip,
		}
	}
}

func (suite *GzipTestSuite) AfterTest(suiteName, testName string) {
	switch testName {
	case "TestCloseGzipReader", "TestReadGzipReader":
		os.Remove(suite.fileName)
	}
}

func (suite *GzipTestSuite) TestCloseGzipWriter() {
	err := suite.fakeGzipWriter.Close()
	assert.Nil(suite.T(), err)
	suite.fakeStr.AssertCalled(suite.T(), "Close", mock.Anything)
}

func (suite *GzipTestSuite) TestWriteGzipWriter() {
	_, err := suite.fakeGzipWriter.Write(suite.data)
	assert.Nil(suite.T(), err)
	suite.fakeStr.AssertCalled(suite.T(), "Write", mock.Anything)
	suite.fakeStr.AssertCalled(suite.T(), "Close", mock.Anything)
}

func (suite *GzipTestSuite) TestCloseGzipReader() {
	err := suite.fakeGzipReader.Close()
	assert.Nil(suite.T(), err)
}

func (suite *GzipTestSuite) TestReadGzipReader() {
	_, err := suite.fakeGzipReader.Read(suite.data)
	assert.Equal(suite.T(), err, io.EOF)
}

func TestGzipTestSuite(t *testing.T) {
	suite.Run(t, new(GzipTestSuite))
}
