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

package utils

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"

	// apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	providerservice "github.com/soda-cdm/kahu/providers/lib/go"
)

const (
	probeInterval               = 1 * time.Second
	EventOwnerNotDeleted        = "OwnerNotDeleted"
	EventCancelVolRestoreFailed = "CancelVolRestoreFailed"
)

var (
	GVKPersistentVolumeClaim = schema.GroupVersionKind{Group: corev1.SchemeGroupVersion.Group,
		Version: corev1.SchemeGroupVersion.Version, Kind: "PersistentVolumeClaim"}
	GVKRestore = schema.GroupVersionKind{Group: kahuapi.SchemeGroupVersion.Group,
		Version: kahuapi.SchemeGroupVersion.Version, Kind: "Restore"}
	GVKBackup = schema.GroupVersionKind{Group: kahuapi.SchemeGroupVersion.Group,
		Version: kahuapi.SchemeGroupVersion.Version, Kind: "Backup"}

	DeletionHandlingMetaNamespaceKeyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc
)

func GetConfig(kubeConfig string) (config *restclient.Config, err error) {
	if kubeConfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeConfig)
	}

	return restclient.InClusterConfig()
}

func NamespaceAndName(objMeta metav1.Object) string {
	if objMeta.GetNamespace() == "" {
		return objMeta.GetName()
	}
	return fmt.Sprintf("%s/%s", objMeta.GetNamespace(), objMeta.GetName())
}

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

func GetGRPCConnection(endpoint string, dialOptions ...grpc.DialOption) (*grpc.ClientConn, error) {
	dialOptions = append(dialOptions,
		grpc.WithInsecure(),                   // unix domain connection.
		grpc.WithBackoffMaxDelay(time.Second), // Retry every second after failure.
		grpc.WithBlock(),                      // Block until connection succeeds.
		grpc.WithChainUnaryInterceptor(
			AddGRPCRequestID, // add gRPC request id
		),
	)

	unixPrefix := "unix://"
	if strings.HasPrefix(endpoint, "/") {
		// It looks like filesystem path.
		endpoint = unixPrefix + endpoint
	}

	if !strings.HasPrefix(endpoint, unixPrefix) {
		return nil, fmt.Errorf("invalid unix domain path [%s]",
			endpoint)
	}
	log.Info("Probing volume provider endpoint")
	return grpc.Dial(endpoint, dialOptions...)
}

func AddGRPCRequestID(ctx context.Context,
	method string,
	req, reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption) error {
	return invoker(ctx, method, req, reply, cc, opts...)
}

func Probe(conn grpc.ClientConnInterface, timeout time.Duration) error {
	probe := func(conn grpc.ClientConnInterface, timeout time.Duration) (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		rsp, err := providerservice.
			NewIdentityClient(conn).
			Probe(ctx, &providerservice.ProbeRequest{})

		if err != nil {
			return false, err
		}

		r := rsp.GetReady()
		if r == nil {
			return true, nil
		}
		return r.GetValue(), nil
	}

	for {
		log.Info("Probing driver for readiness")
		ready, err := probe(conn, timeout)
		if err != nil {
			st, ok := status.FromError(err)
			if !ok {
				return fmt.Errorf("driver probe failed: %s", err)
			}
			if st.Code() != codes.DeadlineExceeded {
				return fmt.Errorf("driver probe failed: %s", err)
			}
			// Timeout -> driver is not ready. Fall through to sleep() below.
			log.Warning("driver probe timed out")
		}
		if ready {
			return nil
		}
		// sleep for retry again
		time.Sleep(probeInterval)
	}
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

func FindMatchedStrings(kind string, allList []string, includeList, excludeList []kahuapi.ResourceSpec) []string {
	var collectAllIncludeds []string
	var collectAllExcludeds []string

	if len(includeList) == 0 {
		collectAllIncludeds = allList
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
		collectAllIncludeds = GetResultantItems(allList, collectAllIncludeds, collectAllExcludeds)
	}

	return collectAllIncludeds
}

// CheckBackupSupport checks if PV is t be considered for backup or not
func CheckBackupSupport(pv corev1.PersistentVolume) error {
	if pv.Spec.CSI == nil {
		// non CSI Volumes
		msg := fmt.Sprintf("PV %s is non CSI volume, can not backup.", pv.Name)
		return errors.New(msg)
	}

	//supportedCsiDrivers := sets.NewString(SupportedCsiDrivers...)
	//driver := pv.Spec.CSI.Driver
	//if !supportedCsiDrivers.Has(driver) {
	//	msg := fmt.Sprintf("PV %s belongs to the driver(%s), not supported for backup.", pv.Name, driver)
	//	return errors.New(msg)
	//}
	return nil
}

func CheckServerUnavailable(err error) bool {
	if e, ok := status.FromError(err); ok {
		return e.Code() == codes.Unavailable
	}

	return false
}

func VolumeProvisioner(pv *corev1.PersistentVolume) string {
	if pv.Spec.CSI != nil {
		return pv.Spec.CSI.Driver
	}
	if pv.Spec.AWSElasticBlockStore != nil {
		return "AWSElasticBlockStore"
	}
	if pv.Spec.AzureDisk != nil {
		return "AzureDisk"
	}
	if pv.Spec.AzureFile != nil {
		return "AzureFile"
	}
	if pv.Spec.CephFS != nil {
		return "CephFS"
	}

	return ""
}

func ToResourceID(apiVersion, kind, namespace, name string) string {
	if namespace == "" {
		return fmt.Sprintf("%s.%s/%s", kind, apiVersion, name)
	}
	return fmt.Sprintf("%s.%s/%s/%s", kind, apiVersion, namespace, name)
}

func FindUnixSocket(dir string) (string, error) {
	unixSocketFile := ""
	err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			// prevent panic by handling failure accessing a path
			return err
		}
		if info.IsDir() && path != dir {
			// skipping a dir without errors
			return fs.SkipDir
		}
		if (info.Mode() & fs.ModeSocket) == fs.ModeSocket {
			if unixSocketFile == "" {
				unixSocketFile = path
			}
			return nil
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("error walking the path %q: %v", dir, err)
	}

	return unixSocketFile, nil
}
