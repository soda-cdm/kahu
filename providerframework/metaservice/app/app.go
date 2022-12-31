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
	"encoding/json"
	"fmt"
	"net"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1 "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahuClient "github.com/soda-cdm/kahu/client"
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
	serviceAnnotation    = "kahu.io/provider-service"
	defaultProbeTimeout  = 30 * time.Second
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
			// validate flags
			if err := validateFlags(cmd, cleanFlagSet, args); err != nil {
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

// validateFlags validates metadata service arguments
func validateFlags(cmd *cobra.Command, cleanFlagSet *pflag.FlagSet, args []string) error {
	// initial flag parse, since we disable cobra's flag parsing
	if err := cleanFlagSet.Parse(args); err != nil {
		log.Error("Failed to parse metadata service flag ", err)
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

	err = utils.Probe(grpcConn, defaultProbeTimeout)
	if err != nil {
		log.Errorf("Unable to probe metadata provider. %s: ", err)
		return err
	}
	log.Infoln("Probe completed with provider")

	// Register the metadata provider
	provider, err := providerIdentity.RegisterMetadataProvider(ctx, &grpcConn)
	if err != nil {
		log.Error("Failed to get provider info: ", err)
		return err
	}
	serviceAddress := serviceOptions.ServiceName + "." + serviceOptions.DeployedNamespace + ":" +
		fmt.Sprintf("%d", serviceOptions.Port)
	err = annotateProvider(serviceAnnotation, serviceAddress, provider)
	if err != nil {
		log.Error("Failed to add annotation to provider info: ", err)
		return err
	}

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	metaservice.RegisterMetaServiceServer(grpcServer, server.NewMetaServiceServer(ctx,
		serviceOptions, repository))

	go func(ctx context.Context, server *grpc.Server) {
		server.Serve(lis)
	}(ctx, grpcServer)

	<-ctx.Done()

	return cleanup(grpcServer)
}

func cleanup(server *grpc.Server) error {
	server.Stop()
	return nil
}

func annotateProvider(
	annotation, value string,
	provider *v1.Provider) error {
	providerName := provider.Name

	_, ok := provider.Annotations[annotation]
	if ok {
		log.Infof("Provider(%s) all-ready annotated with %s", providerName, annotation)
		log.Infof("Provider(%s) overriding annotation with %s", providerName, annotation)
	}

	providerClone := provider.DeepCopy()
	metav1.SetMetaDataAnnotation(&providerClone.ObjectMeta, annotation, value)
	return patchProvider(provider, providerClone)
}

func patchProvider(
	provider *v1.Provider, providerClone *v1.Provider) error {
	providerName := provider.Name

	origBytes, err := json.Marshal(provider)
	if err != nil {
		return errors.Wrap(err, "error marshalling backup")
	}

	updatedBytes, err := json.Marshal(providerClone)
	if err != nil {
		return errors.Wrap(err, "error marshalling updated backup")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return errors.Wrap(err, "error creating json merge patch for backup")
	}

	cfg := kahuClient.NewFactoryConfig()
	clientFactory := kahuClient.NewFactory(componentMetaService, cfg)
	client, err := clientFactory.KahuClient()
	if err != nil {
		return err
	}

	provider, err = client.KahuV1beta1().Providers().Patch(context.TODO(), providerName,
		types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		log.Errorf("Unable to update provider(%s) for service annotation. %s",
			providerName, err)
		return errors.Wrap(err, "error annotating volume backup completeness")
	}

	log.Infof("Annotation updated for the provider:%s", providerName)
	return nil
}
