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

// Package utils provide utility function for k8s clients
package utils

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	log "github.com/sirupsen/logrus"

	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
)

// GetConfig provide rest config from k8s config
func GetConfig(kubeConfig string) (config *restclient.Config, err error) {
	if kubeConfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeConfig)
	}

	return restclient.InClusterConfig()
}

// NamespaceAndName provide namespace and name from object
func NamespaceAndName(objMeta metav1.Object) string {
	if objMeta.GetNamespace() == "" {
		return objMeta.GetName()
	}
	return fmt.Sprintf("%s/%s", objMeta.GetNamespace(), objMeta.GetName())
}

// SetupSignalHandler helps to create cancel signal handler
func SetupSignalHandler(cancel context.CancelFunc) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		log.Infof("Received signal %s, shutting down", sig)
		cancel()
	}()
}

func GetDynamicClient(config *restclient.Config) (dynamic.Interface, error) {
	return dynamic.NewForConfig(config)
}
func GetK8sClient(config *restclient.Config) (*kubernetes.Clientset, error) {
	return kubernetes.NewForConfig(config)
}

func GetgrpcConn(address string, port uint) (*grpc.ClientConn, error) {
	return metaservice.NewLBDial(fmt.Sprintf("%s:%d", address, port), grpc.WithInsecure())
}

func GetMetaserviceClient(grpcConnection *grpc.ClientConn) metaservice.MetaServiceClient {
	return metaservice.NewMetaServiceClient(grpcConnection)
}

func GetMetaserviceBackupClient(address string, port uint) metaservice.MetaService_BackupClient {

	grpcconn, err := metaservice.NewLBDial(fmt.Sprintf("%s:%d", address, port), grpc.WithInsecure())
	if err != nil {
		log.Errorf("error getting grpc connection %s", err)
		return nil
	}
	metaClient := metaservice.NewMetaServiceClient(grpcconn)

	backupClient, err := metaClient.Backup(context.Background())
	if err != nil {
		log.Errorf("error getting backupclient %s", err)
		return nil
	}
	return backupClient
}

func GetSubItemStrings(allList []string, input string, isRegex bool) []string {
	var subItemList []string
	if isRegex {
		re := regexp.MustCompile(input)
		for _, item := range allList {
			if re.MatchString(item) {
				subItemList = append(subItemList, item)
			}
		}
	} else {
		for _, item := range allList {
			if item == input {
				subItemList = append(subItemList, item)
			}
		}
	}
	return subItemList
}

func FindMatchedStrings(kind string, allList []string, includeList, excludeList []v1beta1.ResourceIncluder) []string {
	var collectAllIncludeds []string
	var collectAllExcludeds []string
	var resultantStrings []string

	if len(includeList) == 0 {
		resultantStrings = allList
	}
	for _, resource := range includeList {
		if resource.Kind == kind {
			input, isRegex := resource.Name, resource.IsRegex
			collectAllIncludeds = append(collectAllIncludeds, GetSubItemStrings(allList, input, isRegex)...)
		}
	}
	for _, resource := range excludeList {
		if resource.Kind == kind {
			input, isRegex := resource.Name, resource.IsRegex
			collectAllExcludeds = append(collectAllExcludeds, GetSubItemStrings(allList, input, isRegex)...)
		}
	}
	if len(collectAllIncludeds) > 0 {
		resultantStrings = GetResultantItems(allList, collectAllIncludeds, collectAllExcludeds)
	}

	return resultantStrings
}
