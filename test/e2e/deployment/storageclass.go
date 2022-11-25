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
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"
	k8s "github.com/soda-cdm/kahu/test/e2e/util/k8s"
	kahu "github.com/soda-cdm/kahu/test/e2e/util/kahu"
)

//testcase for E2E deployment backup and restore
var _ = Describe("storageClassBackup", Label("storageClass"), func() {
	Context("Create backup of storageClass and restore", func() {
		It("storageClass", func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create storageClass to test
			UUIDgen, err := uuid.NewRandom()
			Expect(err).To(BeNil())
			name := "storageclass" + "-" + UUIDgen.String()

			storageClass, err := k8s.CreateStorageClass(kubeClient, name)
			log.Infof("storageClass:%v\n", storageClass)
			Expect(err).To(BeNil())
			err = k8s.WaitUntilStorageClassCreated(kubeClient, name)
			Expect(err).To(BeNil())

			//create backup for the storageClass
			backupName := "backup" + "storageclass" + "-" + UUIDgen.String()
			includeNs := kahu.BackupNameSpace
			resourceType := "StorageClass"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of storageClass is done\n")

			// create restore for the backup
			restoreName := "restore" + "storageclass" + "-" + UUIDgen.String()
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore1 is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of storageClass is created\n")

			//check if the restored deployment is up
			storageClass, err = k8s.GetStorageClass(kubeClient, name)
			log.Debugf("storageClass is %v\n", storageClass)
			Expect(err).To(BeNil())
			err = k8s.WaitUntilStorageClassCreated(kubeClient, name)
			Expect(err).To(BeNil())
			log.Infof("storageClass restored is up\n")

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
