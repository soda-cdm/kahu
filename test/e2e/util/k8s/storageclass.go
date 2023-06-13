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

	"github.com/soda-cdm/kahu/test/e2e/util/kahu"
	v1 "k8s.io/api/storage/v1"
	clientset "k8s.io/client-go/kubernetes"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func CreateStorageClass(c clientset.Interface, name string) (*v1.StorageClass, error) {
	s := &v1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Provisioner: "kubernetes.io/aws-ebs",
	}
	return c.StorageV1().StorageClasses().Create(context.TODO(), s, metav1.CreateOptions{})
}

func GetStorageClass(c clientset.Interface, name string) (*v1.StorageClass, error) {
	return c.StorageV1().StorageClasses().Get(context.TODO(), name, metav1.GetOptions{})
}

func DeleteStorageClass(c clientset.Interface, name string) error {
	return c.StorageV1().StorageClasses().Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func WaitUntilStorageClassCreated(c clientset.Interface, name string) error {
	return wait.Poll(kahu.PollInterval, kahu.PollTimeout, func() (bool, error) {
		_, err := c.StorageV1().StorageClasses().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})
}
