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

package nfs_provider

import (
	"context"
	"net"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"

	service "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/providers/nfsprovider/server"
	"github.com/soda-cdm/kahu/providers/nfsprovider/server/options"
)

const (
	protocol = "unix"
	sockAddr = "/tmp/nfs_test.sock"
	fileId   = "filename"
	fileData = "Test data ..."
)

var _ = Describe("NFSProviderService", func() {
	var (
		grpcServer     *grpc.Server
		grpcConnection *grpc.ClientConn
		nfsClient      service.MetaBackupClient
		attrib         map[string]string
	)

	uploadFunction := func() {
		fid := &service.UploadRequest_FileInfo{FileIdentifier: fileId, Attributes: attrib}
		data := []*service.UploadRequest{
			{Data: &service.UploadRequest_Info{Info: fid}},
			{Data: &service.UploadRequest_ChunkData{ChunkData: []byte(fileData)}},
		}

		backupClient, err := nfsClient.Upload(context.Background())
		Expect(err).To(BeNil())
		// Send info
		err = backupClient.Send(data[0])
		Expect(err).To(BeNil())
		// Send chunk
		err = backupClient.Send(data[1])
		Expect(err).To(BeNil())

		_, err = backupClient.CloseAndRecv()
		Expect(err).To(BeNil())
	}

	BeforeEach(func() {
		cleanup := func() {
			if _, err := os.Stat(sockAddr); err == nil {
				if err := os.RemoveAll(sockAddr); err != nil {
					log.Fatal(err)
				}
			}
		}

		cleanup()

		// initialize server
		lis, err := net.Listen(protocol, sockAddr)
		Expect(err).To(BeNil())

		var serverOpts []grpc.ServerOption
		grpcServer = grpc.NewServer(serverOpts...)
		service.RegisterMetaBackupServer(grpcServer,
			server.NewMetaBackupServer(context.Background(), options.NFSProviderOptions{}))
		go grpcServer.Serve(lis)

		// initialize client
		dialer := func(addr string, t time.Duration) (net.Conn, error) {
			return net.Dial(protocol, addr)
		}

		grpcConnection, err = grpc.Dial(sockAddr, grpc.WithInsecure(), grpc.WithDialer(dialer))
		Expect(err).To(BeNil())

		nfsClient = service.NewMetaBackupClient(grpcConnection)
		attrib = map[string]string{"key": "value"}
	})

	AfterEach(func() {
		grpcServer.Stop()
		grpcConnection.Close()
	})

	It("NFS Provider Upload", func() {
		uploadFunction()
	})

	It("NFS Provider Download", func() {
		uploadFunction()
		request := &service.DownloadRequest{FileIdentifier: fileId, Attributes: attrib}
		client, err := nfsClient.Download(context.Background(), request)
		Expect(err).To(BeNil())
		response, err := client.Recv()
		Expect(err).To(BeNil())
		Expect(response.GetInfo().FileIdentifier).To(Equal(fileId))
		response, err = client.Recv()
		Expect(err).To(BeNil())
		Expect(response.GetChunkData()).To(Equal([]byte(fileData)))
	})

	It("NFS Provider Check existence", func() {
		uploadFunction()
		request := &service.ObjectExistsRequest{FileIdentifier: fileId, Attributes: attrib}
		response, err := nfsClient.ObjectExists(context.Background(), request)
		Expect(err).To(BeNil())
		Expect(response.GetExists()).To(Equal(true))
		// Use invalid fileId
		request = &service.ObjectExistsRequest{FileIdentifier: fileId + "_NotExists", Attributes: attrib}
		response, err = nfsClient.ObjectExists(context.Background(), request)
		Expect(err).To(BeNil())
		Expect(response.GetExists()).To(Equal(false))
	})

	It("NFS Provider Delete", func() {
		uploadFunction()
		request := &service.DeleteRequest{FileIdentifier: fileId, Attributes: attrib}
		_, err := nfsClient.Delete(context.Background(), request)
		Expect(err).To(BeNil())
		req := &service.ObjectExistsRequest{FileIdentifier: fileId, Attributes: attrib}
		res, err := nfsClient.ObjectExists(context.Background(), req)
		Expect(err).To(BeNil())
		Expect(res.GetExists()).To(Equal(false))
	})
})
