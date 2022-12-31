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
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
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
	for {
		log.Info("Probing driver for readiness")
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

func GetMetaserviceDeleteClient(address string, port uint) metaservice.MetaServiceClient {

	grpcconn, err := metaservice.NewLBDial(fmt.Sprintf("%s:%d", address, port), grpc.WithInsecure())
	if err != nil {
		log.Errorf("error getting grpc connection %s", err)
		return nil
	}
	return metaservice.NewMetaServiceClient(grpcconn)
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

func GetBackupLocation(
	logger log.FieldLogger,
	locationName string,
	backupLocationLister kahulister.BackupLocationLister) (*kahuapi.BackupLocation, error) {
	// fetch backup location
	backupLocation, err := backupLocationLister.Get(locationName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Errorf("Backup location(%s) do not exist", locationName)
			return nil, err
		}
		logger.Errorf("Failed to get backup location. %s", err)
		return nil, err
	}

	return backupLocation, err
}

func GetProvider(
	logger log.FieldLogger,
	providerName string,
	providerLister kahulister.ProviderLister) (*kahuapi.Provider, error) {
	// fetch provider
	provider, err := providerLister.Get(providerName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Errorf("Metadata Provider(%s) do not exist", providerName)
			return nil, err
		}
		logger.Errorf("Failed to get metadata provider. %s", err)
		return nil, err
	}

	return provider, nil
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

func VolumeProvider(pv *corev1.PersistentVolume) string {
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
