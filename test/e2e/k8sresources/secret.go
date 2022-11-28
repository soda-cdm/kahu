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

package k8sresources

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"

	k8s "github.com/soda-cdm/kahu/test/e2e/util/k8s"
	kahu "github.com/soda-cdm/kahu/test/e2e/util/kahu"
)

//testcase for E2E secret backup and restore
var _ = Describe("SecretBackup", Label("secret"), func() {
	Context("Create backup of secret and restore", func() {
		It("secret with replicas and pods", func() {

			kubeClient, kahuClient := kahu.Clients()
			//Create secret to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["secret"] = "secret"
			UUIDgen, err := uuid.NewRandom()
			Expect(err).To(BeNil())
			name := "secret" + "-" + UUIDgen.String()

			secret, err := k8s.CreateSecret(kubeClient, ns, name, labels)
			log.Infof("secret:%v,err:%v\n", secret, err)
			Expect(err).To(BeNil())
			err = k8s.WaitForSecretsComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the secret
			backupName := "backup" + "secret" + "-" + UUIDgen.String()
			includeNs := kahu.BackupNameSpace
			resourceType := "Secret"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of secret is done\n")

			// create restore for the backup
			restoreName := "restore" + "secret" + "-" + UUIDgen.String()
			nsRestore := "restore-nsdemo"
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore1 is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore is created\n")

			//check if the restored secret is up
			secret, err = k8s.GetSecret(kubeClient, nsRestore, name)
			log.Infof("secret is %v\n", secret)
			Expect(err).To(BeNil())

			err = k8s.WaitForSecretsComplete(kubeClient, nsRestore, name)
			Expect(err).To(BeNil())
			log.Infof("secret restored is up\n")

			//Delete the. restore
			err = kahu.DeleteRestore(kahuClient, restoreName)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreDelete(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of %v is deleted\n", name)

			//Delete the backup
			err = kahu.DeleteBackup(kahuClient, backupName)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupDelete(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of  %v is deleted\n", name)
		})
	})
})
