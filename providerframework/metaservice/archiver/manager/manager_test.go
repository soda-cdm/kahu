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
	"testing"

	"github.com/soda-cdm/kahu/providerframework/metaservice/archiver"
	"github.com/stretchr/testify/assert"
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

func TestGetArchiverInvalidTyp(t *testing.T) {
	//here Registeration of CompressionWriterPlugins is not done
	mgr := &archivalManager{
		archiveYard: "fakeArchiveYard",
	}
	archFileName := "FakeArchfileName1"
	typ := archiver.CompressionType("name")

	_, _, err := mgr.GetArchiver(typ, archFileName)
	assert.Equal(t, fmt.Errorf("archival plugin[%s] not available", typ), err)

}

func TestGetArchiverInvalidPath(t *testing.T) {
	//Here archiver file path given is invalid i.e "fakeArchiveYardfake" archiveYard doesnt exists
	mgr := &archivalManager{
		archiveYard: "fakeArchiveYardfake",
	}
	archFileName := "FakeArchfileName1"
	typ := archiver.CompressionType("name")
	fWriter := &fakeStruct{}
	invoke := func(Writer archiver.Writer) archiver.Writer {
		return fWriter
	}
	RegisterCompressionWriterPlugins(typ, invoke)
	_, _, err := mgr.GetArchiver(typ, archFileName)
	assert.NotNil(t, err)
}

func TestGetArchiver(t *testing.T) {
	//Success case it is
	//where "fakeArchiveYard" folder exists and "FakeArchfileName" file name doesnt exists in the folder
	mgr := &archivalManager{
		archiveYard: "fakeArchiveYard",
	}
	archFileName := "FakeArchfileName"
	typ := archiver.CompressionType("name")
	fWriter := &fakeStruct{}
	invoke := func(Writer archiver.Writer) archiver.Writer {
		return fWriter
	}
	RegisterCompressionWriterPlugins(typ, invoke)

	resArch, archFile, err := mgr.GetArchiver(typ, archFileName)
	assert.NotNil(t, resArch)
	assert.Nil(t, err)
	assert.Equal(t, "fakeArchiveYard/FakeArchfileName", archFile)

}

func TestGetArchiveReaderInvalidTyp(t *testing.T) {
	//here Registeration of CompressionReaderPlugins is not done
	mgr := &archivalManager{
		archiveYard: "fakeArchiveYard",
	}
	archFileName := "FakeArchfileName"
	typ := archiver.CompressionType("name")
	_, err := mgr.GetArchiveReader(typ, archFileName)
	assert.Equal(t, fmt.Errorf("archival plugin[%s] not available", typ), err)
}

func TestGetArchiveReaderInvalidPath(t *testing.T) {
	//archival file fakeArchiveYard1/FakeArchfileName2 do not exist
	mgr := &archivalManager{
		archiveYard: "fakeArchiveYard1",
	}
	archFileName := "FakeArchfileName2"
	typ := archiver.CompressionType("name")
	fReader := &fakeStruct{}
	invoke := func(Reader archiver.Reader) archiver.Reader {
		return fReader
	}
	RegisterCompressionReaderPlugins(typ, invoke)
	_, err := mgr.GetArchiveReader(typ, archFileName)
	assert.NotNil(t, err)
}

func TestGetArchiveReader(t *testing.T) {
	//Success case it is
	//where "fakeArchiveYard" folder name exists and "FakeArchfileName" file name  exists in the folder
	mgr := &archivalManager{
		archiveYard: "fakeArchiveYard",
	}
	typ := archiver.CompressionType("name")

	fReader := &fakeStruct{}
	invoke := func(Reader archiver.Reader) archiver.Reader {
		return fReader
	}
	RegisterCompressionReaderPlugins(typ, invoke)

	archFilePath := "fakeArchiveYard/FakeArchfileName"
	resArch, err := mgr.GetArchiveReader(typ, archFilePath)
	assert.NotNil(t, resArch)
	assert.Nil(t, err)

}
