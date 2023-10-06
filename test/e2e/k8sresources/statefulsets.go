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
	"github.com/soda-cdm/kahu/utils"
)

//testcase for E2E deployment backup and restore
var _ = Describe("statefulsetBackup", Label("statefulset"), func() {
	Context("Create backup of statefulset and restore", func() {
		It("statefulset with replicas", Label("replicas", "testcase21"), func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create statefulset to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			image := "nginx"
			replicas := 2
			labels["statefulset"] = "statefulset"

			name := util.GenerateUniqueName("statefulset")
			statefulSet, err := k8s.NewStatefulset(name, ns, int32(replicas), labels, image)
			log.Infof("statefulset:%v\n", statefulSet)
			statefulSet1, err := k8s.CreateStatefulset(kubeClient, ns, statefulSet)
			log.Debugf("statefulset:%v\n", statefulSet1)
			Expect(err).To(BeNil())
			err = k8s.WaitForStatefulsetComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the statefulset
			backupName := util.GenerateUniqueName("statefulset")
			includeNs := kahu.BackupNameSpace
			resourceType := utils.StatefulSet
			backup, err := kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			log.Infof("backup is:%v\n", backup)
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of statefulset is done\n")

			// create restore for the backup
			restoreName := util.GenerateUniqueName("statefulset")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of statefulset is created\n")

			//check if the restored deployment is up
			err = k8s.WaitForStatefulsetComplete(kubeClient, nsRestore, name)
			Expect(err).To(BeNil())
			log.Infof("statefulset restored is up\n")

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

		It("statefulset with configmap envFrom", Label("configmap", "envfrom", "testcase22"), func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create statefulset to test
			ns := kahu.BackupNameSpace

			configName := util.GenerateUniqueName("configmap", "envfrom")
			data := make(map[string]string)
			data["SPECIAL_LEVEL"] = "very"
			data["SPECIAL_TYPE"] = "charm"

			configmap, err := k8s.CreateConfigMap(kubeClient, ns, configName, data)
			log.Infof("configmap:%v,err:%v\n", configmap, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForConfigMapComplete(kubeClient, ns, configName)
			Expect(err).To(BeNil())

			labels := make(map[string]string)
			image := "nginx"
			replicas := 2
			labels["statefulset"] = "statefulset"

			name := util.GenerateUniqueName("statefulset")
			statefulSet, err := k8s.NewStatefulsetWithEnvFromConfigmap(name, ns, int32(replicas), labels, image, configName)
			log.Infof("statefulset:%v\n", statefulSet)
			statefulSet1, err := k8s.CreateStatefulset(kubeClient, ns, statefulSet)
			log.Debugf("statefulset:%v\n", statefulSet1)
			Expect(err).To(BeNil())
			err = k8s.WaitForStatefulsetComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the statefulset
			backupName := util.GenerateUniqueName("statefulset")
			includeNs := kahu.BackupNameSpace
			resourceType := utils.StatefulSet
			_, err = kahu.CreateBackupSpecResource(kahuClient, backupName, includeNs, resourceType, name)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of statefulset is done\n")

			// create restore for the backup
			restoreName := util.GenerateUniqueName("statefulset")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of statefulset is created\n")

			//check if the configmap is restored
			err = k8s.WaitForConfigMapComplete(kubeClient, nsRestore, configName)
			Expect(err).To(BeNil())

			//check if the restored deployment is up
			err = k8s.WaitForStatefulsetComplete(kubeClient, nsRestore, name)
			Expect(err).To(BeNil())
			log.Infof("statefulset restored is up\n")

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

		It("statefulset with secret envFrom", Label("secret", "envfrom", "testcase23"), func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create statefulset to test
			ns := kahu.BackupNameSpace

			secretName := util.GenerateUniqueName("secret", "envfrom")
			data := make(map[string]string)
			data["SPECIAL_LEVEL"] = "very"
			data["SPECIAL_TYPE"] = "charm"

			secret, err := k8s.CreateSecret(kubeClient, ns, secretName, data)
			log.Infof("secret:%v,err:%v\n", secret, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForSecretsComplete(kubeClient, ns, secretName)
			Expect(err).To(BeNil())

			labels := make(map[string]string)
			image := "nginx"
			replicas := 2
			labels["statefulset"] = "statefulset"

			name := util.GenerateUniqueName("statefulset")
			statefulSet, err := k8s.NewStatefulsetWithEnvFromSecret(name, ns, int32(replicas), labels, image, secretName)
			log.Infof("statefulset:%v\n", statefulSet)
			statefulSet1, err := k8s.CreateStatefulset(kubeClient, ns, statefulSet)
			log.Debugf("statefulset:%v\n", statefulSet1)
			Expect(err).To(BeNil())
			err = k8s.WaitForStatefulsetComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the statefulset
			backupName := util.GenerateUniqueName("statefulset")
			includeNs := kahu.BackupNameSpace
			resourceType := utils.StatefulSet
			_, err = kahu.CreateBackupSpecResource(kahuClient, backupName, includeNs, resourceType, name)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of statefulset is done\n")

			// create restore for the backup
			restoreName := util.GenerateUniqueName("statefulset")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of statefulset is created\n")

			//check if the secret is restored
			err = k8s.WaitForSecretsComplete(kubeClient, nsRestore, secretName)
			Expect(err).To(BeNil())

			//check if the restored deployment is up
			err = k8s.WaitForStatefulsetComplete(kubeClient, nsRestore, name)
			Expect(err).To(BeNil())
			log.Infof("statefulset restored is up\n")

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

		It("statefulset with configmap envVal", Label("configmap", "environment-variable", "testcase24"), func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create statefulset to test
			ns := kahu.BackupNameSpace

			configName := util.GenerateUniqueName("configmap", "envval")
			data := make(map[string]string)
			data["SPECIAL_LEVEL"] = "very"
			data["SPECIAL_TYPE"] = "charm"

			configmap, err := k8s.CreateConfigMap(kubeClient, ns, configName, data)
			log.Infof("configmap:%v,err:%v\n", configmap, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForConfigMapComplete(kubeClient, ns, configName)
			Expect(err).To(BeNil())

			labels := make(map[string]string)
			image := "nginx"
			replicas := 2
			labels["statefulset"] = "statefulset"

			name := util.GenerateUniqueName("statefulset")
			statefulSet, err := k8s.NewStatefulsetWithEnvValConfigmap(name, ns, int32(replicas), labels, image, configName)
			log.Infof("statefulset:%v\n", statefulSet)
			statefulSet1, err := k8s.CreateStatefulset(kubeClient, ns, statefulSet)
			log.Debugf("statefulset:%v\n", statefulSet1)
			Expect(err).To(BeNil())
			err = k8s.WaitForStatefulsetComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the statefulset
			backupName := util.GenerateUniqueName("statefulset")
			includeNs := kahu.BackupNameSpace
			resourceType := utils.StatefulSet
			_, err = kahu.CreateBackupSpecResource(kahuClient, backupName, includeNs, resourceType, name)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of statefulset is done\n")

			// create restore for the backup
			restoreName := util.GenerateUniqueName("statefulset")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of statefulset is created\n")

			//check if the configmap is restored
			err = k8s.WaitForConfigMapComplete(kubeClient, nsRestore, configName)
			Expect(err).To(BeNil())

			//check if the restored deployment is up
			err = k8s.WaitForStatefulsetComplete(kubeClient, nsRestore, name)
			Expect(err).To(BeNil())
			log.Infof("statefulset restored is up\n")

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

		It("statefulset with secret envVal", Label("secret", "environment-variable", "testcase25"), func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create statefulset to test
			ns := kahu.BackupNameSpace

			secretName := util.GenerateUniqueName("secret", "envval")
			data := make(map[string]string)
			data["SPECIAL_LEVEL"] = "very"
			data["SPECIAL_TYPE"] = "charm"

			secret, err := k8s.CreateSecret(kubeClient, ns, secretName, data)
			log.Infof("secret:%v,err:%v\n", secret, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForSecretsComplete(kubeClient, ns, secretName)
			Expect(err).To(BeNil())

			labels := make(map[string]string)
			image := "nginx"
			replicas := 2
			labels["statefulset"] = "statefulset"

			name := util.GenerateUniqueName("statefulset")
			statefulSet, err := k8s.NewStatefulsetWithEnvValSecret(name, ns, int32(replicas), labels, image, secretName)
			log.Infof("statefulset:%v\n", statefulSet)
			statefulSet1, err := k8s.CreateStatefulset(kubeClient, ns, statefulSet)
			log.Debugf("statefulset:%v\n", statefulSet1)
			Expect(err).To(BeNil())
			err = k8s.WaitForStatefulsetComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the statefulset
			backupName := util.GenerateUniqueName("statefulset")
			includeNs := kahu.BackupNameSpace
			resourceType := utils.StatefulSet
			_, err = kahu.CreateBackupSpecResource(kahuClient, backupName, includeNs, resourceType, name)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of statefulset is done\n")

			// create restore for the backup
			restoreName := util.GenerateUniqueName("statefulset")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of statefulset is created\n")

			//check if the secret is restored
			err = k8s.WaitForSecretsComplete(kubeClient, nsRestore, secretName)
			Expect(err).To(BeNil())

			//check if the restored deployment is up
			err = k8s.WaitForStatefulsetComplete(kubeClient, nsRestore, name)
			Expect(err).To(BeNil())
			log.Infof("statefulset restored is up\n")

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
