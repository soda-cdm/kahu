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

package backuprespository

import (
	"context"
	"os"
	"testing"

	pb "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
)

type ClientTestSuite struct {
	suite.Suite
	repo     BackupRepository
	filePath string
}

type FakeMetaBackup_DownloadClient struct {
	mock.Mock
	grpc.ClientStream
}

type FakeMetaBackup_UploadClient struct {
	mock.Mock
	grpc.ClientStream
}

func (f *FakeMetaBackup_DownloadClient) Recv() (*pb.DownloadResponse, error) {
	args := f.Called()
	return args.Get(0).(*pb.DownloadResponse), args.Error(1)
}

func (f *FakeMetaBackup_UploadClient) Send(ur *pb.UploadRequest) error {
	args := f.Called(ur)
	return args.Error(0)
}

func (f *FakeMetaBackup_UploadClient) CloseAndRecv() (*pb.Empty, error) {
	args := f.Called()
	return args.Get(0).(*pb.Empty), args.Error(1)
}

type FakeMetaBackupClient struct {
	mock.Mock
}

func (f *FakeMetaBackupClient) Upload(ctx context.Context, opts ...grpc.CallOption) (pb.MetaBackup_UploadClient, error) {
	args := f.Called(ctx, opts)
	return args.Get(0).(pb.MetaBackup_UploadClient), args.Error(1)
}

func (f *FakeMetaBackupClient) ObjectExists(ctx context.Context, in *pb.ObjectExistsRequest, opts ...grpc.CallOption) (*pb.ObjectExistsResponse, error) {
	args := f.Called(ctx, in, opts)
	return args.Get(0).(*pb.ObjectExistsResponse), args.Error(1)
}

func (f *FakeMetaBackupClient) Download(ctx context.Context, in *pb.DownloadRequest, opts ...grpc.CallOption) (pb.MetaBackup_DownloadClient, error) {
	args := f.Called(ctx, in, opts)
	return args.Get(0).(pb.MetaBackup_DownloadClient), args.Error(1)

}

func (f *FakeMetaBackupClient) Delete(ctx context.Context, in *pb.DeleteRequest, opts ...grpc.CallOption) (*pb.Empty, error) {
	args := f.Called(ctx, in, opts)
	return args.Get(0).(*pb.Empty), args.Error(1)
}

func (suite *ClientTestSuite) BeforeTest(suiteName, testName string) {
	suite.filePath = "FakeFile"
	switch testName {
	case "TestUploadInvalidPath", "TestDownloadFail":
		repo, _, _ := NewBackupRepository("/tmp/nfs_test.sock")
		suite.repo = repo

	case "TestUploadValid":
		client := &FakeMetaBackupClient{}
		fakeRespoClient := &FakeMetaBackup_UploadClient{}
		client.On("Upload", mock.Anything, mock.Anything).Return(fakeRespoClient, nil)
		suite.repo = &backupRepository{
			backupRepositoryAddress: "127.0.0.1:8181",
			client:                  client,
		}
		fakeRespoClient.On("Send", mock.Anything).Return(nil)
		fakeRespoClient.On("CloseAndRecv", mock.Anything).Return(&pb.Empty{}, nil)
		file, err := os.Create(suite.filePath)
		assert.Nil(suite.T(), err)
		defer file.Close()
		str := "this is sample data"
		data := []byte(str)
		err = os.WriteFile(suite.filePath, data, 0777)
	}
}

func (suite *ClientTestSuite) AfterTest(suiteName, testName string) {
	switch testName {
	case "TestUploadValid":
		err := os.Remove(suite.filePath)
		assert.Nil(suite.T(), err)

	}
}

func (suite *ClientTestSuite) TestUploadInvalidPath() {
	err := suite.repo.Upload(suite.filePath, map[string]string{})
	assert.NotNil(suite.T(), err)
}

func (suite *ClientTestSuite) TestDownloadFail() {
	attributes := map[string]string{"key": "value"}
	_, err := suite.repo.Download(suite.filePath, attributes)
	assert.NotNil(suite.T(), err)
}

func (suite *ClientTestSuite) TestUploadValid() {
	err := suite.repo.Upload(suite.filePath, map[string]string{})
	assert.Nil(suite.T(), err)

}

func TestTarTestSuite(t *testing.T) {
	suite.Run(t, new(ClientTestSuite))
}
