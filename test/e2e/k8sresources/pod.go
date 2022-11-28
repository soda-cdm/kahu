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

//testcase for E2E pod backup and restore
var _ = Describe("PodBackup", Label("pod"), func() {
	Context("Create backup of pod and restore", func() {
		It("pod with replicas", func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create pod to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["pod"] = "pod"
			UUIDgen, err := uuid.NewRandom()
			Expect(err).To(BeNil())
			name := "pod" + "-" + UUIDgen.String()

			pod, err := k8s.CreatePod(kubeClient, ns, name)
			log.Infof("pod:%v,err:%v\n", pod, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForPodComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the pod
			backupName := "backup" + "pod" + "-" + UUIDgen.String()
			includeNs := kahu.BackupNameSpace
			resourceType := "Pod"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup %v is created\n", backupName)

			// create restore for the backup
			restoreName := "restore" + "pod" + "-" + UUIDgen.String()
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore %v is created\n", restoreName)

			//check if the restored pod is up
			err = k8s.WaitForPodComplete(kubeClient, nsRestore, name)
			Expect(err).To(BeNil())

			//Delete the. restore
			err = kahu.DeleteRestore(kahuClient, restoreName)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreDelete(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of pod %v is deleted\n", name)

			//Delete the backup
			err = kahu.DeleteBackup(kahuClient, backupName)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupDelete(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of pod %v is deleted\n", name)
		})

		It("pod with restore resourcePrefix", func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create pod to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["pod"] = "pod"
			UUIDgen, err := uuid.NewRandom()
			Expect(err).To(BeNil())

			name := "pod" + "-" + UUIDgen.String()

			pod, err := k8s.CreatePod(kubeClient, ns, name)
			log.Infof("pod:%v,err:%v\n", pod, err)
			Expect(err).To(BeNil())
			err = k8s.WaitForPodComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the pod
			backupName := "backup" + "pod" + "-" + UUIDgen.String()
			includeNs := kahu.BackupNameSpace
			resourceType := "Pod"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())

			// create restore for the backup
			restoreName := "restore" + "pod" + "-" + UUIDgen.String()
			nsRestore := kahu.RestoreNameSpace
			prefix := "resourceprefix"
			restore, err := kahu.CreateRestoreWithPrefix(kahuClient, restoreName, prefix, backupName, includeNs, nsRestore)
			log.Debugf("restore is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())

			//check if the restored pod is up
			err = k8s.WaitForPodComplete(kubeClient, nsRestore, prefix+name)
			Expect(err).To(BeNil())

			//Delete the. restore
			err = kahu.DeleteRestore(kahuClient, restoreName)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreDelete(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of pod %v is deleted\n", name)

			//Delete the backup
			err = kahu.DeleteBackup(kahuClient, backupName)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupDelete(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of pod %v is deleted\n", name)
		})

	})
})
