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

package meta_service

import (
	"context"
	"fmt"
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"

	pb "github.com/soda-cdm/kahu/providerframework/meta_service/lib/go"
	"github.com/soda-cdm/kahu/providerframework/meta_service/server"
	"github.com/soda-cdm/kahu/providerframework/meta_service/server/options"
)

var _ = Describe("MetaService", func() {
	var (
		address        = "127.0.0.1"
		port           = 8181
		grpcServer     *grpc.Server
		grpcConnection *grpc.ClientConn
		metaClient     pb.MetaServiceClient
	)

	BeforeEach(func() {
		// initialize server
		lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d",
			address, port))
		Expect(err).To(BeNil())

		var serverOpts []grpc.ServerOption
		grpcServer = grpc.NewServer(serverOpts...)
		pb.RegisterMetaServiceServer(grpcServer,
			server.NewMetaServiceServer(context.Background(), options.MetaServiceOptions{
				MetaServiceFlags: *options.NewMetaServiceFlags(),
			}))
		go grpcServer.Serve(lis)

		// initialize client
		grpcConnection, err = pb.NewLBDial(fmt.Sprintf("%s:%d", address, port),
			grpc.WithInsecure())
		Expect(err).To(BeNil())

		metaClient = pb.NewMetaServiceClient(grpcConnection)
	})

	AfterEach(func() {
		grpcServer.Stop()
		err := grpcConnection.Close()
		Expect(err).To(BeNil())
	})

	It("Ensure basic Flow", func() {
		backupClient, err := metaClient.Backup(context.Background())
		Expect(err).To(BeNil())

		// send backup info
		err = backupClient.Send(&pb.BackupRequest{
			Backup: &pb.BackupRequest_Identifier{
				Identifier: &pb.BackupIdentifier{
					BackupHandle: "test_basic_backup",
				},
			},
		})
		Expect(err).To(BeNil())

		// send backup data
		err = backupClient.Send(&pb.BackupRequest{
			Backup: &pb.BackupRequest_BackupResource{
				BackupResource: &pb.BackResource{
					Resource: &pb.Resource{
						Name:    "test",
						Group:   "test_group",
						Version: "v1beta1",
						Kind:    "Test",
					},
					Data: []byte("test it ....... "),
				},
			},
		})
		Expect(err).To(BeNil())

		// Close stream
		_, err = backupClient.CloseAndRecv()
		Expect(err).To(BeNil())
	})
})
