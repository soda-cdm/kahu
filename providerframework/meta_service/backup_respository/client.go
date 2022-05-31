// Copyright 2022 The SODA Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package backup_respository

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	pb "github.com/soda-cdm/kahu/providers/lib/go"
)

type BackupRepository interface {
	Upload(filePath string) error
}

type backupRepository struct {
	backupRepositoryAddress string
	client                  pb.MetaBackupClient
}

func NewBackupRepository(backupRepositoryAddress string) (BackupRepository, error) {
	unixPrefix := "unix://"
	if strings.HasPrefix(backupRepositoryAddress, "/") {
		// It looks like filesystem path.
		backupRepositoryAddress = unixPrefix + backupRepositoryAddress
	}

	if !strings.HasPrefix(backupRepositoryAddress, unixPrefix) {
		return nil, fmt.Errorf("invalid unix domain path [%s]",
			backupRepositoryAddress)
	}

	grpcConnection, err := grpc.Dial(backupRepositoryAddress, grpc.WithInsecure(),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`))
	if err != nil {
		return nil, err
	}

	return &backupRepository{
		backupRepositoryAddress: backupRepositoryAddress,
		client:                  pb.NewMetaBackupClient(grpcConnection),
	}, nil
}

func (repo *backupRepository) Upload(filePath string) error {
	log.Infof("Archive file path %s", filePath)

	file, err := os.Open(filePath)
	if err != nil {
		log.Errorf("cannot open backup file: %s", err)
		return err
	}
	defer file.Close()

	repoClient, err := repo.client.Upload(context.Background())
	if err != nil {
		return err
	}

	err = repoClient.Send(&pb.UploadRequest{
		Data: &pb.UploadRequest_Info{
			Info: &pb.UploadRequest_FileInfo{
				FileIdentifier: path.Base(filePath),
			},
		},
	})
	if err != nil {
		return err
	}

	reader := bufio.NewReader(file)
	buffer := make([]byte, 1024)

	for {
		n, err := reader.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Errorf("cannot read buffer: %s", err)
			return err
		}

		err = repoClient.Send(&pb.UploadRequest{
			Data: &pb.UploadRequest_ChunkData{
				ChunkData: buffer[:n],
			},
		})
		if err != nil {
			log.Fatal("cannot send chunk to server: ", err, repoClient.RecvMsg(nil))
		}
	}

	// Close stream
	_, err = repoClient.CloseAndRecv()
	if err != nil {
		return err
	}
	return nil
}
