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
var _ = Describe("rbacBackup", Label("rbac"), func() {
	Context("Create backup of rbac and restore", func() {
		It("rbac", Label("testcase17"), func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create rbac to test
			ns := kahu.BackupNameSpace
			ctx := context.TODO()
			UUIDgen, err := uuid.NewRandom()
			Expect(err).To(BeNil())
			clusterRolename := "clusterrole" + "-" + UUIDgen.String()
			serviceAccount := "serviceaccount" + "-" + UUIDgen.String()
			clusterrolebinding := "clusterrolebinding" + "-" + UUIDgen.String()
			podName := UUIDgen.String() + "-" + "pod"

			//create serviceAccount
			err = k8s.CreateServiceAccount(ctx, kubeClient, ns, serviceAccount)
			Expect(err).To(BeNil())

			//create pod with the serviceAccount
			pod, err := k8s.CreatePodWithServiceAccount(kubeClient, ns, podName, serviceAccount)
			log.Infof("pod with sa is %v\n", pod)
			Expect(err).To(BeNil())

			err = k8s.CreateRBACWithBindingSA(ctx, kubeClient, ns, serviceAccount, clusterRolename, clusterrolebinding)
			Expect(err).To(BeNil())

			_, err = k8s.GetClusterRoleBinding(ctx, kubeClient, clusterrolebinding)
			Expect(err).To(BeNil())

			_, err = k8s.GetClusterRole(ctx, kubeClient, clusterRolename)
			Expect(err).To(BeNil())

			//create backup for the rbac
			backupName := "backup" + "rbac" + "-" + UUIDgen.String()
			includeNs := kahu.BackupNameSpace
			resourceType := "Pod"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of rbac is done\n")

			// create restore for the backup
			restoreName := "restore" + "rbac" + "-" + UUIDgen.String()
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore1 is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore is created\n")

			//check if the restored rbac is up
			err = k8s.WaitForPodComplete(kubeClient, nsRestore, podName)
			Expect(err).To(BeNil())

			//Delete the. restore
			err = kahu.DeleteRestore(kahuClient, restoreName)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreDelete(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore %v is deleted\n", restoreName)

			//Delete the backup
			err = kahu.DeleteBackup(kahuClient, backupName)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupDelete(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup %v is deleted\n", backupName)
		})
	})
})
