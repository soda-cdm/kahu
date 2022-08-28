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

package manager

import (
	"fmt"
	"os"
	"testing"

	"github.com/soda-cdm/kahu/providerframework/metaservice/archiver"
	"github.com/soda-cdm/kahu/providerframework/metaservice/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ManagerTestSuite struct {
	suite.Suite
	mgr          *archivalManager
	archFileName string
	typ          archiver.CompressionType
	fwriter      *mocks.FakeStruct
	fReader      *mocks.FakeStruct
	wrirteInvoke func(Writer archiver.Writer) archiver.Writer
	readInvoke   func(Reader archiver.Reader) archiver.Reader
	archFilePath string
	fakeType     string
	archieveYard string
}

func (suite *ManagerTestSuite) BeforeTest(suiteName, testName string) {
	suite.fakeType = "faketype"
	suite.archieveYard = "fakeArchiveYard1"
	suite.archFileName = "FakeArchfileName"
	suite.typ = archiver.CompressionType("fake")
	suite.archFilePath = suite.archieveYard + "/" + suite.archFileName

	GetArchiverInit := func() {
		suite.mgr = &archivalManager{
			archiveYard: suite.archieveYard,
		}
		suite.archFileName = "FakeArchfileName"

		err := os.Mkdir(suite.archieveYard, 0777)
		assert.Nil(suite.T(), err)

		suite.wrirteInvoke = func(Writer archiver.Writer) archiver.Writer {
			return suite.fwriter
		}
		RegisterCompressionWriterPlugins(suite.typ, suite.wrirteInvoke)
	}

	GetArchiveReaderInit := func() {
		suite.mgr = &archivalManager{
			archiveYard: suite.archieveYard,
		}
		err := os.Mkdir(suite.archieveYard, 0777)
		assert.Nil(suite.T(), err)

		suite.fReader = &mocks.FakeStruct{}
		suite.readInvoke = func(Reader archiver.Reader) archiver.Reader {
			return suite.fReader
		}
		RegisterCompressionReaderPlugins(suite.typ, suite.readInvoke)
	}

	switch testName {
	case "TestGetArchiverFileAlreadyExists":
		GetArchiverInit()
		_, err := os.Create(suite.archFilePath)
		assert.Nil(suite.T(), err)
	case "TestGetArchiverSuccess":
		GetArchiverInit()
	case "TestGetArchiveReaderInvalidFilePath":
		GetArchiveReaderInit()

	case "TestGetArchiveReaderSuccess":
		GetArchiveReaderInit()
		_, err := os.Create(suite.archFilePath)
		assert.Nil(suite.T(), err)
	}
}

func (suite *ManagerTestSuite) AfterTest(suiteName, testName string) {

	switch testName {
	case "TestGetArchiverFileAlreadyExists", "TestGetArchiverSuccess", "TestGetArchiveReaderSuccess":
		err := os.Remove(suite.archFilePath)
		assert.Nil(suite.T(), err)

		err = os.Remove(suite.archieveYard)
		assert.Nil(suite.T(), err)

	case "TestGetArchiveReaderInvalidFilePath":
		err := os.Remove(suite.archieveYard)
		assert.Nil(suite.T(), err)
	}
}

func (suite *ManagerTestSuite) TestGetArchiverFakeCompressionType() {

	//when the compression type is fake not registered
	_, _, err := suite.mgr.GetArchiver(archiver.CompressionType(suite.fakeType), suite.archFileName)
	expErr := fmt.Errorf("archival plugin[%s] not available", suite.fakeType)
	assert.Equal(suite.T(), expErr, err)
}

func (suite *ManagerTestSuite) TestGetArchiverSuccess() {
	//success case
	_, archFile, err := suite.mgr.GetArchiver(suite.typ, suite.archFileName)
	assert.Nil(suite.T(), err)
	assert.Equal(suite.T(), archFile, "fakeArchiveYard1/FakeArchfileName")
}

func (suite *ManagerTestSuite) TestGetArchiverFileAlreadyExists() {
	//when trying to give already existing file
	fakefile := suite.archFileName
	expErr := fmt.Errorf("archival file(fakeArchiveYard1/FakeArchfileName) already exist")
	_, _, err := suite.mgr.GetArchiver(suite.typ, fakefile)
	assert.Equal(suite.T(), expErr, err)
}

func (suite *ManagerTestSuite) TestGetArchiveReaderFakeCompressionType() {
	//invalid compression type

	_, err := suite.mgr.GetArchiveReader(archiver.CompressionType(suite.fakeType), suite.archFileName)
	expErr := fmt.Errorf("archival plugin[%s] not available", suite.fakeType)
	assert.Equal(suite.T(), expErr, err)
}

func (suite *ManagerTestSuite) TestGetArchiveReaderInvalidFilePath() {
	//invalid path
	archFileName := "fakepath"
	_, err := suite.mgr.GetArchiveReader(suite.typ, archFileName)
	expErr := fmt.Errorf("archival file(%s) do not exist", archFileName)
	assert.Equal(suite.T(), expErr, err)
}
func (suite *ManagerTestSuite) TestGetArchiveReaderSuccess() {
	//success case
	resArch, err := suite.mgr.GetArchiveReader(suite.typ, suite.archFilePath)
	assert.NotNil(suite.T(), resArch)
	assert.Nil(suite.T(), err)

}

func TestManagerTestSuite(t *testing.T) {
	suite.Run(t, new(ManagerTestSuite))
}
