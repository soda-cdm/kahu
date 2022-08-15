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

type ExampleTestSuite struct {
	suite.Suite
	mgr          *archivalManager
	archFileName string
	typ          archiver.CompressionType
	fwriter      *fakeStruct
	fReader      *fakeStruct
	invoke       func(Writer archiver.Writer) archiver.Writer
	readInvoke   func(Reader archiver.Reader) archiver.Reader
	archFilePath string
}

func (suite *ExampleTestSuite) BeforeTest(suiteName, testName string) {

	switch testName {
	case "TestGetArchiver":
		suite.mgr = &archivalManager{
			archiveYard: "fakeArchiveYard1",
		}
		suite.archFileName = "FakeArchfileName"

		err := os.Mkdir("fakeArchiveYard1", 0777)
		if err != nil {
			fmt.Print(err)
		}

		suite.typ = archiver.CompressionType("fake")
		suite.invoke = func(Writer archiver.Writer) archiver.Writer {
			return suite.fwriter
		}
		RegisterCompressionWriterPlugins(suite.typ, suite.invoke)

	case "TestGetArchiveReader":

		suite.mgr = &archivalManager{
			archiveYard: "fakeArchiveYard2",
		}
		err := os.Mkdir("fakeArchiveYard2", 0777)
		if err != nil {
			fmt.Print(err)
		}
		_, err = os.Create("fakeArchiveYard2/FakeArchfileName")
		if err != nil {
			fmt.Print(err)
		}

		suite.typ = archiver.CompressionType("name")

		suite.fReader = &fakeStruct{}
		suite.readInvoke = func(Reader archiver.Reader) archiver.Reader {
			return suite.fReader
		}
		RegisterCompressionReaderPlugins(suite.typ, suite.readInvoke)

		suite.archFilePath = "fakeArchiveYard2/FakeArchfileName"
	}
}

func (suite *ExampleTestSuite) AfterTest(suiteName, testName string) {

	switch testName {
	case "TestGetArchiver":
		err := os.Remove("fakeArchiveYard1/FakeArchfileName") //0755
		if err != nil {
			fmt.Print(err)
		}
		err = os.Remove("fakeArchiveYard1") //0755
		if err != nil {
			fmt.Print(err)
		}

	case "TestGetArchiveReader":
		err := os.Remove("fakeArchiveYard2/FakeArchfileName") //0755
		if err != nil {
			fmt.Print(err)
		}
		err = os.Remove("fakeArchiveYard2") //0755
		if err != nil {
			fmt.Print(err)
		}
	}
}

func (suite *ExampleTestSuite) TestGetArchiver() {

	//when the compression type is fake not registered
	faketyp := "faketype"
	_, _, err := suite.mgr.GetArchiver(archiver.CompressionType(faketyp), suite.archFileName)
	expErr := fmt.Errorf("archival plugin[%s] not available", faketyp)
	assert.Equal(suite.T(), expErr, err)

	//success case
	_, archFile, err := suite.mgr.GetArchiver(suite.typ, suite.archFileName)
	assert.Nil(suite.T(), err)
	assert.Equal(suite.T(), archFile, "fakeArchiveYard1/FakeArchfileName")

	//when trying to give already existing file
	fakefile := suite.archFileName
	err1 := fmt.Errorf("archival file(fakeArchiveYard1/FakeArchfileName) already exist")
	_, _, err = suite.mgr.GetArchiver(suite.typ, fakefile)
	assert.Equal(suite.T(), err1, err)
}

func (suite *ExampleTestSuite) TestGetArchiveReader() {
	//invalid compression type
	faketyp := archiver.CompressionType("faketype")
	_, err := suite.mgr.GetArchiveReader(faketyp, suite.archFileName)
	expErr := fmt.Errorf("archival plugin[%s] not available", faketyp)
	assert.Equal(suite.T(), expErr, err)

	//invalid path
	archFileName := "fakepath"
	_, err = suite.mgr.GetArchiveReader(suite.typ, archFileName)
	expErr = fmt.Errorf("archival file(%s) do not exist", archFileName)
	assert.Equal(suite.T(), expErr, err)

	//success case
	resArch, err := suite.mgr.GetArchiveReader(suite.typ, suite.archFilePath)
	assert.NotNil(suite.T(), resArch)
	assert.Nil(suite.T(), err)

}

func TestExampleTestSuite(t *testing.T) {
	suite.Run(t, new(ExampleTestSuite))
}
