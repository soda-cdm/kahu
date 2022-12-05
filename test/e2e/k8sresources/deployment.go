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

//testcase for E2E deployment backup and restore
var _ = Describe("DeploymentBackup", Label("deployment"), func() {
	Context("Create backup of deployment and restore", func() {
		It("deployment with replicas and pods", func() {

			kubeClient, kahuClient := kahu.Clients()
			//Create deployment to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["deployment"] = "deployment"
			image := "nginx"
			UUIDgen, err := uuid.NewRandom()
			Expect(err).To(BeNil())
			name := "deployment" + "-" + UUIDgen.String()
			replicas := 2

			deployment := k8s.NewDeployment(name, ns, int32(replicas), labels, image)
			log.Infof("deployment:%v\n", deployment)
			deployment1, err := k8s.CreateDeployment(kubeClient, ns, deployment)
			log.Debugf("deployment1:%v,err:%v\n", deployment1, err)
			Expect(err).To(BeNil())
			err = k8s.WaitForReadyDeployment(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the deployment
			backupName := "backup" + "deployment" + "-" + UUIDgen.String()
			includeNs := kahu.BackupNameSpace
			resourceType := "Deployment"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of deployment is done\n")

			// create restore for the backup
			restoreName := "restore" + "-" + UUIDgen.String()
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of deployment :%v is created \n", restore)

			//check if the restored deployment is up
			err = k8s.WaitForReadyDeployment(kubeClient, nsRestore, name)
			Expect(err).To(BeNil())
			log.Infof("deployment restored is up\n")

			//Delete the. restore
			err = kahu.DeleteRestore(kahuClient, restoreName)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreDelete(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of deployment: %v is deleted\n", name)

			//Delete the backup
			err = kahu.DeleteBackup(kahuClient, backupName)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupDelete(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of deployment: %v is deleted\n", name)

		})
	})
})