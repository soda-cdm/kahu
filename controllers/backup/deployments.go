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

	metaservice "github.com/soda-cdm/kahu/providerframework/metaservice/lib/go"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

var deploymentIDList = make([]types.UID, 0)
var replicaSetIDList = make([]types.UID, 0)

func (c *Controller) deploymentBackup(gvr GroupResouceVersion, namespace string,
	backup *PrepareBackup, backupClient metaservice.MetaService_BackupClient) error {

	deploymentObjectList, err := c.getResourceObjectsWIthOptions(gvr, namespace, backup)
	if err != nil {
		return err
	}
	// c.Logger.Infof("the objectList %+v\n", deploymentObjectList.Items)

	for _, deployment := range deploymentObjectList.Items {
		deploymentIDList = append(deploymentIDList, deployment.GetUID())
	}

	c.logger.Infof("the deployment uid %+v\n", deploymentIDList)

	// backup deployments dependent source:[replicaset, pods]
	err = c.getReplicasetByOwner(deploymentIDList, namespace, backup, backupClient)
	if err != nil {
		c.logger.Infof("unable to get replicasets for deployments. %s", err)
		return err
	}

	// backup deployments contenet
	err = c.marshalAndSend(gvr, namespace, backupClient, deploymentObjectList)
	if err != nil {
		return err
	}

	c.logger.Infof("the replicaSetIDList uid %+v\n", replicaSetIDList)

	return nil
}

func (c *Controller) getReplicasetByOwner(deploymentList []types.UID, namespace string,
	backup *PrepareBackup, backupClient metaservice.MetaService_BackupClient) error {

	replica_gvr := GroupResouceVersion{
		group:        "apps",
		version:      "v1",
		resourceName: "replicasets",
	}

	var labelSelectors = map[string]string{}

	replicaSetObjectList, err := c.getResourceObjects(backup, replica_gvr, namespace, labelSelectors)
	if err != nil {
		return nil
	}

	replicaSetObjects, err := meta.ExtractList(replicaSetObjectList)
	if err != nil {
		return err
	}

	for _, o := range replicaSetObjects {
		runtimeObject, ok := o.(runtime.Unstructured)
		if !ok {
			c.logger.Errorf("error casting object: %v", o)
			return err
		}

		replicaset, err := meta.Accessor(runtimeObject)
		if err != nil {
			return err
		}

		for _, deploymentUID := range deploymentList {

			for _, owner := range replicaset.GetOwnerReferences() {
				if owner.UID == deploymentUID {
					replicaSetIDList = append(replicaSetIDList, replicaset.GetUID())

					// now marshal and send replicaset content
					resourceData, err := json.Marshal(replicaset)
					if err != nil {
						c.logger.Errorf("Unable to get resource content: %s", err)
						return err
					}
					c.backupSend(replica_gvr, resourceData, namespace, backupClient)
				}
			}
		}
	}
	c.logger.Infof("the  replicaSetList %+v\n", replicaSetIDList)
	// once replicaset backup is done. we can backup all pods of replicasets of deployment
	return c.getPodsByOwner(replicaSetIDList, namespace, backupClient)
}
