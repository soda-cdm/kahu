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

package e2e_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"

	_ "github.com/soda-cdm/kahu/test/e2e/deployment"
	k8s "github.com/soda-cdm/kahu/test/e2e/util/k8s"
	kahu "github.com/soda-cdm/kahu/test/e2e/util/kahu"
)

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2e Suite")
}

var _ = BeforeSuite(func() {
	ctx := context.TODO()
	backupNs := kahu.BackupNameSpace
	restoreNs := kahu.RestoreNameSpace
	kubeClient, _ := kahu.Clients()
	err := k8s.CreateNamespace(ctx, kubeClient, backupNs)
	if err != nil {
		log.Errorf("not able to create namespace %v bez %v\n", backupNs, err)
	}
	err = k8s.CreateNamespace(ctx, kubeClient, restoreNs)
	if err != nil {
		log.Errorf("not able to create namespace %v bez %v\n", restoreNs, err)
	}
	log.Info("********Created namespaces************\n")
})

var _ = AfterSuite(func() {
	ctx := context.TODO()
	backupNs := kahu.BackupNameSpace
	restoreNs := kahu.RestoreNameSpace
	kubeClient, _ := kahu.Clients()
	err := k8s.DeleteNamespace(ctx, kubeClient, backupNs, true)
	if err != nil {
		log.Errorf("not able to delete namespace %v bez %v\n", backupNs, err)
	}
	err = k8s.DeleteNamespace(ctx, kubeClient, restoreNs, true)
	if err != nil {
		log.Errorf("not able to delete namespace %v bez %v\n", restoreNs, err)
	}
	log.Info("********Deleted namespaces************\n")
})
