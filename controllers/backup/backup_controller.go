package backup

import (
	"log"
	"time"

	"github.com/soda-cdm/kahu/controllers"
	kahuclientset "github.com/soda-cdm/kahu/controllers/client/clientset/versioned"
	kinf "github.com/soda-cdm/kahu/controllers/client/informers/externalversions/kahu/v1beta1"
	kahulister "github.com/soda-cdm/kahu/controllers/client/listers/kahu/v1beta1"
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
		log.Println("cache was not sycned")
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
		log.Printf("error %s calling Namespace key func on cache for item", err.Error())
		return false
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		log.Printf("splitting key into namespace and name, error %s\n", err.Error())
		return false
	}

	bkp, err := c.kLister.Backups(ns).Get(name)
	if err != nil {
		log.Printf("error %s, Getting the backup resource from lister", err.Error())
		return false
	}
	log.Printf("Kahu backup spec: %+v\n", bkp.Spec)

	return true
}

func (c *Controller) handleAdd(obj interface{}) {
	log.Println("handleAdd was called")
	c.Wq.Add(obj)
}

func (c *Controller) handleDel(obj interface{}) {
	log.Println("handleDel was called")
	c.Wq.Add(obj)
}
