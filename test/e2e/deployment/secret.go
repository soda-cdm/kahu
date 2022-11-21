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

//testcase for E2E secret backup and restore
var _ = Describe("SecretBackup", Label("secret"), func() {
	Describe("secretE2ETest", func() {
		Context("Create backup of secret and restore", func() {
			It("secret with replicas and pods", func() {

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

				//Create secret to test
				ns := "default"
				labels := make(map[string]string)
				labels["secret"] = "secret"
				UUIDgen, _ := uuid.NewRandom()
				name := "secret" + "-" + UUIDgen.String()
				secret, err := k8s.CreateSecret(kubeClient, ns, name, labels)
				log.Infof("secret:%v,err:%v\n", secret, err)
				Expect(err).To(BeNil())
				err = k8s.WaitForSecretsComplete(kubeClient, ns, name)
				Expect(err).To(BeNil())

				//create backup for the secret
				backupName := "backup" + "secret" + "-" + UUIDgen.String()
				includeNs := "default"
				backup := kahu.NewBackup(backupName, includeNs, "Secret")
				opts := metav1.CreateOptions{}
				ctx := context.TODO()
				_, err = kahuClient.KahuV1beta1().Backups().Create(ctx, backup, opts)
				Expect(err).To(BeNil())
				log.Infof("backup is done\n")
				time.Sleep(40 * time.Second)

				// create restore for the backup
				restoreName := "restore" + "secret" + "-" + UUIDgen.String()
				nsRestore := "restore-nsdemo"
				restore := kahu.NewRestore(restoreName, nsRestore, includeNs, backupName)
				log.Infof("restore is %v\n", restore)
				restore1, err := kahuClient.KahuV1beta1().Restores().Create(ctx, restore, opts)
				log.Infof("restore1 is %v\n", restore1)
				Expect(err).To(BeNil())
				log.Infof("restore is created\n")

				time.Sleep(40 * time.Second)

				//check if the restored secret is up
				secret, err = k8s.GetSecret(kubeClient, nsRestore, name)
				log.Infof("secret is %v\n", secret)
				Expect(err).To(BeNil())
				err = k8s.WaitForSecretsComplete(kubeClient, nsRestore, name)
				Expect(err).To(BeNil())
				log.Infof("secret backedup is up\n")

				//Delete the. restore
				optsDel := metav1.DeleteOptions{}
				err = kahuClient.KahuV1beta1().Restores().Delete(ctx, restoreName, optsDel)
				Expect(err).To(BeNil())

				//Delete the backup
				err = kahuClient.KahuV1beta1().Backups().Delete(ctx, backupName, optsDel)
				Expect(err).To(BeNil())

				//Delete secret created
				err = k8s.WaitForSecretDelete(kubeClient, name, ns)

				//Delete restored secret
				err = k8s.WaitForSecretDelete(kubeClient, name, nsRestore)
			})
		})
	})
})
