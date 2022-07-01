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

package backup

import (
	"encoding/json"
	"io/ioutil"

	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func (c *Controller) getResourceObjectsWIthOptions(gvr GroupResouceVersion, ns string, backup *PrepareBackup) (*unstructured.UnstructuredList, error) {
	var labelSelectors = map[string]string{}
	if backup.Spec.Label != nil {
		labelSelectors = backup.Spec.Label.MatchLabels
	}
	resourceObjectList, err := c.getResourceObjects(backup, gvr, ns, labelSelectors)
	if err != nil {
		c.logger.Errorf("unable to get object resource for deployments %s", err)
		return nil, err
	}
	return resourceObjectList, nil
}

func (c *Controller) unstructuredMarshalSend(gvr GroupResouceVersion,
	namespace string, backupClient metaservice.MetaService_BackupClient,
	resourceObjectsList *unstructured.UnstructuredList) error {
	resourceObjects, err := meta.ExtractList(resourceObjectsList)
	if err != nil {
		return err
	}

	for _, o := range resourceObjects {
		runtimeObject, ok := o.(runtime.Unstructured)
		if !ok {
			c.logger.Errorf("error casting object: %v", o)
			return err
		}

		metadata, err := meta.Accessor(runtimeObject)
		if err != nil {
			return err
		}

		resourceData, err := json.Marshal(metadata)
		if err != nil {
			c.logger.Errorf("Unable to get resource content: %s", err)
			return err
		}
		// file, _ := json.MarshalIndent(metadata, "", " ")
		filename := gvr.resourceName + "_" + metadata.GetName() + ".json"
		c.logger.Infoln("filename:", filename)
		_ = ioutil.WriteFile(filename, resourceData, 0644)

		c.backupSend(gvr, resourceData, namespace, backupClient)

	}
	return nil
}

func (c *Controller) marshalAndSend(gvr GroupResouceVersion,
	namespace string, backupClient metaservice.MetaService_BackupClient,
	resourceObjectsList *unstructured.UnstructuredList) error {
	resourceObjects, err := meta.ExtractList(resourceObjectsList)
	if err != nil {
		return err
	}

	for _, o := range resourceObjects {
		runtimeObject, ok := o.(runtime.Unstructured)
		if !ok {
			c.logger.Errorf("error casting object: %v", o)
			return err
		}

		metadata, err := meta.Accessor(runtimeObject)
		if err != nil {
			return err
		}

		resourceData, err := json.Marshal(metadata)
		if err != nil {
			c.logger.Errorf("Unable to get resource content: %s", err)
			return err
		}
		// file, _ := json.MarshalIndent(metadata, "", " ")
		filename := gvr.resourceName + "_" + metadata.GetName() + ".json"
		c.logger.Infoln("filename:", filename)
		_ = ioutil.WriteFile(filename, resourceData, 0644)

		c.backupSend(gvr, resourceData, namespace, backupClient)

	}
	return nil
}

func (c *Controller) resourcesBackup(gvr GroupResouceVersion, namespace string,
	backup *PrepareBackup, backupClient metaservice.MetaService_BackupClient) error {

	resourceObjectList, err := c.getResourceObjectsWIthOptions(gvr, namespace, backup)
	if err != nil {
		return err
	}

	return c.unstructuredMarshalSend(gvr, namespace, backupClient, resourceObjectList)
}
