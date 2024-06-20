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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"

	"github.com/soda-cdm/kahu/test/e2e/util"
	k8s "github.com/soda-cdm/kahu/test/e2e/util/k8s"
	kahu "github.com/soda-cdm/kahu/test/e2e/util/kahu"
)

//testcase for E2E deployment backup and restore
var _ = Describe("ConfigMapBackup", Label("configmap", "testcase01"), func() {
	Context("Create backup of configmap and restore", func() {
		It("configmap", func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create configmap to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)

			name := util.GenerateUniqueName("configmap")
			configMap, err := k8s.CreateConfigMap(kubeClient, ns, name, labels)
			log.Infof("configMap:%v\n", configMap)
			Expect(err).To(BeNil())

			err = k8s.WaitForConfigMapComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the configMap
			backupName := util.GenerateUniqueName("configmap")
			includeNs := kahu.BackupNameSpace
			resourceType := "ConfigMap"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of configmap is done\n")

			// create restore for the backup
			restoreName := util.GenerateUniqueName("configmap")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore1 is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore is created\n")

			//check if the restored configmap is up
			configMap, err = k8s.GetConfigmap(kubeClient, nsRestore, name)
			log.Debugf("configmap is %v\n", configMap)
			Expect(err).To(BeNil())

			err = k8s.WaitForConfigMapComplete(kubeClient, nsRestore, name)
			Expect(err).To(BeNil())
			log.Infof("configmap restored is up\n")

			//Delete the restore
			err = kahu.DeleteRestore(kahuClient, restoreName)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreDelete(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of configmap %v is deleted\n", name)

			//Delete the backup
			err = kahu.DeleteBackup(kahuClient, backupName)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupDelete(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of configmap %v is deleted\n", name)
		})
	})
})
