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

package kahu

import (
	"context"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	waitutil "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	"github.com/soda-cdm/kahu/controllers/app/options"
	"github.com/soda-cdm/kahu/test/e2e/util"
)

const (
	AgentBaseName = "controller-manager"
	PollInterval  = 2 * time.Second
	PollTimeout   = 30 * time.Second
)

var BackupNameSpace string = util.GenerateUniqueName("backup")
var RestoreNameSpace string = util.GenerateUniqueName("restore")

func NewBackup(name string, ns string, objType string) *kahuapi.Backup {
	backup := &kahuapi.Backup{}
	backup.APIVersion = "kahu.io/v1beta1"
	backup.Kind = "backup"
	backup.Name = name
	backup.Spec.IncludeNamespaces = []string{ns}
	resourceSpec := kahuapi.ResourceSpec{
		Kind:    objType,
		IsRegex: true,
	}
	backup.Spec.IncludeResources = []kahuapi.ResourceSpec{resourceSpec}
	backup.Spec.MetadataLocation = "nfs"
	return backup
}

func NewBackupSpecResource(name string, ns string, objType, objName string) *kahuapi.Backup {
	backup := &kahuapi.Backup{}
	backup.APIVersion = "kahu.io/v1beta1"
	backup.Kind = "backup"
	backup.Name = name
	backup.Spec.IncludeNamespaces = []string{ns}
	resourceSpec := kahuapi.ResourceSpec{
		Name:    objName,
		Kind:    objType,
		IsRegex: true,
	}
	backup.Spec.IncludeResources = []kahuapi.ResourceSpec{resourceSpec}
	backup.Spec.MetadataLocation = "nfs"
	return backup
}

func NewRestore(name, nsRestore, nsBackup, backupName string) *kahuapi.Restore {
	restore := &kahuapi.Restore{}
	restore.APIVersion = "kahu.io/v1beta1"
	restore.Kind = "Restore"
	restore.ObjectMeta.Name = name
	restore.Spec.BackupName = backupName
	restore.Spec.NamespaceMapping = map[string]string{nsBackup: nsRestore}
	return restore
}

func NewRestoreWithPrefix(name, nsRestore, nsBackup, backupName, prefix string) *kahuapi.Restore {
	restore := &kahuapi.Restore{}
	restore.APIVersion = "kahu.io/v1beta1"
	restore.Kind = "Restore"
	restore.ObjectMeta.Name = name
	restore.Spec.BackupName = backupName
	restore.Spec.NamespaceMapping = map[string]string{nsBackup: nsRestore}
	// restore.Spec.ResourcePrefix = prefix
	return restore
}

func CreateRestoreWithPrefix(c versioned.Interface, restoreName, prefix, backupName, backupNS, restoreNS string) (*v1beta1.Restore, error) {
	opts := metav1.CreateOptions{}
	ctx := context.TODO()
	restore := NewRestoreWithPrefix(restoreName, restoreNS, backupNS, backupName, prefix)
	restore, err := c.KahuV1beta1().Restores().Create(ctx, restore, opts)
	return restore, err
}

func CreateBackup(c versioned.Interface, backupName, nameSpace, resourceType string) (*v1beta1.Backup, error) {
	backup := NewBackup(backupName, nameSpace, resourceType)
	opts := metav1.CreateOptions{}
	ctx := context.TODO()
	backup, err := c.KahuV1beta1().Backups().Create(ctx, backup, opts)
	return backup, err

}

func CreateBackupSpecResource(c versioned.Interface, backupName, nameSpace, resourceType, resourceName string) (*v1beta1.Backup, error) {
	backup := NewBackupSpecResource(backupName, nameSpace, resourceType, resourceName)
	opts := metav1.CreateOptions{}
	ctx := context.TODO()
	backup, err := c.KahuV1beta1().Backups().Create(ctx, backup, opts)
	return backup, err

}

func CreateRestore(c versioned.Interface, restoreName, backupName, backupNS, restoreNS string) (*v1beta1.Restore, error) {
	opts := metav1.CreateOptions{}
	ctx := context.TODO()
	restore := NewRestore(restoreName, restoreNS, backupNS, backupName)
	restore, err := c.KahuV1beta1().Restores().Create(ctx, restore, opts)
	return restore, err
}

func DeleteBackup(c versioned.Interface, backupName string) error {
	ctx := context.TODO()
	optsDel := metav1.DeleteOptions{}
	err := c.KahuV1beta1().Backups().Delete(ctx, backupName, optsDel)
	return err
}

func DeleteRestore(c versioned.Interface, restoreName string) error {
	ctx := context.TODO()
	optsDel := metav1.DeleteOptions{}
	err := c.KahuV1beta1().Restores().Delete(ctx, restoreName, optsDel)
	return err
}

func Clients() (kubernetes.Interface, versioned.Interface) {
	optManager, err := options.NewOptionsManager()
	if err != nil {
		log.Fatalf("Failed to initialize controller option manager")
	}
	cfg, err := optManager.Config()
	if err != nil {
		log.Errorf("Failed to get configuration %s", err)
		os.Exit(1)
	}

	f := client.NewFactory(AgentBaseName, &cfg.KahuClientConfig)
	kubeClient, err := f.KubeClient()
	kahuClient, err := f.KahuClient()
	return kubeClient, kahuClient
}

func WaitForBackupCreate(c versioned.Interface, backupName string) error {
	getOpts := metav1.GetOptions{}
	return wait.Poll(PollInterval, PollTimeout, func() (bool, error) {
		backup, err := c.KahuV1beta1().Backups().Get(context.TODO(), backupName, getOpts)
		log.Debugf("backup is %v:\n", backup)
		if err != nil {
			return false, err
		}
		if backup.Status.State != kahuapi.BackupState(v1beta1.BackupStateCompleted) {
			logrus.Infof("backup %q  is still being created...", backupName)
			return false, nil
		}
		return true, nil
	})
}

func WaitForRestoreCreate(c versioned.Interface, restoreName string) error {
	getOpts := metav1.GetOptions{}
	return wait.Poll(PollInterval, PollTimeout, func() (bool, error) {
		restore, err := c.KahuV1beta1().Restores().Get(context.TODO(), restoreName, getOpts)
		log.Debugf("restore %v:\n", restore)
		if err != nil {
			return false, err
		}
		if restore.Status.State != kahuapi.RestoreStateCompleted {
			logrus.Infof("restore %q  is still being created...", restoreName)
			return false, nil
		}
		return true, nil
	})
}

func WaitForBackupDelete(c versioned.Interface, backupName string) error {
	getOpts := metav1.GetOptions{}
	return waitutil.PollImmediateInfinite(5*time.Second,
		func() (bool, error) {
			if _, err := c.KahuV1beta1().Backups().Get(context.TODO(), backupName, getOpts); err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			}
			logrus.Debugf("backup %v is still getting deleted...\n", backupName)
			return false, nil
		})
}

func WaitForRestoreDelete(c versioned.Interface, restoreName string) error {
	getOpts := metav1.GetOptions{}
	return waitutil.PollImmediateInfinite(5*time.Second,
		func() (bool, error) {
			if _, err := c.KahuV1beta1().Restores().Get(context.TODO(), restoreName, getOpts); err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			}
			logrus.Debugf("restore %v is still getting deleted...\n", restoreName)
			return false, nil
		})
}
