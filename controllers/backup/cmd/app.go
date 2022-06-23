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
	"fmt"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	kahuClient "github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuInformer "github.com/soda-cdm/kahu/client/informers/externalversions"
	"github.com/soda-cdm/kahu/controllers/backup"
	"github.com/soda-cdm/kahu/controllers/backup/cmd/options"
	"github.com/soda-cdm/kahu/utils"
	logOptions "github.com/soda-cdm/kahu/utils/log"
)

const (
	componentController = "controller"
)

func NewBackupControllerCommand() *cobra.Command {
	cleanFlagSet := pflag.NewFlagSet(componentController, pflag.ContinueOnError)

	backupControllerFlags := options.NewBackupControllerFlags()
	loggingOptions := logOptions.NewLoggingOptions()

	cmd := &cobra.Command{
		Use:  componentController,
		Long: `The Backup Controller agent`,
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

			// validate and apply initial backup controller Flags
			if err := backupControllerFlags.Apply(); err != nil {
				log.Error("Failed to validate meta service flags ", err)
				os.Exit(1)
			}
			log.Infof("Backup Controller flags %s", backupControllerFlags.Print())

			// validate and apply logging flags
			if err := loggingOptions.Apply(); err != nil {
				log.Error("Failed to apply logging flags ", err)
				os.Exit(1)
			}

			config, err := utils.GetConfig(backupControllerFlags.KubeConfig)
			if err != nil {
				log.Errorf("failed to initialize kube config %s", err)
				os.Exit(1)
			}

			klientset, err := kahuClient.NewForConfig(config)
			if err != nil {
				log.Errorf("getting klient set %s\n", err.Error())

			}
			log.Debug("kclintset object:", klientset)

			infoFactory := kahuInformer.NewSharedInformerFactory(klientset, 20*time.Minute)

			ch := make(chan struct{})
			c := backup.NewController(klientset, infoFactory.Kahu().V1beta1().Backups(),
				config, klientset.KahuV1beta1(), klientset.KahuV1beta1(), backupControllerFlags)

			infoFactory.Start(ch)
			if err := c.Run(ch); err != nil {
				log.Errorf("error running controller %s\n", err.Error())
			}
		},
	}

	backupControllerFlags.AddFlags(cleanFlagSet)
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
