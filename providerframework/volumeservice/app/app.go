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
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	"github.com/soda-cdm/kahu/providerframework/volumeservice/app/config"
	"github.com/soda-cdm/kahu/providerframework/volumeservice/app/options"
	volumeservice "github.com/soda-cdm/kahu/providerframework/volumeservice/lib/go"
	"github.com/soda-cdm/kahu/providerframework/volumeservice/server"
	providerSvc "github.com/soda-cdm/kahu/providers/lib/go"
	"github.com/soda-cdm/kahu/utils"
)

const (
	// Controllers component name
	service                    = "volume-service"
	defaultLeaderLockNamespace = "kube-system"
	defaultProbeTimeout        = 30 * time.Second
	eventComponentName         = "Kahu-Volume-Service"
	leaderLockObjectName       = "vs-"
)

// NewServiceCommand creates a *cobra.Command object with default parameters
func NewServiceCommand() *cobra.Command {
	cleanFlagSet := pflag.NewFlagSet(service, pflag.ContinueOnError)

	optManager, err := options.NewOptionsManager()
	if err != nil {
		log.Fatalf("Failed to initialize volume service option manager")
	}

	cmd := &cobra.Command{
		Use: service,
		Long: `The Volume-Backup-Service is a set of controllers which watches the VolumeBackupContent 
and VolumeRestoreContent state on the cluster through the apiserver and makes attempts to move the current 
state towards the desired state`,
		// Disabled flag parsing from cobra framework
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			validateFlags(cleanFlagSet, cmd, args)
			// validate, apply flags and get controller manager configuration
			completeConfig := getConfig(optManager)
			ctx, cancel := context.WithCancel(context.Background())
			// setup signal handler
			setupSignalHandler(cancel)

			// connect with driver and get provider info
			grpcConn, err := utils.GetGRPCConnection(completeConfig.DriverEndpoint)
			if err != nil {
				log.Fatalf("Failed to connect to volume backup driver. %s", err)
				return
			}
			defer grpcConn.Close()

			err = ensureDriver(ctx, completeConfig, grpcConn)
			if err != nil {
				log.Errorf("unable to configure volume driver. %s", err)
				return
			}

			if err := Run(ctx, completeConfig, grpcConn); err != nil {
				log.Fatalf("Controller manager exited. %s", err)
			}
		},
	}

	// add flags
	optManager.AddFlags(cleanFlagSet)

	return setCmdHelp(cleanFlagSet, cmd)
}

func getConfig(optManager options.OptionManager) *config.CompletedConfig {
	cfg, err := optManager.Config()
	if err != nil {
		log.Errorf("Failed to get configuration %s", err)
		os.Exit(1)
	}

	completeConfig, err := cfg.Complete()
	if err != nil {
		log.Errorf("Failed to complete configuration %s", err)
		os.Exit(1)
	}

	log.Info("Volume service build information ")
	utils.GetBuildInfo().Print()

	return completeConfig
}

func ensureDriver(ctx context.Context,
	completeConfig *config.CompletedConfig, grpcConn grpc.ClientConnInterface) error {
	// probe driver till get ready
	err := utils.Probe(grpcConn, defaultProbeTimeout)
	if err != nil {
		log.Errorf("Unable to probe volume backup driver. %s", err)
		return err
	}

	// get provider info
	identityClient := providerSvc.NewIdentityClient(grpcConn)
	providerInfo, err := identityClient.GetProviderInfo(context.Background(),
		&providerSvc.GetProviderInfoRequest{})
	if err != nil {
		log.Errorf("Unable to retrieve volume backup provider info. %s", err)
		return err
	}

	// get volume backup driver
	providerName := providerInfo.GetProvider()
	// add provider info in config
	completeConfig.Provider = providerName
	completeConfig.Version = providerInfo.GetVersion()
	completeConfig.Manifest = providerInfo.GetManifest()

	return nil
}

func validateFlags(
	cleanFlagSet *pflag.FlagSet,
	cmd *cobra.Command,
	args []string) {
	// initial flag parse, since we disable cobra's flag parsing
	if err := cleanFlagSet.Parse(args); err != nil {
		log.Error("Failed to parse volume service service flag ", err)
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
		os.Exit(0)
	}
}

func setCmdHelp(
	cleanFlagSet *pflag.FlagSet,
	cmd *cobra.Command) *cobra.Command {
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

func Run(ctx context.Context, config *config.CompletedConfig, grpcConn *grpc.ClientConn) error {
	log.Info("Volume service started with configuration")
	config.Print()

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d",
		config.Address, config.Port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	volumeservice.RegisterVolumeServiceServer(grpcServer, server.NewServer(config,
		providerSvc.NewVolumeBackupClient(grpcConn),
		providerSvc.NewIdentityClient(grpcConn)))

	go func(ctx context.Context, server *grpc.Server) {
		server.Serve(lis)
	}(ctx, grpcServer)

	<-ctx.Done()

	return cleanup(grpcServer, grpcConn)
}

func cleanup(server *grpc.Server, repoClient *grpc.ClientConn) error {
	server.Stop()
	repoClient.Close()
	return nil
}

func setupSignalHandler(cancel context.CancelFunc) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		log.Infof("Received signal %s, shutting down", sig)
		cancel()
	}()
}
