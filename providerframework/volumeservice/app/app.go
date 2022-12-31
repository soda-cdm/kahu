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
	"github.com/pkg/errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	snapshotclientset "github.com/kubernetes-csi/external-snapshotter/client/v4/clientset/versioned"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	providerIdentity "github.com/soda-cdm/kahu/providerframework/identityservice"
	"github.com/soda-cdm/kahu/providerframework/volumeservice/app/config"
	"github.com/soda-cdm/kahu/providerframework/volumeservice/app/options"
	"github.com/soda-cdm/kahu/providerframework/volumeservice/backup"
	"github.com/soda-cdm/kahu/providerframework/volumeservice/restore"
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

			if !completeConfig.EnableLeaderElection {
				// run the controller manager
				if err := Run(ctx, completeConfig, grpcConn); err != nil {
					log.Errorf("Controller manager exited. %s", err)
					os.Exit(1)
				}
				return
			}

			runLeader(ctx, cancel, completeConfig, grpcConn)
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

	log.Infof("Registering volume backup provider for %s", providerName)
	// Register the volume provider
	// TODO (Amit Roushan): Ensure registration and de-registration of Volume provider
	_, err = providerIdentity.RegisterVolumeProvider(ctx, &grpcConn)
	if err != nil {
		log.Error("Failed to get provider info: ", err)
		return err
	}

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

func runLeader(ctx context.Context,
	cancel context.CancelFunc,
	completeConfig *config.CompletedConfig,
	grpcConn grpc.ClientConnInterface) {
	providerName := completeConfig.Provider
	id, err := os.Hostname()
	if err != nil {
		log.Fatalf("Error getting hostname: %v", err)
	}

	eventRecorder := completeConfig.EventBroadcaster.NewRecorder(scheme.Scheme,
		v1.EventSource{Component: fmt.Sprintf("kahu-%s-volume-service-lease", providerName)})

	lockConfig := resourcelock.ResourceLockConfig{
		Identity:      id,
		EventRecorder: eventRecorder,
	}

	if completeConfig.LeaderLockNamespace == "" {
		completeConfig.LeaderLockNamespace = defaultLeaderLockNamespace
	}

	resourceLock, err := resourcelock.New(
		resourcelock.ConfigMapsLeasesResourceLock,
		completeConfig.LeaderLockNamespace,
		leaderLockObjectName+providerName,
		completeConfig.KubeClient.CoreV1(),
		completeConfig.KubeClient.CoordinationV1(),
		lockConfig)
	if err != nil {
		log.Fatalf("Error creating resource lock: %v", err)
	}

	leaderElectionConfig := leaderelection.LeaderElectionConfig{
		Lock:          resourceLock,
		LeaseDuration: completeConfig.LeaderLeaseDuration,
		RenewDeadline: completeConfig.LeaderRenewDeadline,
		RetryPeriod:   completeConfig.LeaderRetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				if err := Run(ctx, completeConfig, grpcConn); err != nil {
					log.Fatalf("Controller manager exited. %s", err)
				}
			},
			OnStoppedLeading: func() {
				log.Fatalf("Volume service lost master")
				cancel()
			},
			OnNewLeader: func(identity string) {
				log.Infof("New leader elected. Current leader %s", identity)
			},
		},
	}
	leaderElector, err := leaderelection.NewLeaderElector(leaderElectionConfig)
	if err != nil {
		log.Fatalf("Error creating leader elector: %v", err)
	}
	leaderElector.Run(ctx)
}

func Run(ctx context.Context, config *config.CompletedConfig, grpcConn grpc.ClientConnInterface) error {
	log.Info("Volume service started with configuration")
	config.Print()

	backupProviderClient := providerSvc.NewVolumeBackupClient(grpcConn)
	scheme := runtime.NewScheme()
	kahuapi.AddToScheme(scheme)

	clientConfig, err := config.ClientFactory.ClientConfig()
	if err != nil {
		return err
	}

	ctrlRuntimeManager, err := controllerruntime.NewManager(clientConfig, controllerruntime.Options{
		Scheme: scheme,
	})
	if err != nil {
		return err
	}

	availableControllers, err := configureControllers(ctx, config, backupProviderClient)
	if err != nil {
		return err
	}

	// start the informers & and wait for the caches to sync
	config.InformerFactory.Start(ctx.Done())
	log.Info("Waiting for informer to sync")
	for informer, synced := range config.InformerFactory.WaitForCacheSync(ctx.Done()) {
		if !synced {
			return fmt.Errorf("cache was not synced for informer %s", informer.Name())
		}
		log.WithField("resource", informer).Info("Informer cache synced")
	}

	for _, ctrl := range availableControllers {
		ctrlRuntimeManager.
			Add(manager.RunnableFunc(func(c controllers.Controller, workers int) func(ctx context.Context) error {
				return func(ctx context.Context) error {
					return c.Run(ctx, config.ControllerWorkers)
				}
			}(ctrl, config.ControllerWorkers)))
	}

	log.Info("Birth cry ")
	return ctrlRuntimeManager.Start(ctx)
}

func configureControllers(ctx context.Context,
	config *config.CompletedConfig,
	backupProviderClient providerSvc.VolumeBackupClient) (map[string]controllers.Controller, error) {

	clientset, err := config.ClientFactory.ClientConfig()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	csiSnapshotClient, err := snapshotclientset.NewForConfig(clientset)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	availableControllers := make(map[string]controllers.Controller, 0)
	// add controllers here, integrate backup controller
	backupController, err := backup.NewController(ctx, config.Provider, config.KahuClient, config.InformerFactory,
		config.EventBroadcaster, backupProviderClient, config.KubeClient, csiSnapshotClient.SnapshotV1())
	if err != nil {
		return availableControllers, fmt.Errorf("failed to initialize volume backup controller. %s", err)
	}
	availableControllers[backupController.Name()] = backupController

	restoreController, err := restore.NewController(ctx, config.Provider, config.KahuClient, config.InformerFactory,
		config.EventBroadcaster, backupProviderClient)
	if err != nil {
		return availableControllers, fmt.Errorf("failed to initialize volume backup controller. %s", err)
	}
	availableControllers[restoreController.Name()] = restoreController

	// remove disabled controllers
	for _, controllerName := range config.DisableControllers {
		if _, ok := availableControllers[controllerName]; ok {
			log.Infof("Disabling controller: %s", controllerName)
			delete(availableControllers, controllerName)
		}
	}

	return availableControllers, nil
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
