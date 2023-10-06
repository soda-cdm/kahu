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

//testcase for E2E service backup and restore
var _ = Describe("ServiceBackup", Label("service"), func() {
	Context("Create backup of service and restore", func() {
		It("service", Label("testcase20"), func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create service to test
			ns := kahu.BackupNameSpace
			UUIDgen, err := uuid.NewRandom()
			Expect(err).To(BeNil())
			name := "service" + "-" + UUIDgen.String()
			service, err := k8s.CreateService(kubeClient, ns, name)
			log.Infof("service,err:%v,%v\n", service, err)
			Expect(err).To(BeNil())
			err = k8s.WaitUntilServiceCreated(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the service
			backupName := "backup" + "service" + "-" + UUIDgen.String()
			includeNs := kahu.BackupNameSpace
			resourceType := "Service"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of service is done %v\n", backupName)

			// create restore for the backup
			restoreName := "restore" + "service" + "-" + UUIDgen.String()
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore1 is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())

			//check if the restored service is up
			service, err = k8s.GetService(kubeClient, name, nsRestore)
			log.Debugf("service is %v\n", service)
			Expect(err).To(BeNil())

			err = k8s.WaitUntilServiceCreated(kubeClient, nsRestore, name)
			Expect(err).To(BeNil())
			log.Infof("restored service %v is up\n", name)

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

//TODO need to create namespace include steps
