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

package classsyncer

import (
	"context"
	"fmt"
	"sync"

	"github.com/soda-cdm/kahu/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	snapshotapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	snapshotclientset "github.com/kubernetes-csi/external-snapshotter/client/v4/clientset/versioned"
	snapshotinformer "github.com/kubernetes-csi/external-snapshotter/client/v4/informers/externalversions"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	kahuapi "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
)

type eventType string

const (
	controllerName = "SnapshotSyncer"

	providerNameIndex       = "provider.name"
	AnnSnapshotClassDefault = "snapshot.storage.kubernetes.io/is-default-class"
)

var (
	eventAdd    eventType = "Add"
	eventUpdate eventType = "Update"
	eventDelete eventType = "Delete"
)

// SnapshotClassSync syncs with server and make decision on SnapshotClass for the volume
type Interface interface {
	SnapshotClassByProvider(provider string) (*snapshotapi.VolumeSnapshotClass, error)
	SnapshotClassByVolume(volume kahuapi.ResourceReference) (*snapshotapi.VolumeSnapshotClass, error)
}

type snapshotClassSyncer struct {
	logger              log.FieldLogger
	lock                sync.Mutex
	kubeClient          kubernetes.Interface
	defaultSCByProvider map[string]*snapshotapi.VolumeSnapshotClass
}

func NewSnapshotClassSync(ctx context.Context,
	restConfig *rest.Config,
	kubeClient kubernetes.Interface) (Interface, error) {
	client, err := snapshotclientset.NewForConfig(restConfig)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	scSyncer := &snapshotClassSyncer{
		kubeClient:          kubeClient,
		defaultSCByProvider: make(map[string]*snapshotapi.VolumeSnapshotClass),
		logger:              log.WithField("controller", controllerName),
	}

	snapshotInformer := snapshotinformer.NewSharedInformerFactory(client, 0)
	snapshotInformer.
		Snapshot().
		V1().
		VolumeSnapshotClasses().
		Informer().
		AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					scSyncer.processSnapshotClass(eventAdd, obj)
				},
				UpdateFunc: func(oldObj, newObj interface{}) {
					scSyncer.processSnapshotClass(eventUpdate, newObj)
				},
				DeleteFunc: func(obj interface{}) {
					scSyncer.processSnapshotClass(eventDelete, obj)
				},
			})

	// start shared informer
	snapshotInformer.Start(ctx.Done())
	log.Info("Waiting for informer to sync")
	cacheSyncResults := snapshotInformer.WaitForCacheSync(ctx.Done())
	log.Info("Done waiting for informer to sync")
	for informer, synced := range cacheSyncResults {
		if !synced {
			return nil, errors.Errorf("cache was not synced for informer %v", informer)
		}
		log.WithField("resource", informer).Info("Informer cache synced")
	}

	return scSyncer, nil
}

func (syncer *snapshotClassSyncer) processSnapshotClass(eType eventType, obj interface{}) {
	var sc *snapshotapi.VolumeSnapshotClass
	switch class := obj.(type) {
	case *snapshotapi.VolumeSnapshotClass:
		sc = class
	case snapshotapi.VolumeSnapshotClass:
		sc = class.DeepCopy()
	default:
		syncer.logger.Warning("Invalid type store in SnapshotClass cache")
		return
	}

	switch eType {
	case eventAdd:
		syncer.processAdd(sc)
	case eventDelete:
		syncer.processDelete(sc)
	case eventUpdate:
		syncer.logger.Warnf("SC is immutable. Update events are not expected")
	}
}

func (syncer *snapshotClassSyncer) processAdd(sc *snapshotapi.VolumeSnapshotClass) {
	syncer.lock.Lock()
	defer syncer.lock.Unlock()

	provider := sc.Driver
	defaultSC, ok := syncer.defaultSCByProvider[provider]
	if ok {
		syncer.defaultSCByProvider[provider] = syncer.decideDefaultSC(defaultSC, sc)
	}
	syncer.defaultSCByProvider[provider] = sc

	syncer.logger.Infof("Selected Snapshot class %s for provider %s", sc.Name, provider)
}

func (syncer *snapshotClassSyncer) decideDefaultSC(defaultSC,
	sc *snapshotapi.VolumeSnapshotClass) *snapshotapi.VolumeSnapshotClass {
	// first check for annotation
	if utils.ContainsAnnotation(defaultSC, AnnSnapshotClassDefault) {
		return defaultSC
	}

	if utils.ContainsAnnotation(sc, AnnSnapshotClassDefault) {
		return defaultSC
	}

	if defaultSC.CreationTimestamp.After(sc.CreationTimestamp.UTC()) {
		return sc
	}

	return defaultSC
}

func (syncer *snapshotClassSyncer) processDelete(sc *snapshotapi.VolumeSnapshotClass) {
	syncer.lock.Lock()
	defer syncer.lock.Unlock()

	provider := sc.Driver
	defaultSC, ok := syncer.defaultSCByProvider[provider]
	if ok && defaultSC.Name == sc.Name { // SC are cluster scope resources. Only name check is enough
		delete(syncer.defaultSCByProvider, provider)
	}

}

func (syncer *snapshotClassSyncer) SnapshotClassByProvider(
	provider string) (*snapshotapi.VolumeSnapshotClass, error) {
	defaultSC, ok := syncer.defaultSCByProvider[provider]
	if ok {
		return defaultSC, nil
	}

	return nil, fmt.Errorf("default Snapshot class not available for %s", provider)
}

func (syncer *snapshotClassSyncer) SnapshotClassByVolume(
	volume kahuapi.ResourceReference) (*snapshotapi.VolumeSnapshotClass, error) {
	switch volume.Kind {
	case utils.PVC:
		return syncer.snapshotClassByPVC(volume.Name, volume.Namespace)
	case utils.PV:
		return syncer.snapshotClassByPV(volume.Name)
	default:
		return nil, fmt.Errorf("invalid volume kind (%s)", volume.Kind)
	}
}

func (syncer *snapshotClassSyncer) snapshotClassByPVC(name,
	namespace string) (*snapshotapi.VolumeSnapshotClass, error) {
	pvc, err := syncer.kubeClient.CoreV1().
		PersistentVolumeClaims(namespace).
		Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if pvc.Spec.VolumeName == "" {
		return nil, fmt.Errorf("pvc(%s/%s) is not bound", namespace, name)
	}

	return syncer.snapshotClassByPV(pvc.Spec.VolumeName)
}

func (syncer *snapshotClassSyncer) snapshotClassByPV(name string) (*snapshotapi.VolumeSnapshotClass, error) {
	pv, err := syncer.kubeClient.CoreV1().PersistentVolumes().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return syncer.SnapshotClassByProvider(utils.VolumeProvider(pv))
}
