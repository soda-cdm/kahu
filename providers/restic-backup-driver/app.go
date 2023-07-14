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

// Package restic defines Restic Provider server service
package restic

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	provider "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/providers/restic-backup-driver/config"
	"github.com/soda-cdm/kahu/providers/restic-backup-driver/options"
	"github.com/soda-cdm/kahu/utils"
)

const (
	// ResticService component name
	componentResticService = "ResticProvider"
	columnWrapSize         = 2
)

// NewProviderCommand creates a *cobra.Command object with default parameters
func NewProviderCommand() *cobra.Command {
	cleanFlagSet := pflag.NewFlagSet(componentResticService, pflag.ContinueOnError)

	resticProviderFlags := options.NewOptions()

	cmd := &cobra.Command{
		Use:  componentResticService,
		Long: `The Restic volume backup provider`,
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
				os.Exit(1)
			}

			// short-circuit on help
			help, err := cleanFlagSet.GetBool("help")
			if err != nil {
				log.Error(`"help" flag is non-bool`)
				os.Exit(1)
			}
			if help {
				_ = cmd.Help()
				return
			}

			// validate flags
			if err := validateFlags(cmd, cleanFlagSet, args); err != nil {
				return
			}

			// validate and apply initial ResticService Flags
			if err := resticProviderFlags.ValidateAndApply(); err != nil {
				log.Error("Failed to validate restic provider service flags ", err)
				return
			}

			// create and initialize config
			providerConfig := config.NewConfig(resticProviderFlags)
			if err := providerConfig.Complete(); err != nil {
				log.Error("Failed to initialize provider configurations", err)
				return
			}

			ctx, cancel := context.WithCancel(context.Background())
			// setup signal handler
			utils.SetupSignalHandler(cancel)

			// run the meta service
			if err := Run(ctx, providerConfig); err != nil {
				log.Error("Failed to run Restic provider service", err)
				return
			}

		},
	}

	resticProviderFlags.AddFlags(cleanFlagSet)
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

// validateFlags validates Restic provider arguments
func validateFlags(cmd *cobra.Command, cleanFlagSet *pflag.FlagSet, args []string) error {
	// initial flag parse, since we disable cobra's flag parsing
	if err := cleanFlagSet.Parse(args); err != nil {
		log.Error("Failed to parse Restic provider service flag ", err)
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
		return err
	}
	return nil
}

// Run start and run Restic Provider service
func Run(ctx context.Context, config config.Config) error {
	log.Info("Starting Server ...")

	unixSocketPath := config.GetUnixSocketPath()
	serverAddr, err := net.ResolveUnixAddr("unix", unixSocketPath)
	if err != nil {
		log.Fatal("failed to resolve unix addr")
		return errors.New("failed to resolve unix addr")
	}
	if _, err := os.Stat(unixSocketPath); err == nil {
		if err := os.RemoveAll(unixSocketPath); err != nil {
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
	backupServer, err := NewVolumeBackupServer(ctx, config)
	if err != nil {
		log.Errorf("Unable to initialize backup server %s", err)
		return err
	}

	provider.RegisterVolumeBackupServer(grpcServer, backupServer)
	provider.RegisterIdentityServer(grpcServer, NewIdentityServer(ctx, config))

	go func(ctx context.Context, server *grpc.Server) {
		<-ctx.Done()
		server.Stop()
	}(ctx, grpcServer)

	return grpcServer.Serve(lis)
}
