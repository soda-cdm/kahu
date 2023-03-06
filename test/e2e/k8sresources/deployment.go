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

	util "github.com/soda-cdm/kahu/test/e2e/util"
	k8s "github.com/soda-cdm/kahu/test/e2e/util/k8s"
	kahu "github.com/soda-cdm/kahu/test/e2e/util/kahu"
)

//testcase for E2E deployment backup and restore
var _ = Describe("DeploymentBackup", Label("deployment"), func() {
	Context("Create backup of deployment and restore", func() {
		It("deployment with replicas and pods", Label("replicas", "testcase07"), func() {

			kubeClient, kahuClient := kahu.Clients()
			//Create deployment to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["deployment"] = "deployment"
			image := "nginx"
			name := util.GenerateUniqueName("deployment")
			replicas := 2

			deployment := k8s.NewDeployment(name, ns, int32(replicas), labels, image)
			log.Infof("deployment:%v\n", deployment)
			deployment1, err := k8s.CreateDeployment(kubeClient, ns, deployment)
			log.Debugf("deployment1:%v,err:%v\n", deployment1, err)
			Expect(err).To(BeNil())
			err = k8s.WaitForReadyDeployment(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the deployment
			backupName := util.GenerateUniqueName("backup", "deployment")
			includeNs := kahu.BackupNameSpace
			resourceType := "Deployment"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of deployment is done\n")

			// create restore for the backup
			restoreName := util.GenerateUniqueName("restore", "deployment")
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

		It("deployment with configmap EnvFrom replicas and pods", Label("configmap", "envfrom", "testcase07"), func() {

			kubeClient, kahuClient := kahu.Clients()
			//Create deployment to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["deployment"] = "deployment"
			image := "nginx"

			name := util.GenerateUniqueName("envfrom", "configmap")
			replicas := 2

			configName := util.GenerateUniqueName("configmap" + "envfrom")
			data := make(map[string]string)
			data["SPECIAL_LEVEL"] = "very"
			data["SPECIAL_TYPE"] = "charm"

			configmap, err := k8s.CreateConfigMap(kubeClient, ns, configName, data)
			log.Infof("configmap:%v,err:%v\n", configmap, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForConfigMapComplete(kubeClient, ns, configName)
			Expect(err).To(BeNil())

			deployment := k8s.NewDeploymentWithEnvFromConfigmap(name, ns, int32(replicas), labels, image, configName)
			log.Infof("deployment:%v\n", deployment)
			deployment1, err := k8s.CreateDeployment(kubeClient, ns, deployment)
			log.Debugf("deployment1:%v,err:%v\n", deployment1, err)
			Expect(err).To(BeNil())
			err = k8s.WaitForReadyDeployment(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the deployment
			backupName := util.GenerateUniqueName("envfrom", "deployment")
			includeNs := kahu.BackupNameSpace
			resourceType := "Deployment"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of deployment is done\n")

			// create restore for the backup
			restoreName := util.GenerateUniqueName("envfrom", "deployment")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of deployment :%v is created \n", restore)

			err = k8s.WaitForConfigMapComplete(kubeClient, nsRestore, configName)
			Expect(err).To(BeNil())

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

		It("deployment with secret EnvFrom replicas and pods", Label("secret", "envfrom", "testcase08"), func() {

			kubeClient, kahuClient := kahu.Clients()
			//Create deployment to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["deployment"] = "deployment"
			image := "nginx"

			name := util.GenerateUniqueName("envfrom", "secret")
			replicas := 2

			secretName := util.GenerateUniqueName("secret", "envfrom")
			data := make(map[string][]byte)
			data["SPECIAL_LEVEL"] = []byte("very")
			data["SPECIAL_TYPE"] = []byte("charm")

			secret, err := k8s.CreateSecretWithData(kubeClient, ns, secretName, labels, data)
			log.Infof("secret:%v,err:%v\n", secret, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForSecretsComplete(kubeClient, ns, secretName)
			Expect(err).To(BeNil())

			deployment := k8s.NewDeploymentWithEnvFromsecret(name, ns, int32(replicas), labels, image, secretName)
			log.Infof("deployment:%v\n", deployment)
			deployment1, err := k8s.CreateDeployment(kubeClient, ns, deployment)
			log.Debugf("deployment1:%v,err:%v\n", deployment1, err)
			Expect(err).To(BeNil())
			err = k8s.WaitForReadyDeployment(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the deployment
			backupName := util.GenerateUniqueName("envfrom", "secret")
			includeNs := kahu.BackupNameSpace
			resourceType := "Deployment"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of deployment is done\n")

			// create restore for the backup
			restoreName := util.GenerateUniqueName("envfrom", "secret")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of deployment :%v is created \n", restore)

			err = k8s.WaitForSecretsComplete(kubeClient, nsRestore, secretName)
			Expect(err).To(BeNil())

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

		It("deployment with configmap EnvVal replicas and pods", Label("configmap", "environment-variable", "testcase09"), func() {

			kubeClient, kahuClient := kahu.Clients()
			//Create deployment to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["deployment"] = "deployment"
			image := "nginx"

			name := util.GenerateUniqueName("envval", "configmap")
			replicas := 2

			configName := util.GenerateUniqueName("configmap" + "envval")
			data := make(map[string]string)
			data["SPECIAL_LEVEL"] = "very"
			data["SPECIAL_TYPE"] = "charm"

			configmap, err := k8s.CreateConfigMap(kubeClient, ns, configName, data)
			log.Infof("configmap:%v,err:%v\n", configmap, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForConfigMapComplete(kubeClient, ns, configName)
			Expect(err).To(BeNil())

			deployment := k8s.NewDeploymentWithEnvValConfigmap(name, ns, int32(replicas), labels, image, configName)
			log.Infof("deployment:%v\n", deployment)
			deployment1, err := k8s.CreateDeployment(kubeClient, ns, deployment)
			log.Debugf("deployment1:%v,err:%v\n", deployment1, err)
			Expect(err).To(BeNil())
			err = k8s.WaitForReadyDeployment(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the deployment
			backupName := util.GenerateUniqueName("envval", "deployment")
			includeNs := kahu.BackupNameSpace
			resourceType := "Deployment"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of deployment is done\n")

			// create restore for the backup
			restoreName := util.GenerateUniqueName("envval", "deployment")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of deployment :%v is created \n", restore)

			err = k8s.WaitForConfigMapComplete(kubeClient, nsRestore, configName)
			Expect(err).To(BeNil())

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

		It("deployment with secret EnvVal replicas and pods", Label("secret", "environment-variable", "testcase10"), func() {

			kubeClient, kahuClient := kahu.Clients()
			//Create deployment to test
			ns := kahu.BackupNameSpace
			labels := make(map[string]string)
			labels["deployment"] = "deployment"
			image := "nginx"

			name := util.GenerateUniqueName("envval", "secret")
			replicas := 2

			secretName := util.GenerateUniqueName("secret", "envval")
			data := make(map[string][]byte)
			data["SPECIAL_LEVEL"] = []byte("very")
			data["SPECIAL_TYPE"] = []byte("charm")

			secret, err := k8s.CreateSecretWithData(kubeClient, ns, secretName, labels, data)
			log.Infof("secret:%v,err:%v\n", secret, err)
			Expect(err).To(BeNil())

			err = k8s.WaitForSecretsComplete(kubeClient, ns, secretName)
			Expect(err).To(BeNil())

			deployment := k8s.NewDeploymentWithEnvValsecret(name, ns, int32(replicas), labels, image, secretName)
			log.Infof("deployment:%v\n", deployment)
			deployment1, err := k8s.CreateDeployment(kubeClient, ns, deployment)
			log.Debugf("deployment1:%v,err:%v\n", deployment1, err)
			Expect(err).To(BeNil())
			err = k8s.WaitForReadyDeployment(kubeClient, ns, name)
			Expect(err).To(BeNil())

			//create backup for the deployment
			backupName := util.GenerateUniqueName("envval", "secret", "deployment")
			includeNs := kahu.BackupNameSpace
			resourceType := "Deployment"
			_, err = kahu.CreateBackup(kahuClient, backupName, includeNs, resourceType)
			Expect(err).To(BeNil())
			err = kahu.WaitForBackupCreate(kahuClient, backupName)
			Expect(err).To(BeNil())
			log.Infof("backup of deployment is done\n")

			// create restore for the backup
			restoreName := util.GenerateUniqueName("envval", "secret", "deployment")
			nsRestore := kahu.RestoreNameSpace
			restore, err := kahu.CreateRestore(kahuClient, restoreName, backupName, includeNs, nsRestore)
			Expect(err).To(BeNil())
			err = kahu.WaitForRestoreCreate(kahuClient, restoreName)
			Expect(err).To(BeNil())
			log.Infof("restore of deployment :%v is created \n", restore)

			err = k8s.WaitForSecretsComplete(kubeClient, nsRestore, secretName)
			Expect(err).To(BeNil())

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
