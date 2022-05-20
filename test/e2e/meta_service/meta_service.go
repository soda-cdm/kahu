package meta_service

import (
	"context"
	"fmt"
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"

	metaservice "github.com/soda-cdm/kahu/provider/meta_service/lib/go"
	"github.com/soda-cdm/kahu/provider/meta_service/server"
	"github.com/soda-cdm/kahu/provider/meta_service/server/options"
)

var _ = Describe("MetaService", func() {
	var (
		address        = "127.0.0.1"
		port           = 8181
		grpcServer     *grpc.Server
		grpcConnection *grpc.ClientConn
		metaClient     metaservice.MetaServiceClient
	)

	BeforeEach(func() {
		// initialize server
		lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d",
			address, port))
		Expect(err).To(BeNil())

		var serverOpts []grpc.ServerOption
		grpcServer = grpc.NewServer(serverOpts...)
		metaservice.RegisterMetaServiceServer(grpcServer,
			server.NewMetaServiceServer(context.Background(), options.MetaServiceOptions{}))
		go grpcServer.Serve(lis)

		// initialize client
		grpcConnection, err = grpc.Dial(fmt.Sprintf("%s:%d", address, port), grpc.WithInsecure())
		Expect(err).To(BeNil())

		metaClient = metaservice.NewMetaServiceClient(grpcConnection)
	})

	AfterEach(func() {
		grpcServer.Stop()
		grpcConnection.Close()
	})

	It("Basic Flow", func() {
		backupClient, err := metaClient.Backup(context.Background())
		Expect(err).To(BeNil())

		backupClient.Send(&metaservice.BackupRequest{})
	})
})
