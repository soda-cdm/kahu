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
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/soda-cdm/kahu/controllers/app/config"
	"github.com/soda-cdm/kahu/controllers/app/options"
	"github.com/soda-cdm/kahu/controllers/manager"
	"github.com/soda-cdm/kahu/utils"
)

const (
	// Controllers component name
	controllerManagerService    = "controller-manager"
	defaultLeaderLockObjectName = "kahu-controller-manager"
	defaultLeaderLockNamespace  = "kube-system"
)

// NewControllersCommand creates a *cobra.Command object with default parameters
func NewControllersCommand() *cobra.Command {
	cleanFlagSet := pflag.NewFlagSet(controllerManagerService, pflag.ContinueOnError)

	optManager, err := options.NewOptionsManager()
	if err != nil {
		log.Fatalf("Failed to initialize controller option manager")
	}

	cmd := &cobra.Command{
		Use: controllerManagerService,
		Long: `The Controller-Manager is set of controllers which watches the kahu state of the cluster 
through the apiserver and makes attempts to move the current state towards the desired state`,
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

			// validate, apply flags and get controller manager configuration
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

			ctx, cancel := context.WithCancel(context.Background())
			// setup signal handler
			setupSignalHandler(cancel)

			if !completeConfig.EnableLeaderElection {
				// run the controller manager
				if err := Run(ctx, completeConfig); err != nil {
					log.Errorf("Controller manager exited. %s", err)
					os.Exit(1)
				}
				return
			}

			id, err := os.Hostname()
			if err != nil {
				log.Fatalf("Error getting hostname: %v", err)
			}

			eventRecorder := completeConfig.EventBroadcaster.NewRecorder(scheme.Scheme,
				v1.EventSource{Component: "kahu-controller-manager-lease"})

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
				defaultLeaderLockObjectName,
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
						if err := Run(ctx, completeConfig); err != nil {
							log.Fatalf("Controller manager exited. %s", err)
						}
					},
					OnStoppedLeading: func() {
						log.Fatalf("Controller manager lost master")
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
				os.Exit(1)
			}
			leaderElector.Run(ctx)
		},
	}

	// add flags
	optManager.AddFlags(cleanFlagSet)
	// add help flag
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

func Run(ctx context.Context, config *config.CompletedConfig) error {
	log.Infof("Starting controller manager with build info %s", utils.GetBuildInfo())
	config.Print()

	// init discovery helper
	initDiscoveryHelper(ctx, config)

	// init controller manager
	controllerManager, err := manager.NewControllerManager(ctx,
		config,
		config.ClientFactory,
		config.KahuInformer)
	if err != nil {
		return err
	}

	// initialize all controllers
	controllerMap, err := controllerManager.InitControllers()
	if err != nil {
		return err
	}

	// remove disabled controllers
	err = controllerManager.RemoveDisabledControllers(controllerMap)
	if err != nil {
		return err
	}

	// start the informers & and wait for the caches to sync
	config.KahuInformer.Start(ctx.Done())
	log.Info("Waiting for informer to sync")
	cacheSyncResults := config.KahuInformer.WaitForCacheSync(ctx.Done())
	log.Info("Done waiting for informer to sync")

	for informer, synced := range cacheSyncResults {
		if !synced {
			return errors.Errorf("cache was not synced for informer %v", informer)
		}
		log.WithField("resource", informer).Info("Informer cache synced")
	}

	return controllerManager.RunControllers(controllerMap)
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

// initDiscoveryHelper instantiates kubernetes APIs discovery helper and refresh itself
func initDiscoveryHelper(ctx context.Context, config *config.CompletedConfig) {
	discoveryHelper := config.DiscoveryHelper

	go wait.Until(
		func() {
			if err := discoveryHelper.Refresh(); err != nil {
				log.WithError(err).Error("Error refreshing discovery")
			}
		},
		5*time.Minute,
		ctx.Done(),
	)
}
