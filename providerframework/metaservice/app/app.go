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

package app

import (
	"context"
	"fmt"
	"net"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	providerIdentity "github.com/soda-cdm/kahu/providerframework/identityservice"
	"github.com/soda-cdm/kahu/providerframework/metaservice/app/options"
	repo "github.com/soda-cdm/kahu/providerframework/metaservice/backuprespository"
	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"github.com/soda-cdm/kahu/providerframework/metaservice/server"
	"github.com/soda-cdm/kahu/utils"
	logOptions "github.com/soda-cdm/kahu/utils/logoptions"
)

const (
	// MetaService component name
	componentMetaService = "metaservice"
)

// NewMetaServiceCommand creates a *cobra.Command object with default parameters
func NewMetaServiceCommand() *cobra.Command {
	cleanFlagSet := pflag.NewFlagSet(componentMetaService, pflag.ContinueOnError)

	metaServiceFlags := options.NewMetaServiceFlags()
	loggingOptions := logOptions.NewLogOptions()

	cmd := &cobra.Command{
		Use:  componentMetaService,
		Long: `The MetaService is metadata backup agent`,
		// Disabled flag parsing from cobra framework
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			// initial flag parse, since we disable cobra's flag parsing
			if err := cleanFlagSet.Parse(args); err != nil {
				log.Error("Failed to parse meta service flag ", err)
				_ = cmd.Usage()
				os.Exit(1)
			}

			// check if there are non-flag arguments in the command line
			cmds := cleanFlagSet.Args()
			if len(cmds) > 0 {
				log.Error("Unknown command ", cmds[0])
				_ = cmd.Usage()
				return
			}

			// short-circuit on help
			help, err := cleanFlagSet.GetBool("help")
			if err != nil {
				log.Error(`"help" flag is non-bool`)
				return
			}
			if help {
				_ = cmd.Help()
				return
			}

			// validate and apply initial MetaService Flags
			if err := metaServiceFlags.Apply(); err != nil {
				log.Error("Failed to validate meta service flags ", err)
				return
			}

			// validate and apply logging flags
			if err := loggingOptions.Apply(); err != nil {
				log.Error("Failed to apply logging flags ", err)
				return
			}

			// TODO: Setup signal handler with context
			ctx, cancel := context.WithCancel(context.Background())
			// setup signal handler
			utils.SetupSignalHandler(cancel)

			// run the meta service
			if err := Run(ctx, options.MetaServiceOptions{MetaServiceFlags: *metaServiceFlags}); err != nil {
				log.Error("Failed to run meta service", err)
			}

			return
		},
	}

	metaServiceFlags.AddFlags(cleanFlagSet)
	// add logging flags
	loggingOptions.AddFlags(cleanFlagSet)
	cleanFlagSet.BoolP("help", "h", false, fmt.Sprintf("help for %s", cmd.Name()))

	const usageFmt = "Usage:\n  %s\n\nFlags:\n%s"
	cmd.SetUsageFunc(func(cmd *cobra.Command) error {
		fmt.Fprintf(cmd.OutOrStderr(), usageFmt, cmd.UseLine(), cleanFlagSet.FlagUsagesWrapped(2))
		return nil
	})
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n"+usageFmt, cmd.Long, cmd.UseLine(),
			cleanFlagSet.FlagUsagesWrapped(2))
	})

	return cmd
}

func Run(ctx context.Context, serviceOptions options.MetaServiceOptions) error {
	log.Info("Starting Server ...")
	log.Infof("Configuration %+v", serviceOptions)

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d",
		serviceOptions.Address, serviceOptions.Port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// initialize backup repository
	repository, grpcConn, err := repo.NewBackupRepository(serviceOptions.BackupDriverAddress)
	if err != nil {
		return err
	}

	// Register the metadata provider
	err = providerIdentity.RegisterMetadataProvider(ctx, &grpcConn)
	if err != nil {
		log.Error("Failed to get provider info: ", err)
		return err
	}

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	metaservice.RegisterMetaServiceServer(grpcServer, server.NewMetaServiceServer(ctx,
		serviceOptions, repository))

	go func(ctx context.Context, server *grpc.Server) {
		<-ctx.Done()
		server.Stop()
	}(ctx, grpcServer)

	return grpcServer.Serve(lis)
}
