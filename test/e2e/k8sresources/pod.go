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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"

	util "github.com/soda-cdm/kahu/test/e2e/util"
	k8s "github.com/soda-cdm/kahu/test/e2e/util/k8s"
	kahu "github.com/soda-cdm/kahu/test/e2e/util/kahu"
	utils "github.com/soda-cdm/kahu/utils"
)

//testcase for E2E pod backup and restore
var _ = Describe("PodBackup", Label("pod"), func() {
	Context("Create backup of pod and restore", func() {

		It("pod associated with env value configmap ", Label("configmap", "environment-variable", "testcase11"), func() {
			kubeClient, kahuClient := kahu.Clients()

			//Create pod to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["pod"] = "pod"

			name := util.GenerateUniqueName("pod", "envvalue")
			configName := util.GenerateUniqueName("configmap", "envvalue")
			data := make(map[string]string)
			data["SPECIAL_LEVEL"] = "very"
			data["SPECIAL_TYPE"] = "charm"

			configmap, err := k8s.CreateConfigMap(kubeClient, ns, configName, data)
			log.Infof("configmap:%v,err:%v\n", configmap, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForConfigMapComplete(kubeClient, ns, configName)
			Expect(err).To(BeNil())

			pod, err := k8s.CreatePodWithEnvValueConfigmap(kubeClient, ns, name, configName)
			log.Infof("pod:%v,err:%v\n", pod, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForPodComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the pod
			backupName := util.GenerateUniqueName("configmap", "envvalue", "pod")
			includeNs := kahu.BackupNameSpace
			resourceType := utils.Pod
			_, err = kahu.CreateBackupSpecResource(kahuClient, backupName, includeNs, resourceType, name)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup %v is created\n", backupName)

			// create restore for the backup
			restoreName := util.GenerateUniqueName("configmap", "envvalue", "pod")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore %v is created\n", restoreName)

			//check if the configmap is restored
			err = k8s.WaitForConfigMapComplete(kubeClient, nsRestore, configName)
			Expect(err).To(BeNil())

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

		It("pod associated with env value secret ", Label("secret", "environment-variable", "testcase12"), func() {
			kubeClient, kahuClient := kahu.Clients()

			//Create pod to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["pod"] = "pod"

			name := util.GenerateUniqueName("pod", "envvalue", "secret")

			secretName := util.GenerateUniqueName("secret", "envvalue")
			data := make(map[string][]byte)
			data["SPECIAL_LEVEL"] = []byte("very")
			data["SPECIAL_TYPE"] = []byte("charm")

			secret, err := k8s.CreateSecretWithData(kubeClient, ns, secretName, labels, data)
			log.Infof("secret:%v,err:%v\n", secret, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForSecretsComplete(kubeClient, ns, secretName)
			Expect(err).To(BeNil())

			pod, err := k8s.CreatePodWithEnvValueSecret(kubeClient, ns, name, secretName)
			log.Infof("pod:%v,err:%v\n", pod, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForPodComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the pod
			backupName := util.GenerateUniqueName("secret", "envvalue", "pod")
			includeNs := kahu.BackupNameSpace
			resourceType := utils.Pod
			_, err = kahu.CreateBackupSpecResource(kahuClient, backupName, includeNs, resourceType, name)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup %v is created\n", backupName)

			// create restore for the backup
			restoreName := util.GenerateUniqueName("secret", "envvalue", "pod")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore %v is created\n", restoreName)

			//check if the configmap is restored
			err = k8s.WaitForSecretsComplete(kubeClient, nsRestore, secretName)
			Expect(err).To(BeNil())

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

		It("pod associated with envFrom secret", Label("secret", "envfrom", "testcase13"), func() {
			kubeClient, kahuClient := kahu.Clients()

			//Create pod to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["pod"] = "pod"

			name := util.GenerateUniqueName("pod", "envfrom", "secret")

			secretName := util.GenerateUniqueName("secret", "envfrom")
			data := make(map[string]string)
			data["secret"] = "secret"

			secret, err := k8s.CreateSecret(kubeClient, ns, secretName, data)
			log.Infof("secret:%v,err:%v\n", secret, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForSecretsComplete(kubeClient, ns, secretName)
			Expect(err).To(BeNil())

			pod, err := k8s.CreatePodWithEnvFromSecret(kubeClient, ns, name, secretName)
			log.Infof("pod:%v,err:%v\n", pod, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForPodComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the pod
			backupName := util.GenerateUniqueName("secret", "envfrom", "pod")
			includeNs := kahu.BackupNameSpace
			resourceType := utils.Pod
			_, err = kahu.CreateBackupSpecResource(kahuClient, backupName, includeNs, resourceType, name)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup %v is created\n", backupName)

			// create restore for the backup
			restoreName := util.GenerateUniqueName("secret", "envfrom", "pod")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			log.Debugf("restore is %v\n", restore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore %v is created\n", restoreName)

			//check if the secret is restored
			err = k8s.WaitForSecretsComplete(kubeClient, nsRestore, secretName)
			Expect(err).To(BeNil())

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

			//delete resources created in backup namespace
			err = k8s.DeleteSecret(kubeClient, nsRestore, secretName)
			Expect(err).To(BeNil())

			ctx := context.TODO()
			err = k8s.DeletePod(ctx, kubeClient, nsRestore, name)
			Expect(err).To(BeNil())

			//delete resources created in restore namespace
			err = k8s.DeleteSecret(kubeClient, ns, secretName)
			Expect(err).To(BeNil())

			err = k8s.DeletePod(ctx, kubeClient, ns, name)
			Expect(err).To(BeNil())
		})

		It("pod associated with envFrom configmap", Label("configmap", "envfrom", "testcase14"), func() {
			kubeClient, kahuClient := kahu.Clients()

			//Create pod to test
			ctx := context.TODO()
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["pod"] = "pod"

			name := util.GenerateUniqueName("pod", "envfrom", "configmap")

			configName := util.GenerateUniqueName("configmap", "envfrom")
			data := make(map[string]string)
			data["SPECIAL_LEVEL"] = "very"
			data["SPECIAL_TYPE"] = "charm"

			configmap, err := k8s.CreateConfigMap(kubeClient, ns, configName, data)
			log.Infof("configmap:%v,err:%v\n", configmap, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForConfigMapComplete(kubeClient, ns, configName)
			Expect(err).To(BeNil())

			pod, err := k8s.CreatePodWithEnvFromConfigmap(kubeClient, ns, name, configName)
			log.Infof("pod:%v,err:%v\n", pod, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForPodComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the pod
			backupName := util.GenerateUniqueName("configmap", "envfrom", "pod")
			includeNs := kahu.BackupNameSpace
			resourceType := utils.Pod
			_, err = kahu.CreateBackupSpecResource(kahuClient, backupName, includeNs, resourceType, name)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup %v is created\n", backupName)

			// create restore for the backup
			restoreName := util.GenerateUniqueName("configmap", "envfrom", "pod")
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

			//check if the configmap is restored
			err = k8s.WaitForConfigMapComplete(kubeClient, nsRestore, configName)
			Expect(err).To(BeNil())

			//Delete the restore
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

			//delete resources created in backup namespace
			err = k8s.DeleteConfigmap(kubeClient, nsRestore, configName)
			Expect(err).To(BeNil())

			err = k8s.DeletePod(ctx, kubeClient, nsRestore, name)
			Expect(err).To(BeNil())

			//delete resources created in restore namespace
			err = k8s.DeleteConfigmap(kubeClient, ns, configName)
			Expect(err).To(BeNil())

			err = k8s.DeletePod(ctx, kubeClient, ns, name)
			Expect(err).To(BeNil())
		})

		It("pod", Label("pod", "testcase15"), func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create pod to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["pod"] = "pod"

			name := util.GenerateUniqueName("pod")

			pod, err := k8s.CreatePod(kubeClient, ns, name)
			log.Infof("pod:%v,err:%v\n", pod, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForPodComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the pod
			backupName := util.GenerateUniqueName("backup", "pod")
			includeNs := kahu.BackupNameSpace
			resourceType := utils.Pod
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup %v is created\n", backupName)

			// create restore for the backup
			restoreName := util.GenerateUniqueName("restore", "pod")
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

		It("pod with restore resourcePrefix", Label("resourcePrefix", "testcase16"), func() {
			kubeClient, kahuClient := kahu.Clients()
			//Create pod to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["pod"] = "pod"

			name := util.GenerateUniqueName("pod", "resourceprefix")

			pod, err := k8s.CreatePod(kubeClient, ns, name)
			log.Infof("pod:%v,err:%v\n", pod, err)
			Expect(err).To(BeNil())
			err = k8s.WaitForPodComplete(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the pod
			backupName := util.GenerateUniqueName("resourceprefix")
			includeNs := kahu.BackupNameSpace
			resourceType := utils.Pod
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())

			// create restore for the backup
			restoreName := util.GenerateUniqueName("resourceprefix")
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
