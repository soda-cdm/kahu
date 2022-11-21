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

package deployment

import (
	"context"
	"os"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"
	"github.com/soda-cdm/kahu/client"
	"github.com/soda-cdm/kahu/controllers/app/options"
	k8s "github.com/soda-cdm/kahu/test/e2e/util/k8s"
	kahu "github.com/soda-cdm/kahu/test/e2e/util/kahu"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	AgentBaseName = "controller-manager"
)

//testcase for E2E deployment backup and restore
var _ = Describe("DeploymentBackup", Label("deployment"), func() {
	Describe("deploymentE2ETest", func() {
		Context("Create backup of deployment and restore", func() {
			It("deployment with replicas and pods", func() {

				optManager, err := options.NewOptionsManager()
				Expect(err).To(BeNil())
				if err != nil {
					log.Fatalf("Failed to initialize controller option manager")
				}
				cfg, err := optManager.Config()
				Expect(err).To(BeNil())
				if err != nil {
					log.Errorf("Failed to get configuration %s", err)
					os.Exit(1)
				}

				f := client.NewFactory(AgentBaseName, &cfg.KahuClientConfig)
				kubeClient, err := f.KubeClient()
				kahuClient, err := f.KahuClient()

				//Create deployment to test
				ns := "default"
				labels := make(map[string]string)
				labels["deployment"] = "deployment"
				image := "nginx"
				UUIDgen, _ := uuid.NewRandom()
				name := "deployment" + "-" + UUIDgen.String()
				replicas := 2

				deployment := k8s.NewDeployment(name, ns, int32(replicas), labels, image)
				log.Infof("deployment:%v\n", deployment)
				deployment1, err := k8s.CreateDeployment(kubeClient, ns, deployment)
				log.Infof("deployment1:%v,err:%v\n", deployment1, err)
				Expect(err).To(BeNil())
				err = k8s.WaitForReadyDeployment(kubeClient, ns, name)
				Expect(err).To(BeNil())

				//create backup for the deployment
				backupName := "backup" + "-" + UUIDgen.String()
				includeNs := "default"
				backup := kahu.NewBackup(backupName, includeNs, "Deployment")
				opts := metav1.CreateOptions{}
				ctx := context.TODO()
				_, err = kahuClient.KahuV1beta1().Backups().Create(ctx, backup, opts)
				Expect(err).To(BeNil())
				log.Infof("backup is done\n")
				time.Sleep(40 * time.Second)

				// create restore for the backup
				restoreName := "restore" + "-" + UUIDgen.String()
				nsRestore := "restore-nsdemo"
				restore := kahu.NewRestore(restoreName, nsRestore, includeNs, backupName)
				log.Infof("restore is %v\n", restore)
				restore1, err := kahuClient.KahuV1beta1().Restores().Create(ctx, restore, opts)
				log.Infof("restore1 is %v\n", restore1)
				Expect(err).To(BeNil())
				log.Infof("restore is created\n")

				time.Sleep(40 * time.Second)

				//check if the restored deployment is up
				deployment, err = k8s.GetDeployment(kubeClient, nsRestore, name)
				log.Infof("deployment is %v\n", deployment)
				Expect(err).To(BeNil())
				err = k8s.WaitForReadyDeployment(kubeClient, nsRestore, name)
				Expect(err).To(BeNil())
				log.Infof("deployment backedup is up\n")

				//Delete the. restore
				optsDel := metav1.DeleteOptions{}
				err = kahuClient.KahuV1beta1().Restores().Delete(ctx, restoreName, optsDel)
				Expect(err).To(BeNil())

				//Delete the backup
				err = kahuClient.KahuV1beta1().Backups().Delete(ctx, backupName, optsDel)
				Expect(err).To(BeNil())

				//Delete deployment created
				err = k8s.DeleteDeployment(kubeClient, name, ns)

				//Delete restored deployment
				err = k8s.DeleteDeployment(kubeClient, name, nsRestore)
			})
		})
	})
})

//TODO need to create namespace include steps
