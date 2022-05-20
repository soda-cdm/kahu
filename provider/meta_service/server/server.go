package server

import (
	"context"
	log "github.com/sirupsen/logrus"

	metaservice "github.com/soda-cdm/kahu/provider/meta_service/lib/go"
	"github.com/soda-cdm/kahu/provider/meta_service/server/options"
)

type metaServer struct {
	ctx            context.Context
	serviceOptions options.MetaServiceOptions
}

func NewMetaServiceServer(ctx context.Context,
	serviceOptions options.MetaServiceOptions) metaservice.MetaServiceServer {
	return &metaServer{
		ctx:            ctx,
		serviceOptions: serviceOptions,
	}
}

func (server *metaServer) Backup(metaservice.MetaService_BackupServer) error {
	log.Info("Backup Called")
	return nil
}

func (server *metaServer) Restore(*metaservice.RestoreRequest,
	metaservice.MetaService_RestoreServer) error {
	log.Info("Restore Called")
	return nil
}
