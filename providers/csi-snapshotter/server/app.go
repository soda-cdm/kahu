/*
Copyright 2023 The SODA Authors.

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

// Package server defines csi snapshotter provider server service
package server

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

	"github.com/soda-cdm/kahu/providers/csi-snapshotter/server/options"
	provider "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/utils"
	logOptions "github.com/soda-cdm/kahu/utils/logoptions"
)

const (
	// CSISnapshotterProvider component name
	componentCSISnapshotterProvider = "CSISnapshotter"
	columnWrapSize                  = 2
)

// NewCSISnapshotterProviderCommand creates a *cobra.Command object with default parameters
func NewCSISnapshotterProviderCommand() *cobra.Command {
	cleanFlagSet := pflag.NewFlagSet(componentCSISnapshotterProvider, pflag.ContinueOnError)

	lvmServiceFlags := options.NewCSISnapshotterProviderFlags()
	loggingOptions := logOptions.NewLogOptions()
	kubeconfigFlags := options.NewKahuClientOptions()

	cmd := &cobra.Command{
		Use:  componentCSISnapshotterProvider,
		Long: `The Volume backup provider using snapshot`,
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

			// validate and apply initial csi snapshotter provider Flags
			if err := lvmServiceFlags.Apply(); err != nil {
				log.Error("Failed to validate provider service flags ", err)
				return
			}

			// validate and apply initial csi snapshotter provider Flags
			if err := kubeconfigFlags.Apply(); err != nil {
				log.Error("Failed to validate kube config flags ", err)
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
			if err := Run(ctx, options.CSISnapshotterProviderOptions{CSISnapshotterProviderFlags: *lvmServiceFlags,
				KubeClientOptions: *kubeconfigFlags}); err != nil {
				log.Error("Failed to run LVM provider service", err)
				return
			}

		},
	}

	lvmServiceFlags.AddFlags(cleanFlagSet)
	kubeconfigFlags.AddFlags(cleanFlagSet)
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

// validateFlags validates LVM provider arguments
func validateFlags(cmd *cobra.Command, cleanFlagSet *pflag.FlagSet, args []string) error {
	// initial flag parse, since we disable cobra's flag parsing
	if err := cleanFlagSet.Parse(args); err != nil {
		log.Error("Failed to parse LVM provider service flag ", err)
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

// Run start and run csi snapshotter provider service
func Run(ctx context.Context, serviceOptions options.CSISnapshotterProviderOptions) error {
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
	backupServer, err := NewVolumeBackupServer(ctx, serviceOptions)
	if err != nil {
		log.Errorf("Unable to initialize backup server %s", err)
		return err
	}

	provider.RegisterVolumeBackupServer(grpcServer, backupServer)
	provider.RegisterIdentityServer(grpcServer, NewIdentityServer(ctx, serviceOptions))

	go func(ctx context.Context, server *grpc.Server) {
		<-ctx.Done()
		server.Stop()
	}(ctx, grpcServer)

	return grpcServer.Serve(lis)
}
