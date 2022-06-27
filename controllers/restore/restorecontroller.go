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

package restore

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuv1client "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/informers/externalversions"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/discovery"
)

const (
	controllerName                   = "RestoreController"
	backupObjectNamespaceIndex       = "backupObject-namespace-index"
	backupObjectResourceIndex        = "backupObject-resource-index"
	backupObjectClusterResourceIndex = "backupObject-cluster-resource-index"
)

type controller struct {
	logger               log.FieldLogger
	genericController    controllers.Controller
	kubeClient           kubernetes.Interface
	dynamicClient        dynamic.Interface
	restoreClient        kahuv1client.RestoreInterface
	discoveryHelper      discovery.DiscoveryHelper
	restoreLister        kahulister.RestoreLister
	backupLister         kahulister.BackupLister
	backupLocationLister kahulister.BackupLocationLister
	providerLister       kahulister.ProviderLister
}

func NewController(kubeClient kubernetes.Interface,
	kahuClient versioned.Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper,
	informer externalversions.SharedInformerFactory) (controllers.Controller, error) {

	logger := log.WithField("controller", controllerName)
	restoreController := &controller{
		logger:               logger,
		kubeClient:           kubeClient,
		dynamicClient:        dynamicClient,
		discoveryHelper:      discoveryHelper,
		restoreClient:        kahuClient.KahuV1beta1().Restores(),
		restoreLister:        informer.Kahu().V1beta1().Restores().Lister(),
		backupLister:         informer.Kahu().V1beta1().Backups().Lister(),
		backupLocationLister: informer.Kahu().V1beta1().BackupLocations().Lister(),
		providerLister:       informer.Kahu().V1beta1().Providers().Lister(),
	}

	// construct controller interface to process worker queue
	genericController, err := controllers.NewControllerBuilder(controllerName).
		SetLogger(logger).
		SetHandler(restoreController.handler).
		Build()
	if err != nil {
		return nil, err
	}

	// register to informer to receive events and push events to worker queue
	informer.Kahu().V1beta1().Restores().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: genericController.Enqueue,
		},
	)

	// reference back
	restoreController.genericController = genericController
	return genericController, err
}

type restoreContext struct {
	logger               log.FieldLogger
	kubeClient           kubernetes.Interface
	dynamicClient        dynamic.Interface
	discoveryHelper      discovery.DiscoveryHelper
	restoreClient        kahuv1client.RestoreInterface
	restoreLister        kahulister.RestoreLister
	backupLister         kahulister.BackupLister
	backupLocationLister kahulister.BackupLocationLister
	providerLister       kahulister.ProviderLister
	backupObjectIndexer  cache.Indexer
	filter               filterHandler
	mutator              mutationHandler
}

func newRestoreContext(name string, ctrl *controller) *restoreContext {
	backupObjectIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, newBackupObjectIndexers())
	logger := ctrl.logger.WithField("restore", name)
	return &restoreContext{
		logger:               logger,
		kubeClient:           ctrl.kubeClient,
		restoreClient:        ctrl.restoreClient,
		restoreLister:        ctrl.restoreLister,
		backupLister:         ctrl.backupLister,
		backupLocationLister: ctrl.backupLocationLister,
		providerLister:       ctrl.providerLister,
		dynamicClient:        ctrl.dynamicClient,
		discoveryHelper:      ctrl.discoveryHelper,
		backupObjectIndexer:  backupObjectIndexer,
		filter:               constructFilterHandler(backupObjectIndexer, logger),
		mutator:              constructMutationHandler(backupObjectIndexer),
	}
}

func newBackupObjectIndexers() cache.Indexers {
	return cache.Indexers{
		backupObjectNamespaceIndex: func(obj interface{}) ([]string, error) {
			keys := make([]string, 0)
			metadata, err := meta.Accessor(obj)
			if err != nil {
				return nil, fmt.Errorf("object has no meta: %v", err)
			}
			if len(metadata.GetNamespace()) > 0 {
				keys = append(keys, metadata.GetNamespace())
			}
			return keys, nil
		},
		backupObjectClusterResourceIndex: func(obj interface{}) ([]string, error) {
			keys := make([]string, 0)
			metadata, err := meta.Accessor(obj)
			if err != nil {
				return nil, fmt.Errorf("object has no meta: %v", err)
			}
			if len(metadata.GetNamespace()) == 0 {
				keys = append(keys, metadata.GetName())
			}
			return keys, nil
		},
		backupObjectResourceIndex: func(obj interface{}) ([]string, error) {
			keys := make([]string, 0)
			switch u := obj.(type) {
			case runtime.Unstructured:
				keys = append(keys, u.GetObjectKind().GroupVersionKind().Kind)
			default:
				log.Warnf("%v is not unstructred object. %s skipped", obj,
					backupObjectResourceIndex)
			}

			return keys, nil
		},
	}
}

func (ctrl *controller) handler(key string) error {
	// get restore name
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ctrl.logger.Errorf("splitting key into namespace and name, error %s", err)
		return err
	}

	// get restore from cache
	restore, err := ctrl.restoreLister.Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Debugf("restore %s not found", name)
		}
		ctrl.logger.Errorf("error %s, Getting the restore resource from lister", err)
		return err
	}

	//TODO(Amit Roushan): Add check to ignore terminating restore

	// avoid polluting restore object
	workCopyRestore := restore.DeepCopy()

	ctrl.logger.WithField("restore", workCopyRestore.Name).Infof("Starting restore")
	restoreCtx := newRestoreContext(workCopyRestore.Name, ctrl)
	// update start time
	if workCopyRestore.Status.StartTimestamp.IsZero() {
		time := metav1.Now()
		workCopyRestore.Status.StartTimestamp = &time
		if workCopyRestore.Status.Phase == "" {
			workCopyRestore.Status.Phase = kahuapi.RestorePhaseInit
		}
		workCopyRestore, err = restoreCtx.updateRestoreStatus(workCopyRestore)
		if err != nil {
			return err
		}
	}

	return restoreCtx.runRestore(workCopyRestore)
}

// runRestore validates and perform restoration of resources
// Restoration process are divided into multiple steps and each step are based on restore phases
// Only restore object are passed on in Phases. The mechanism will help in failure/crash scenario
func (ctx *restoreContext) runRestore(restore *kahuapi.Restore) error {
	var err error
	ctx.logger.Infof("Processing restore for %s", restore.Name)
	// handle restore with stages
	switch restore.Status.Phase {
	case "", kahuapi.RestorePhaseInit:
		ctx.logger.Infof("Restore in %s phase", kahuapi.RestorePhaseInit)

		// validate restore
		ctx.validateRestore(restore)
		if len(restore.Status.ValidationErrors) > 0 {
			ctx.logger.Errorf("Restore validation failed. %s",
				strings.Join(restore.Status.ValidationErrors, ","))
			restore.Status.Phase = kahuapi.RestorePhaseFailedValidation
			restore, err = ctx.updateRestoreStatus(restore)
			return err
		}

		// validate and fetch backup info
		_, err := ctx.fetchBackupInfo(restore)
		if err != nil {
			// update failure reason
			if len(restore.Status.FailureReason) > 0 {
				restore, err = ctx.updateRestoreStatus(restore)
				return err
			}
			return err
		}

		ctx.logger.Info("Restore specification validation success")
		// update status to metadata restore
		restore.Status.Phase = kahuapi.RestorePhaseMeta
		restore, err = ctx.updateRestoreStatus(restore)
		if err != nil {
			ctx.logger.Errorf("Restore status update failed. %s", err)
			return err
		}
		fallthrough
	case kahuapi.RestorePhaseMeta:
		ctx.logger.Infof("Restore in %s phase", kahuapi.RestorePhaseMeta)
		// metadata restore should be last step for restore
		return ctx.processMetadataRestore(restore)
	default:
		ctx.logger.Warnf("Ignoring restore phase %s", restore.Status.Phase)
	}

	return nil
}

func (ctx *restoreContext) validateRestore(restore *kahuapi.Restore) {
	// namespace validation
	includeNamespaces := sets.NewString(restore.Spec.IncludeNamespaces...)
	excludeNamespaces := sets.NewString(restore.Spec.ExcludeNamespaces...)
	// check common namespace name in include/exclude list
	if intersection := includeNamespaces.Intersection(excludeNamespaces); intersection.Len() > 0 {
		restore.Status.ValidationErrors =
			append(restore.Status.ValidationErrors,
				fmt.Sprintf("common namespace name (%s) in include and exclude namespace list",
					strings.Join(intersection.List(), ",")))
	}

	// resource validation
	// check regular expression validity
	for _, resourceSpec := range restore.Spec.IncludeResources {
		if _, err := regexp.Compile(resourceSpec.Name); err != nil {
			restore.Status.ValidationErrors =
				append(restore.Status.ValidationErrors,
					fmt.Sprintf("invalid include resource name specification name %s",
						resourceSpec.Name))
		}
	}
	for _, resourceSpec := range restore.Spec.ExcludeResources {
		if _, err := regexp.Compile(resourceSpec.Name); err != nil {
			restore.Status.ValidationErrors =
				append(restore.Status.ValidationErrors,
					fmt.Sprintf("invalid include resource name specification name %s",
						resourceSpec.Name))
		}
	}
}

func (ctx *restoreContext) updateRestoreStatus(restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	// get restore status from lister
	currentRestore, err := ctx.restoreLister.Get(restore.Name)
	if err != nil {
		return ctx.updatePhaseWithClient(restore)
	}

	currentRestore.Status = restore.Status
	updatedRestore, err := ctx.restoreClient.UpdateStatus(context.TODO(),
		currentRestore,
		v1.UpdateOptions{})
	if err != nil {
		if apierrors.IsResourceExpired(err) {
			return ctx.updatePhaseWithClient(restore)
		}
		return restore, err
	}

	return updatedRestore, err
}

func (ctx *restoreContext) updatePhaseWithClient(restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	currentRestore, err := ctx.restoreClient.Get(context.TODO(), restore.Name, v1.GetOptions{})
	if err != nil {
		return restore, err
	}

	currentRestore.Status = restore.Status
	updatedRestore, err := ctx.restoreClient.UpdateStatus(context.TODO(),
		currentRestore,
		v1.UpdateOptions{})
	if err != nil {
		return restore, err
	}

	return updatedRestore, err
}
