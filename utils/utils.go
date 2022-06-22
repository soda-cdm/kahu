// Copyright 2022 The SODA Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func GetConfig(kubeConfig string) (config *restclient.Config, err error) {
	if kubeConfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeConfig)
	}

	return restclient.InClusterConfig()
}

func EnableLogTimeStamp() {
	customFormatter := new(log.TextFormatter)
	customFormatter.TimestampFormat = "2006-01-02 15:04:05"
	log.SetFormatter(customFormatter)
	customFormatter.FullTimestamp = true
}

func NamespaceAndName(objMeta metav1.Object) string {
	if objMeta.GetNamespace() == "" {
		return objMeta.GetName()
	}
	return fmt.Sprintf("%s/%s", objMeta.GetNamespace(), objMeta.GetName())
}

func EnsureNamespaceExistsAndIsReady(namespace string, client corev1client.NamespaceInterface) (bool, error) {
	// nsCreated tells whether the namespace was created by this method
	// required for keeping track of number of restored items

	_, err := client.Get(context.TODO(), namespace, metav1.GetOptions{})

	if apierrors.IsNotFound(err) {
		// Namespace isn't in cluster, we're good to create.
		return true, nil
	}
	return true, nil
}

func configFileName() string {
	return filepath.Join(os.Getenv("HOME"), ".config", "kahu", "config.json")
}

type kahuConfig map[string]interface{}

// LoadConfig loads the Velero client configuration file and returns it as a VeleroConfig. If the
// file does not exist, an empty map is returned.
func LoadConfig() (kahuConfig, error) {
	fileName := configFileName()

	_, err := os.Stat(fileName)
	if os.IsNotExist(err) {
		// If the file isn't there, just return an empty map
		return kahuConfig{}, nil
	}
	if err != nil {
		// For any other Stat() error, return it
		return nil, errors.WithStack(err)
	}

	configFile, err := os.Open(fileName)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer configFile.Close()

	var config kahuConfig
	if err := json.NewDecoder(configFile).Decode(&config); err != nil {
		return nil, errors.WithStack(err)
	}

	return config, nil
}
