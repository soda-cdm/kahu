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

package main

import (
	"flag"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"

	utils "github.com/soda-cdm/kahu/utils"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/soda-cdm/kahu/controllers/backup"
	kahuClient "github.com/soda-cdm/kahu/controllers/client/clientset/versioned"
	kahuInformer "github.com/soda-cdm/kahu/controllers/client/informers/externalversions"
)

func main() {
	// enable log with timestamp
	utils.EnableLogTimeStamp()

	var kubeconfig *string

	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Errorf("Building config from flags failed, %s, trying to build inclusterconfig", err.Error())
	}

	klientset, err := kahuClient.NewForConfig(config)
	if err != nil {
		log.Errorf("getting klient set %s\n", err.Error())

	}
	log.Debug("kclintset object:", klientset)

	infoFactory := kahuInformer.NewSharedInformerFactory(klientset, 20*time.Minute)
	ch := make(chan struct{})
	c := backup.NewController(klientset, infoFactory.Kahu().V1beta1().Backups())

	infoFactory.Start(ch)
	if err := c.Run(ch); err != nil {
		log.Errorf("error running controller %s\n", err.Error())
	}

}
