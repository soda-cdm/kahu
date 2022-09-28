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
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"sync"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/clientset/versioned"
	kahuclient "github.com/soda-cdm/kahu/client/clientset/versioned/typed/kahu/v1beta1"
	"github.com/soda-cdm/kahu/client/informers/externalversions"
	kahulister "github.com/soda-cdm/kahu/client/listers/kahu/v1beta1"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/soda-cdm/kahu/discovery"
	"github.com/soda-cdm/kahu/hooks"
	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"github.com/soda-cdm/kahu/utils"
)

type controller struct {
	logger               log.FieldLogger
	genericController    controllers.Controller
	kubeClient           kubernetes.Interface
	dynamicClient        dynamic.Interface
	restoreClient        kahuclient.RestoreInterface
	discoveryHelper      discovery.DiscoveryHelper
	restoreLister        kahulister.RestoreLister
	backupLister         kahulister.BackupLister
	backupLocationLister kahulister.BackupLocationLister
	providerLister       kahulister.ProviderLister
	restoreVolumeClient  kahuclient.VolumeRestoreContentInterface
	podCommandExecutor   hooks.PodCommandExecutor
	podGetter            cache.Getter
	processedRestore     utils.Store
}

func NewController(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	kahuClient versioned.Interface,
	dynamicClient dynamic.Interface,
	discoveryHelper discovery.DiscoveryHelper,
	informer externalversions.SharedInformerFactory,
	podCommandExecutor hooks.PodCommandExecutor,
	podGetter cache.Getter) (controllers.Controller, error) {

	logger := log.WithField("controller", controllerName)
	processedRestoreCache := utils.NewStore(utils.DeletionHandlingMetaNamespaceKeyFunc)
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
		restoreVolumeClient:  kahuClient.KahuV1beta1().VolumeRestoreContents(),
		podCommandExecutor:   podCommandExecutor,
		podGetter:            podGetter,
		processedRestore:     processedRestoreCache,
	}

	// construct controller interface to process worker queue
	genericController, err := controllers.NewControllerBuilder(controllerName).
		SetLogger(logger).
		SetHandler(restoreController.processQueue).
		Build()
	if err != nil {
		return nil, err
	}

	// register to informer to receive events and push events to worker queue
	informer.Kahu().V1beta1().Restores().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: genericController.Enqueue,
			UpdateFunc: func(oldObj, newObj interface{}) {
				genericController.Enqueue(newObj)
			},
		},
	)

	go newReconciler(defaultReconcileTimeLoop,
		restoreController.logger.WithField("source", "reconciler"),
		restoreController.restoreVolumeClient,
		restoreController.restoreClient,
		restoreController.restoreLister).Run(ctx.Done())

	// reference back
	restoreController.genericController = genericController
	return genericController, err
}

type restoreContext struct {
	logger               log.FieldLogger
	kubeClient           kubernetes.Interface
	dynamicClient        dynamic.Interface
	discoveryHelper      discovery.DiscoveryHelper
	restoreClient        kahuclient.RestoreInterface
	restoreLister        kahulister.RestoreLister
	backupLister         kahulister.BackupLister
	backupLocationLister kahulister.BackupLocationLister
	providerLister       kahulister.ProviderLister
	restoreVolumeClient  kahuclient.VolumeRestoreContentInterface
	backupObjectIndexer  cache.Indexer
	resolveObjects       cache.Indexer
	filter               filterHandler
	mutator              mutationHandler
	resolver             Interface
	hooksWaitGroup       sync.WaitGroup
	hooksErrs            chan error
	waitExecHookHandler  hooks.WaitExecHookHandler
	hooksContext         context.Context
	hooksCancelFunc      context.CancelFunc
	podCommandExecutor   hooks.PodCommandExecutor
	processedRestore     utils.Store
}

func newRestoreContext(name string, ctrl *controller) *restoreContext {
	backupObjectIndexer := cache.NewIndexer(uidKeyFunc, newBackupObjectIndexers())
	logger := ctrl.logger.WithField("restore", name)
	hooksCtx, hooksCancelFunc := context.WithCancel(context.Background())
	waitExecHookHandler := &hooks.DefaultWaitExecHookHandler{
		PodCommandExecutor: ctrl.podCommandExecutor,
		ListWatchFactory: &hooks.DefaultListWatchFactory{
			PodsGetter: ctrl.podGetter,
		},
	}
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
		restoreVolumeClient:  ctrl.restoreVolumeClient,
		filter:               constructFilterHandler(logger.WithField("context", "filter")),
		mutator:              constructMutationHandler(logger.WithField("context", "mutator")),
		resolver:             NewResolver(logger.WithField("context", "resolver")),
		hooksErrs:            make(chan error),
		waitExecHookHandler:  waitExecHookHandler,
		podCommandExecutor:   ctrl.podCommandExecutor,
		hooksContext:         hooksCtx,
		hooksCancelFunc:      hooksCancelFunc,
		processedRestore:     ctrl.processedRestore,
	}
}

func uidKeyFunc(obj interface{}) (string, error) {
	if key, ok := obj.(cache.ExplicitKey); ok {
		return string(key), nil
	}
	meta, err := meta.Accessor(obj)
	if err != nil {
		return "", fmt.Errorf("object has no meta: %v", err)
	}

	return string(meta.GetUID()), nil
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
				log.Warningf("%v is not unstructred object. %s skipped", obj,
					backupObjectResourceIndex)
			}

			return keys, nil
		},
	}
}

func (ctrl *controller) processQueue(index string) error {
	// get restore name
	_, name, err := cache.SplitMetaNamespaceKey(index)
	if err != nil {
		ctrl.logger.Errorf("splitting key into namespace and name, error %s", err)
		return err
	}

	// get restore from cache
	restore, err := ctrl.restoreLister.Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctrl.logger.Debugf("Restore %s not found", name)
			return nil
		}
		ctrl.logger.Errorf("Getting the restore resource from lister, Error %s", err)
		return err
	}

	// avoid polluting restore object
	restoreClone := restore.DeepCopy()
	newObj, err := utils.StoreRevisionUpdate(ctrl.processedRestore, restoreClone, "Restore")
	if err != nil {
		ctrl.logger.Errorf("%s", err)
	}
	if !newObj {
		return nil
	}

	restoreCtx := newRestoreContext(restoreClone.Name, ctrl)

	if restore.DeletionTimestamp != nil {
		return restoreCtx.deleteRestore(restoreClone)
	}

	err = restoreCtx.syncRestore(restoreClone)
	if apierrors.IsConflict(err) { // if failed to update status restart
		return nil
	}
	return err
}

func (ctx *restoreContext) deleteRestore(restore *kahuapi.Restore) error {
	ctx.logger.Infof("Deleting restore %s", restore.Name)

	err := ctx.removeVolumeRestore(restore)
	if err != nil {
		ctx.logger.Errorf("Unable to delete volume backup. %s", err)
		return err
	}

	// If restore is not fully successful, trigger resource cleanup
	if !(restore.Status.State == kahuapi.RestoreStateCompleted &&
		restore.Status.Stage == kahuapi.RestoreStageFinished) {
		// cleanup metadata except pv/pvc which has restoreName annotation
		_, err = ctx.cleanupRestoredMetadata(restore)
		if err != nil {
			ctx.logger.Errorf("Unable to cleanup restored metadata for restore %s. Reason: %s", restore.Name, err)
			return errors.Wrap(err, "Unable to cleanup restored metadata")
		}
	}

	// check if all volume backup contents are deleted
	vrcList, err := ctx.restoreVolumeClient.List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.Set{volumeContentRestoreLabel: restore.Name}.AsSelector().String()})
	if err != nil {
		ctx.logger.Errorf("Unable to get volume restore list. %s", err)
		return err
	}
	if len(vrcList.Items) > 0 {
		ctx.logger.Infoln("Volume restore list is not empty. Continue to wait for Volume backup delete")
		return nil
	}

	ctx.logger.Info("Volume restore deleted successfully")

	restoreClone := restore.DeepCopy()
	utils.RemoveFinalizer(restoreClone, restoreFinalizer)
	_, err = ctx.patchRestore(restore, restoreClone)
	if err != nil {
		ctx.logger.Errorf("removing finalizer failed for %s", restore.Name)
	}

	err = utils.StoreClean(ctx.processedRestore, restore, "Restore")
	if err != nil {
		ctx.logger.Warningf("Failed to clean processed cache. %s", err)
	}
	return err
}

func (ctx *restoreContext) initRestore(restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	// update start time
	if restore.Status.StartTimestamp == nil {
		time := metav1.Now()
		restore.Status.StartTimestamp = &time
	}
	if restore.Status.Stage == "" {
		restore.Status.Stage = kahuapi.RestoreStageInitial
	}
	if restore.Status.State == "" {
		restore.Status.State = kahuapi.RestoreStateNew
	}

	newRestore, err := ctx.updateRestoreStatus(restore)
	if err != nil {
		ctx.logger.Error("Unable to update initial restore state")
		return restore, err
	}

	return newRestore, err
}

func (ctx *restoreContext) restoreCompleted(restore *kahuapi.Restore) bool {
	return restore.Status.Stage == kahuapi.RestoreStageFinished &&
		restore.Status.State == kahuapi.RestoreStateCompleted
}

func (ctx *restoreContext) restoreFailed(restore *kahuapi.Restore) bool {
	return restore.Status.State == kahuapi.RestoreStateFailed
}

// syncRestore validates and perform restoration of resources
// Restoration process are divided into multiple steps and each step are based on restore phases
// Only restore object are passed on in Phases. The mechanism will help in failure/crash scenario
func (ctx *restoreContext) syncRestore(restore *kahuapi.Restore) error {
	var err error

	// Rollback restore in case of failure at any stage
	//defer func() {
	//	if ctx.restoreFailed(restore) &&
	//		!metav1.HasAnnotation(restore.ObjectMeta, annRestoreCleanupDone) {
	//		ctx.deleteRestore(restore)
	//	}
	//}()

	// do not process if restore failed already
	if ctx.restoreFailed(restore) || ctx.restoreCompleted(restore) {
		return nil
	}

	ctx.logger.Infof("Processing restore for %s", restore.Name)
	if !utils.ContainsFinalizer(restore, restoreFinalizer) {
		updatedRestore := restore.DeepCopy()
		utils.SetFinalizer(updatedRestore, restoreFinalizer)
		restore, err = ctx.patchRestore(restore, updatedRestore)
		if err != nil {
			ctx.logger.Error("Unable to update finalizer for restore")
			return err
		}
	}

	// handle restore with stages
	switch restore.Status.Stage {
	case "", kahuapi.RestoreStageInitial:
		ctx.logger.Infof("Restore in %s phase", kahuapi.RestoreStageInitial)
		if restore, err = ctx.initRestore(restore); err != nil {
			return err
		}

		// validate restore
		if err = ctx.validateRestore(restore); err != nil {
			return err
		}

		ctx.logger.Info("Restore specification validation success")
		restore.Status.Stage = kahuapi.RestoreStageVolumes
		restore.Status.State = kahuapi.RestoreStateNew
		restore, err = ctx.updateRestoreStatus(restore)
		if err != nil {
			return err
		}
	}

	if restore, err = ctx.populateRestoreResources(restore); err != nil {
		restore.Status.FailureReason = fmt.Sprintf("Failed to get restore content. %s", err)
		_, err = ctx.updateRestoreState(restore, kahuapi.RestoreStateFailed)
		if err != nil {
			return err
		}
		return nil
	}

	switch restore.Status.Stage {
	case kahuapi.RestoreStageVolumes:
		if restore, err = ctx.syncVolumeRestore(restore, ctx.resolveObjects); err != nil {
			return err
		}
		if restore.Status.State != kahuapi.RestoreStateCompleted {
			return nil
		}
		ctx.logger.Info("Volume restore successful. Moving on to Resource restore")
		restore.Status.State = kahuapi.RestoreStateNew
		if restore, err = ctx.updateRestoreStage(restore, kahuapi.RestoreStageResources); err != nil {
			return err
		}
		fallthrough
	case kahuapi.RestoreStageResources:
		if restore, err = ctx.syncMetadataRestore(restore, ctx.resolveObjects); err != nil {
			return err
		}
		if restore.Status.State != kahuapi.RestoreStateCompleted {
			return nil
		}
		ctx.logger.Info("Resources restore successful. Moving on to Finishing restore")
		fallthrough
	case kahuapi.RestoreStagePostHook:
		if restore.Status.State != kahuapi.RestoreStateCompleted {
			return nil
		}
		if restore, err = ctx.updateRestoreStage(restore, kahuapi.RestoreStageFinished); err != nil {
			return err
		}
		fallthrough
	case kahuapi.RestoreStageFinished:
		if restore.Status.State == kahuapi.RestoreStateCompleted {
			return nil
		}
		restore.Status.State = kahuapi.RestoreStateCompleted
		time := metav1.Now()
		restore.Status.CompletionTimestamp = &time
		if restore, err = ctx.updateRestoreStatus(restore); err != nil {
			return err
		}
		return nil
	default:
		return nil
	}
}

func (ctx *restoreContext) validateRestore(restore *kahuapi.Restore) error {
	validationErrors := make([]string, 0)
	// namespace validation
	includeNamespaces := sets.NewString(restore.Spec.IncludeNamespaces...)
	excludeNamespaces := sets.NewString(restore.Spec.ExcludeNamespaces...)
	// check common namespace name in include/exclude list
	if intersection := includeNamespaces.Intersection(excludeNamespaces); intersection.Len() > 0 {
		validationErrors = append(validationErrors,
			fmt.Sprintf("common namespace name (%s) in include and exclude namespace list",
				strings.Join(intersection.List(), ",")))
	}

	// resource validation
	// check regular expression validity
	for _, resourceSpec := range restore.Spec.IncludeResources {
		if _, err := regexp.Compile(resourceSpec.Name); err != nil {
			validationErrors = append(validationErrors,
				fmt.Sprintf("invalid include resource specification name %s", resourceSpec.Name))
		}
	}

	for _, resourceSpec := range restore.Spec.ExcludeResources {
		if _, err := regexp.Compile(resourceSpec.Name); err != nil {
			validationErrors = append(validationErrors,
				fmt.Sprintf("invalid exclude resource specification name %s", resourceSpec.Name))
		}
	}

	// validate and fetch backup info
	_, err := ctx.fetchBackupInfo(restore)
	if err != nil {
		validationErrors = append(validationErrors,
			fmt.Sprintf("backup information misconfigured. %s", err))
	}

	if len(validationErrors) > 0 {
		ctx.logger.Errorf("Restore validation failed. %s", strings.Join(validationErrors, ", "))
		restore.Status.ValidationErrors = validationErrors
		restore.Status.State = kahuapi.RestoreStateFailed
		_, err = ctx.updateRestoreStatus(restore)
		if err != nil {
			return err
		}
		return errors.New("restore validation failed")
	}

	return nil
}

func (ctx *restoreContext) populateRestoreResources(restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	if ctx.resolveObjects != nil {
		return restore, nil
	}
	// fetch backup info
	backupInfo, err := ctx.fetchBackupInfo(restore)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to get backup content. %s", err)
		restore, err = ctx.updateRestoreStatus(restore)
		if err != nil {
			return restore, err
		}
		return restore, errors.New("Failed to fetch backup info")
	}

	// construct backup identifier
	backupIdentifier, err := utils.GetBackupIdentifier(backupInfo.backup,
		backupInfo.backupLocation,
		backupInfo.backupProvider)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to get backup identifier. %s", err)
		restore, err = ctx.updateRestoreStatus(restore)
		if err != nil {
			return restore, err
		}
		return restore, errors.New("Failed to fetch backup identifier")
	}

	// fetch backup content and cache them
	err = ctx.fetchBackupContent(backupInfo.backupProvider, backupIdentifier, restore)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to get backup content. %s",
			err)
		restore, err = ctx.updateRestoreStatus(restore)
		if err != nil {
			return restore, err
		}
		return restore, errors.New("Failed to fetch backup content")
	}

	restoreIndexer, err := ctx.cloneIndexer()
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = err.Error()
		restore, err = ctx.updateRestoreStatus(restore)
		if err != nil {
			return restore, err
		}
		return restore, errors.New("Failed to create index")
	}

	// filter resources from cache
	err = ctx.filter.handle(restore, restoreIndexer)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to filter resources. %s", err)
		restore, err = ctx.updateRestoreStatus(restore)
		if err != nil {
			return restore, err
		}
		return restore, errors.New("Failed to filter resources from cache")
	}

	// resolve dependencies for restore candidates
	err = ctx.resolver.Resolve(restoreIndexer, ctx.backupObjectIndexer)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to get dependency resources. %s", err)
		restore, err = ctx.updateRestoreStatus(restore)
		if err != nil {
			return restore, err
		}
		return restore, errors.New("Failed to resolve dependencies during restore validation")
	}

	// add mutation
	err = ctx.mutator.handle(restore, restoreIndexer)
	if err != nil {
		restore.Status.State = kahuapi.RestoreStateFailed
		restore.Status.FailureReason = fmt.Sprintf("Failed to mutate resources. %s", err)
		restore, err = ctx.updateRestoreStatus(restore)
		if err != nil {
			return restore, err
		}
		return restore, errors.New("Failed to add mutation")
	}

	// update resolved resources in restore object
	if restore, err = ctx.patchRestoreObjects(restore, restoreIndexer); err != nil {
		ctx.logger.Warningf("Failed to sync restore resources with restore(%s)", restore.Name)
	}

	ctx.resolveObjects = restoreIndexer

	//err = ctx.checkForRestoreResConflict(restore)
	//if err != nil {
	//	restore.Status.State = kahuapi.RestoreStateFailed
	//	restore.Status.FailureReason = err.Error()
	//	restore, err = ctx.updateRestoreStatus(restore)
	//	if err != nil {
	//		return restore, err
	//	}
	//	return restore, errors.New("Resource conflict occurred during restore validation, " +
	//		"suggest to restore to new namespace or perform residual resource cleanup in source namespace")
	//}

	return restore, nil
}

func (ctx *restoreContext) patchRestoreObjects(
	restore *kahuapi.Restore,
	indexer cache.Indexer) (*kahuapi.Restore, error) {
	resourceList := indexer.List()
	restoreObjects := make([]kahuapi.RestoreResource, 0)

	var restoreResource *unstructured.Unstructured
	for _, resource := range resourceList {
		switch unstructuredResource := resource.(type) {
		case *unstructured.Unstructured:
			restoreResource = unstructuredResource
		case unstructured.Unstructured:
			restoreResource = unstructuredResource.DeepCopy()
		default:
			ctx.logger.Warningf("Unknown cached resource type. %s", reflect.TypeOf(resource))
			continue
		}

		restoreObjects = append(restoreObjects, kahuapi.RestoreResource{
			Namespace:    restoreResource.GetNamespace(),
			ResourceName: restoreResource.GetName(),
			TypeMeta: metav1.TypeMeta{
				APIVersion: restoreResource.GetAPIVersion(),
				Kind:       restoreResource.GetKind(),
			},
		})
	}

	newRestore := restore.DeepCopy()
	newRestore.Status.Resources = restoreObjects

	return ctx.updateRestoreStatus(newRestore)
}

func (ctx *restoreContext) patchRestore(oldRestore, updatedRestore *kahuapi.Restore) (*kahuapi.Restore, error) {
	origBytes, err := json.Marshal(oldRestore)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original backup")
	}

	updatedBytes, err := json.Marshal(updatedRestore)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated backup")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for backup")
	}

	newRestore, err := ctx.restoreClient.Patch(context.TODO(),
		oldRestore.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "error patching backup")
	}

	_, err = utils.StoreRevisionUpdate(ctx.processedRestore, newRestore, "Restore")
	if err != nil {
		return newRestore, errors.Wrap(err, "Failed to updated processed restore cache")
	}

	return newRestore, nil
}

func (ctx *restoreContext) checkForRestoreResConflict(restore *kahuapi.Restore) error {
	Resources := ctx.resolveObjects
	resourceList := Resources.List()
	var restoreResource *unstructured.Unstructured

	for _, resource := range resourceList {
		switch unstructuredResource := resource.(type) {
		case *unstructured.Unstructured:
			restoreResource = unstructuredResource
		case unstructured.Unstructured:
			restoreResource = unstructuredResource.DeepCopy()
		default:
			ctx.logger.Warningf("Unknown cached resource type. %s", reflect.TypeOf(resource))
			continue
		}

		isResConflict := ctx.checkWorkloadConflict(restore, restoreResource)
		if isResConflict == true {
			msg := fmt.Sprintf("Resource(%s) kind(%s) namespace(%s) conflicts with existing resource label"+
				" in cluster ", restoreResource.GetName(), restoreResource.GetKind(), restoreResource.GetNamespace())
			ctx.logger.Errorf(msg)
			return errors.New(msg)
		}
	}

	return nil
}

func (ctx *restoreContext) patchVolRestoreContent(oldVrc, updatedVrc *kahuapi.VolumeRestoreContent) (*kahuapi.VolumeRestoreContent, error) {
	origBytes, err := json.Marshal(oldVrc)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling original vrc")
	}

	updatedBytes, err := json.Marshal(updatedVrc)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling updated vrc")
	}

	patchBytes, err := jsonpatch.CreateMergePatch(origBytes, updatedBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error creating json merge patch for vrc")
	}

	newVrc, err := ctx.restoreVolumeClient.Patch(context.TODO(),
		oldVrc.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "error patching vrc")
	}

	return newVrc, nil
}

func (ctx *restoreContext) updateRestoreStatus(restore *kahuapi.Restore) (*kahuapi.Restore, error) {
	// get restore status from lister
	updatedRestore, err := ctx.restoreClient.UpdateStatus(context.TODO(), restore,
		v1.UpdateOptions{})
	if err != nil {
		return restore, err
	}

	_, err = utils.StoreRevisionUpdate(ctx.processedRestore, updatedRestore, "Restore")
	if err != nil {
		return updatedRestore, errors.Wrap(err, "Failed to updated processed restore cache")
	}

	return updatedRestore, err
}

func (ctx *restoreContext) updateRestoreState(restore *kahuapi.Restore,
	state kahuapi.RestoreState) (*kahuapi.Restore, error) {
	restore.Status.State = state
	return ctx.updateRestoreStatus(restore)
}

func (ctx *restoreContext) updateRestoreStage(restore *kahuapi.Restore,
	stage kahuapi.RestoreStage) (*kahuapi.Restore, error) {
	restore.Status.Stage = stage
	return ctx.updateRestoreStatus(restore)
}

func (ctx *restoreContext) fetchBackup(restore *kahuapi.Restore) (*kahuapi.Backup, error) {
	// fetch backup
	backupName := restore.Spec.BackupName
	backup, err := ctx.backupLister.Get(backupName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("Backup(%s) do not exist", backupName)
			return nil, err
		}
		ctx.logger.Errorf("Failed to get backup. %s", err)
		return nil, err
	}

	return backup, err
}

func (ctx *restoreContext) fetchBackupLocation(locationName string) (*kahuapi.BackupLocation, error) {
	// fetch backup location
	backupLocation, err := ctx.backupLocationLister.Get(locationName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("Backup location(%s) do not exist", locationName)
			return nil, err
		}
		ctx.logger.Errorf("Failed to get backup location. %s", err)
		return nil, err
	}

	return backupLocation, err
}

func (ctx *restoreContext) fetchProvider(providerName string) (*kahuapi.Provider, error) {
	// fetch provider
	provider, err := ctx.providerLister.Get(providerName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ctx.logger.Errorf("Metadata Provider(%s) do not exist", providerName)
			return nil, err
		}
		ctx.logger.Errorf("Failed to get metadata provider. %s", err)
		return nil, err
	}

	return provider, nil
}

func (ctx *restoreContext) fetchBackupInfo(restore *kahuapi.Restore) (*backupInfo, error) {
	backup, err := ctx.fetchBackup(restore)
	if err != nil {
		ctx.logger.Errorf("Failed to get backup information for backup(%s). %s",
			restore.Spec.BackupName, err)
		return nil, fmt.Errorf("backup(%s) not available", restore.Spec.BackupName)
	}

	backupLocation, err := ctx.fetchBackupLocation(backup.Spec.MetadataLocation)
	if err != nil {
		ctx.logger.Errorf("Failed to get backup location information for %s. %s",
			backup.Spec.MetadataLocation, err)
		return nil, fmt.Errorf("backup location(%s) not available", backup.Spec.MetadataLocation)
	}

	provider, err := ctx.fetchProvider(backupLocation.Spec.ProviderName)
	if err != nil {
		ctx.logger.Errorf("Failed to get backup location provider for %s. %s",
			backupLocation.Spec.ProviderName, err)
		return nil, fmt.Errorf("backup provider(%s) not available", backupLocation.Spec.ProviderName)
	}

	return &backupInfo{
		backup:         backup,
		backupLocation: backupLocation,
		backupProvider: provider,
	}, nil
}

func (ctx *restoreContext) fetchMetaServiceClient(backupProvider *kahuapi.Provider,
	_ *kahuapi.Restore) (metaservice.MetaServiceClient, *grpc.ClientConn, error) {
	if backupProvider.Spec.Type != kahuapi.ProviderTypeMetadata {
		return nil, nil, fmt.Errorf("invalid metadata provider type (%s)",
			backupProvider.Spec.Type)
	}

	// fetch service name
	providerService, exist := backupProvider.Annotations[utils.BackupLocationServiceAnnotation]
	if !exist {
		return nil, nil, fmt.Errorf("failed to get metadata provider(%s) service info",
			backupProvider.Name)
	}

	metaServiceClient, grpcConn, err := metaservice.GetMetaServiceClient(providerService)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get metadata service client(%s)",
			providerService)
	}

	return metaServiceClient, grpcConn, nil
}

func (ctx *restoreContext) fetchBackupContent(backupProvider *kahuapi.Provider,
	backupIdentifier *metaservice.BackupIdentifier,
	restore *kahuapi.Restore) error {
	// fetch meta service client
	metaServiceClient, grpcConn, err := ctx.fetchMetaServiceClient(backupProvider, restore)
	if err != nil {
		ctx.logger.Errorf("Error fetching meta service client. %s", err)
		return err
	}
	defer grpcConn.Close()

	// fetch metadata backup file
	restoreClient, err := metaServiceClient.Restore(context.TODO(), &metaservice.RestoreRequest{
		Id: backupIdentifier,
	})
	if err != nil {
		ctx.logger.Errorf("Error fetching meta service restore client. %s", err)
		return fmt.Errorf("error fetching meta service restore client. %s", err)
	}

	var returnError error
	if err = getContent(ctx, restoreClient); err != nil {
		returnError = err
	}
	if err = restoreClient.CloseSend(); err != nil {
		returnError = err
	}

	return returnError
}

func getContent(ctx *restoreContext,
	restoreClient metaservice.MetaService_RestoreClient) error {
	for {
		res, err := restoreClient.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Errorf("Failed fetching data. %s", err)
			return errors.Wrap(err, "unable to receive backup resource meta")
		}

		obj := new(unstructured.Unstructured)
		err = json.Unmarshal(res.GetBackupResource().GetData(), obj)
		if err != nil {
			log.Errorf("Failed to unmarshal on backed up data %s", err)
			return errors.Wrap(err, "unable to unmarshal received resource meta")
		}

		ctx.logger.Infof("Received %s/%s from meta service", obj.GroupVersionKind(), obj.GetName())
		if ctx.excludeResource(obj) {
			ctx.logger.Infof("Excluding %s/%s from processing", obj.GroupVersionKind(), obj.GetName())
			continue
		}

		err = ctx.backupObjectIndexer.Add(obj)
		if err != nil {
			ctx.logger.Errorf("Unable to add resource %s/%s in restore cache. %s", obj.GroupVersionKind(),
				obj.GetName(), err)
			return err
		}
	}
	return nil
}

func (ctx *restoreContext) cloneIndexer() (cache.Indexer, error) {
	restoreObjectIndexer := cache.NewIndexer(uidKeyFunc, newBackupObjectIndexers())

	for _, restoreObj := range ctx.backupObjectIndexer.List() {
		err := restoreObjectIndexer.Add(restoreObj)
		if err != nil {
			ctx.logger.Errorf("Unable to clone backup objects for restore. %s", err)
			return restoreObjectIndexer, errors.Wrap(err, "unable to clone backup objects for restore")
		}
	}

	return restoreObjectIndexer, nil
}

func (ctx *restoreContext) excludeResource(resource *unstructured.Unstructured) bool {
	if excludeResources.Has(resource.GetKind()) {
		return true
	}

	switch resource.GetKind() {
	case "Service":
		if resource.GetName() == "kubernetes" {
			return true
		}
	}

	return false
}
