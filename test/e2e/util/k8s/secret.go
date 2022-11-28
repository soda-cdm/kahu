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

package util

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	waitutil "k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

func CreateSecret(c clientset.Interface, ns, name string, labels map[string]string) (*v1.Secret, error) {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Labels:    labels,
			Namespace: ns,
		},
	}
	return c.CoreV1().Secrets(ns).Create(context.TODO(), secret, metav1.CreateOptions{})
}

func WaitForSecretDelete(c clientset.Interface, ns, name string) error {
	if err := c.CoreV1().Secrets(ns).Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to delete  secret in namespace %q", ns))
	}
	return waitutil.PollImmediateInfinite(5*time.Second,
		func() (bool, error) {
			if _, err := c.CoreV1().Secrets(ns).Get(context.TODO(), ns, metav1.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			}
			logrus.Debugf("secret %q in namespace %q is still being deleted...", name, ns)
			return false, nil
		})
}

// WaitForSecretsComplete uses c to wait for completions to complete for the Job jobName in namespace ns.
func WaitForSecretsComplete(c clientset.Interface, ns, secretName string) error {
	return wait.Poll(PollInterval, PollTimeout, func() (bool, error) {
		_, err := c.CoreV1().Secrets(ns).Get(context.TODO(), secretName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})
}

func GetSecret(c clientset.Interface, ns, secretName string) (*v1.Secret, error) {
	return c.CoreV1().Secrets(ns).Get(context.TODO(), secretName, metav1.GetOptions{})
}

//CreateVCCredentialSecret refer to https://github.com/vmware-tanzu/velero-plugin-for-vsphere/blob/v1.3.0/docs/vanilla.md
func CreateVCCredentialSecret(c clientset.Interface, veleroNamespace string) error {
	secret, err := GetSecret(c, "kube-system", "vsphere-config-secret")
	if err != nil {
		return err
	}
	vsphereCfg, exist := secret.Data["csi-vsphere.conf"]
	if !exist {
		return errors.New("failed to retrieve csi-vsphere config")
	}
	se := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "velero-vsphere-config-secret",
			Namespace: veleroNamespace,
		},
		Type: v1.SecretTypeOpaque,
		Data: map[string][]byte{"csi-vsphere.conf": vsphereCfg},
	}
	_, err = c.CoreV1().Secrets(veleroNamespace).Create(context.TODO(), se, metav1.CreateOptions{})
	return err
}

func DeleteSecret(c clientset.Interface, ns, name string) error {
	return c.CoreV1().Secrets(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
}
