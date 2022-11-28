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
	"context"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"

	k8s "github.com/soda-cdm/kahu/test/e2e/util/k8s"
	kahu "github.com/soda-cdm/kahu/test/e2e/util/kahu"
)

//testcase for E2E deployment backup and restore
var _ = Describe("roleBackup", Label("role"), func() {
	Context("Create backup of role and restore", func() {
		It("role", func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create role to test
			ns := kahu.BackupNameSpace
			saNamespace := kahu.BackupNameSpace
			ctx := context.TODO()
			UUIDgen, err := uuid.NewRandom()
			Expect(err).To(BeNil())
			rolename := "role" + "-" + UUIDgen.String()
			serviceAccount := "serviceaccount" + "-" + UUIDgen.String()
			rolebinding := "rolebinding" + "-" + UUIDgen.String()
			podName := "pod" + "-" + UUIDgen.String()

			//create serviceAccount
			err = k8s.CreateServiceAccount(ctx, kubeClient, ns, serviceAccount)
			Expect(err).To(BeNil())

			//create pod with the serviceAccount
			pod, err := k8s.CreatePodWithServiceAccount(kubeClient, ns, podName, serviceAccount)
			log.Infof("pod with sa is %v\n", pod)
			Expect(err).To(BeNil())

			err = k8s.CreateRBACWithBindingSAWithRole(ctx, kubeClient, saNamespace, ns, serviceAccount, rolename, rolebinding)
			Expect(err).To(BeNil())

			//create backup for the role
			backupName := "backup" + "role" + "-" + UUIDgen.String()
			includeNs := kahu.BackupNameSpace
			resourceType := "Pod"
			backup, err := kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup %v is done\n", backup)

			// create restore for the backup
			restoreName := "restore" + "role" + "-" + UUIDgen.String()
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore1 is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore is created\n")

			//check if the restored role is up
			_, err = k8s.GetRole(ctx, kubeClient, rolename, nsRestore)
			Expect(err).To(BeNil())

			err = k8s.WaitForPodComplete(kubeClient, nsRestore, podName)
			Expect(err).To(BeNil())

			_, err = k8s.GetRoleBinding(ctx, kubeClient, rolebinding, nsRestore)
			Expect(err).To(BeNil())
			log.Infof("pod restored is up\n")

			//Delete the restore
			err = kahu.DeleteRestore(kahuClient, restoreName)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreDelete(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of pod %v is deleted\n", podName)

			//Delete the backup
			err = kahu.DeleteBackup(kahuClient, backupName)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupDelete(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of roleBinding %v is deleted\n", podName)

		})
	})
})
