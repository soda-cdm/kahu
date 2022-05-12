package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/soda-cdm/kahu/controllers/backup"
	kahulient "github.com/soda-cdm/kahu/controllers/client/clientset/versioned"
	kInfFac "github.com/soda-cdm/kahu/controllers/client/informers/externalversions"
)

func main() {
	var kubeconfig *string

	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Printf("Building config from flags failed, %s, trying to build inclusterconfig", err.Error())
	}

	klientset, err := kahulient.NewForConfig(config)
	if err != nil {
		log.Printf("getting klient set %s\n", err.Error())

	}
	fmt.Println("kclintset object:", klientset)

	infoFactory := kInfFac.NewSharedInformerFactory(klientset, 20*time.Minute)
	ch := make(chan struct{})
	c := backup.NewController(klientset, infoFactory.Kahu().V1beta1().Backups())

	infoFactory.Start(ch)
	if err := c.Run(ch); err != nil {
		log.Printf("error running controller %s\n", err.Error())
	}

}
