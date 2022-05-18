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

package backup

import (
	"fmt"
	"time"

	"github.com/soda-cdm/kahu/controllers"
	kahuclientset "github.com/soda-cdm/kahu/controllers/client/clientset/versioned"
	kinf "github.com/soda-cdm/kahu/controllers/client/informers/externalversions/kahu/v1beta1"
	kahulister "github.com/soda-cdm/kahu/controllers/client/listers/kahu/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type Controller struct {
	*controllers.BaseController
	client       kubernetes.Interface
	klient       kahuclientset.Interface
	backupSynced cache.InformerSynced
	kLister      kahulister.BackupLister
}

func NewController(klient kahuclientset.Interface, backupInformer kinf.BackupInformer) *Controller {

	c := &Controller{
		BaseController: controllers.NewBaseController(controllers.Backup),
		klient:         klient,
		backupSynced:   backupInformer.Informer().HasSynced,
		kLister:        backupInformer.Lister(),
	}

	backupInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.handleAdd,
			DeleteFunc: c.handleDel,
		},
	)

	return c
}

func (c *Controller) Run(ch chan struct{}) error {
	if ok := cache.WaitForCacheSync(ch, c.backupSynced); !ok {
		c.Logger.Infoln("cache was not sycned")
	}

	go wait.Until(c.worker, time.Second, ch)

	<-ch
	return nil
}

func (c *Controller) worker() {
	for c.processNextItem() {

	}
}

func (c *Controller) processNextItem() bool {

	item, shutDown := c.Wq.Get()
	if shutDown {
		return false
	}

	defer c.Wq.Forget(item)
	key, err := cache.MetaNamespaceKeyFunc(item)
	if err != nil {
		c.Logger.Errorf("error %s calling Namespace key func on cache for item", err.Error())
		return false
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("splitting key into namespace and name, error %s\n", err.Error())
		return false
	}

	bkp, err := c.kLister.Backups(ns).Get(name)
	if err != nil {
		c.Logger.Errorf("error %s, Getting the backup resource from lister", err.Error())
		return false
	}

	c.Logger.Infof("Kahu backup spec: %+v\n", bkp.Spec)

	return true
}

// NamespaceAndName returns a string in the format <namespace>/<name>
func NamespaceAndName(objMeta metav1.Object) string {
	if objMeta.GetNamespace() == "" {
		return objMeta.GetName()
	}
	return fmt.Sprintf("%s/%s", objMeta.GetNamespace(), objMeta.GetName())
}

func (c *Controller) handleAdd(obj interface{}) {
	c.Logger.Infoln("handleAdd was called")
	c.Wq.Add(obj)
}

func (c *Controller) handleDel(obj interface{}) {
	c.Logger.Infoln("handleDel was called")
	c.Wq.Add(obj)
}
