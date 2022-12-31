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

// Package server defines NFS Provider server service
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	nfsprovider "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/providers/nfs/server/options"
	"github.com/soda-cdm/kahu/utils"
	logOptions "github.com/soda-cdm/kahu/utils/logoptions"
)

const (
	// NFSService component name
	componentNFSService = "nfsprovider"
	columnWrapSize      = 2
)

// NewNFSProviderCommand creates a *cobra.Command object with default parameters
func NewNFSProviderCommand() *cobra.Command {
	cleanFlagSet := pflag.NewFlagSet(componentNFSService, pflag.ContinueOnError)

	nfsServiceFlags := options.NewNFSServiceFlags()
	loggingOptions := logOptions.NewLogOptions()

	cmd := &cobra.Command{
		Use:  componentNFSService,
		Long: `The NFS Provider server`,
		// Disabled flag parsing from cobra framework
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			// validate flags
			if err := validateFlags(cmd, cleanFlagSet, args); err != nil {
				return
			}

			// validate and apply initial NFSService Flags
			if err := nfsServiceFlags.Apply(); err != nil {
				log.Error("Failed to validate nfs provider service flags ", err)
				return
			}

			// validate and apply logging flags
			if err := loggingOptions.Apply(); err != nil {
				log.Error("Failed to apply logging flags ", err)
				return
			}

			ctx, cancel := context.WithCancel(context.Background())
			// setup signal handler
			utils.SetupSignalHandler(cancel)

			// run the meta service
			if err := Run(ctx, options.NFSProviderOptions{NFSServiceFlags: *nfsServiceFlags}); err != nil {
				log.Error("Failed to run nfs provider service", err)
				return
			}

		},
	}

	nfsServiceFlags.AddFlags(cleanFlagSet)
	// add logging flags
	loggingOptions.AddFlags(cleanFlagSet)
	cleanFlagSet.BoolP("help", "h", false, fmt.Sprintf("help for %s", cmd.Name()))

	const usageFmt = "Usage:\n  %s\n\nFlags:\n%s"
	cmd.SetUsageFunc(func(cmd *cobra.Command) error {
		fmt.Fprintf(cmd.OutOrStderr(), usageFmt, cmd.UseLine(),
			cleanFlagSet.FlagUsagesWrapped(columnWrapSize))
		return nil
	})
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n"+usageFmt, cmd.Long, cmd.UseLine(),
			cleanFlagSet.FlagUsagesWrapped(columnWrapSize))
	})

	return cmd
}

// validateFlags validates nfs provider arguments
func validateFlags(cmd *cobra.Command, cleanFlagSet *pflag.FlagSet, args []string) error {
	// initial flag parse, since we disable cobra's flag parsing
	if err := cleanFlagSet.Parse(args); err != nil {
		log.Error("Failed to parse nfs provider service flag ", err)
		_ = cmd.Usage()
		return err
	}

	// check if there are non-flag arguments in the command line
	cmds := cleanFlagSet.Args()
	if len(cmds) > 0 {
		log.Error("unknown command ", cmds[0])
		_ = cmd.Usage()
		return errors.New("unknown command")
	}

	// short-circuit on help
	help, err := cleanFlagSet.GetBool("help")
	if err != nil {
		log.Error(`"help" flag is non-bool`)
		return err
	}
	if help {
		_ = cmd.Help()
		return errors.New("help command executed, exiting")
	}
	return nil
}

// Run start and run NFS Provider service
func Run(ctx context.Context, serviceOptions options.NFSProviderOptions) error {
	log.Info("Starting Server ...")

	serverAddr, err := net.ResolveUnixAddr("unix", serviceOptions.UnixSocketPath)
	if err != nil {
		log.Fatal("failed to resolve unix addr")
		return errors.New("failed to resolve unix addr")
	}
	if _, err := os.Stat(serviceOptions.UnixSocketPath); err == nil {
		if err := os.RemoveAll(serviceOptions.UnixSocketPath); err != nil {
			log.Fatal(err)
			return err
		}
	}
	lis, err := net.ListenUnix("unix", serverAddr)
	if err != nil {
		log.Fatal("failed to listen: ", err)
		return err
	}
	defer func() {
		err := lis.Close()
		if err != nil {
			log.Errorf("failed to close listening socket %s", err)
		}
	}()

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	nfsprovider.RegisterMetaBackupServer(grpcServer, NewMetaBackupServer(ctx, serviceOptions))
	nfsprovider.RegisterIdentityServer(grpcServer, NewIdentityServer(ctx, serviceOptions))
	go func(ctx context.Context, server *grpc.Server) {
		<-ctx.Done()
		server.Stop()
	}(ctx, grpcServer)

	return grpcServer.Serve(lis)
}
