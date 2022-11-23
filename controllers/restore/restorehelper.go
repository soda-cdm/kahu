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

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

func (handler *prefixMutation) updatePodContent(prefixString string, unstructuredResource *unstructured.Unstructured) *unstructured.Unstructured {

	unstructuredResource = handler.serviceAccountSectionUpdate(prefixString, unstructuredResource)

	if unstructuredResource == nil {
		return nil
	}

	volumes, found, err := unstructured.NestedSlice(unstructuredResource.UnstructuredContent(), "spec", "volumes")
	if !found || err != nil {
		handler.logger.Warningf("volumes section not found in spec")
		return unstructuredResource
	}

	return handler.volumesSectionUpdate(prefixString, volumes, unstructuredResource)
}

func (handler *prefixMutation) serviceAccountSectionUpdate(prefixString string, unstructuredResource *unstructured.Unstructured) *unstructured.Unstructured {
	handler.logger.Infof("started adding prefix to serviceaccount")
	kind := unstructuredResource.GetKind()
	var err error
	var sa string
	var found bool
	if kind == "Pod" {
		sa, found, err = unstructured.NestedString(unstructuredResource.UnstructuredContent(), "spec", "serviceAccountName")
	} else {
		sa, found, err = unstructured.NestedString(unstructuredResource.UnstructuredContent(), "spec", "template", "spec", "serviceAccountName")
	}
	if !found || err != nil {
		handler.logger.Warningf("serviceaccount not found!")
		return unstructuredResource
	}

	var newSa string
	if sa == "default" {
		handler.logger.Infof("serviceaccount is default in pod spec. so ignoring!!!")
	} else {
		newSa = prefixString + sa
		handler.logger.Infof("adding prefix to serviceaccount. so newname is :%s", newSa)
		if kind == "Pod" {
			err = unstructured.SetNestedField(unstructuredResource.UnstructuredContent(), newSa, "spec", "serviceAccountName")
		} else {
			err = unstructured.SetNestedField(unstructuredResource.UnstructuredContent(), newSa, "spec", "template", "spec", "serviceAccountName")
		}
		if err != nil {
			handler.logger.Errorf("unable to add prefix to serviceAccountName with error:%s", err)
			return nil
		}
	}

	return unstructuredResource
}

func (handler *prefixMutation) volumesSectionUpdate(prefixString string, volumes []interface{}, unstructuredResource *unstructured.Unstructured) *unstructured.Unstructured {

	if volumes == nil {
		handler.logger.Errorf("skipping as volumes section of spec is empty.")
		return nil
	}
	handler.logger.Infof("started adding prefix to volumes section")
	var err error
	kind := unstructuredResource.GetKind()
	for _, v := range volumes {
		// add prefix string to configmap name
		if v.(map[string]interface{})["configMap"] != nil {
			configMapName := v.(map[string]interface{})["configMap"].(map[string]interface{})["name"]
			v.(map[string]interface{})["configMap"].(map[string]interface{})["name"] = prefixString + configMapName.(string)
			if kind == "Pod" {
				err = unstructured.SetNestedSlice(unstructuredResource.UnstructuredContent(), volumes, "spec", "volumes")
			} else {
				err = unstructured.SetNestedSlice(unstructuredResource.UnstructuredContent(), volumes, "spec", "template", "spec", "volumes")
			}
			if err != nil {
				handler.logger.Errorf("unable to add prefix to configMap with error:%s", err)
				return nil
			}
		}
		// add prefix string to secret name
		if v.(map[string]interface{})["secret"] != nil {
			secretName := v.(map[string]interface{})["secret"].(map[string]interface{})["secretName"]
			handler.logger.Infof("the secret name: %s", secretName)
			v.(map[string]interface{})["secret"].(map[string]interface{})["secretName"] = prefixString + secretName.(string)
			if kind == "Pod" {
				err = unstructured.SetNestedSlice(unstructuredResource.UnstructuredContent(), volumes, "spec", "volumes")
			} else {
				err = unstructured.SetNestedSlice(unstructuredResource.UnstructuredContent(), volumes, "spec", "template", "spec", "volumes")
			}
			if err != nil {
				handler.logger.Errorf("unable to add prefix to secretname with error:%s", err)
				return nil
			}
		}
		// add prefix string to pvc name
		if v.(map[string]interface{})["persistentVolumeClaim"] != nil {
			claimName := v.(map[string]interface{})["persistentVolumeClaim"].(map[string]interface{})["claimName"]
			v.(map[string]interface{})["persistentVolumeClaim"].(map[string]interface{})["claimName"] = prefixString + claimName.(string)
			if kind == "Pod" {
				err = unstructured.SetNestedSlice(unstructuredResource.UnstructuredContent(), volumes, "spec", "volumes")
			} else {
				err = unstructured.SetNestedSlice(unstructuredResource.UnstructuredContent(), volumes, "spec", "template", "spec", "volumes")
			}
			if err != nil {
				handler.logger.Errorf("unable to add prefix to persistentVolumeClaim with error:%s", err)
				return nil
			}
		}

	}
	return unstructuredResource
}

func (handler *prefixMutation) updatePVCContent(prefixString string, unstructuredResource *unstructured.Unstructured) *unstructured.Unstructured {
	handler.logger.Infof("started adding prefix to storageclass of PVC")
	sc, found, err := unstructured.NestedString(unstructuredResource.UnstructuredContent(), "spec", "storageClassName")
	if !found || err != nil {
		handler.logger.Warningf("storageclass not found!")
		return unstructuredResource
	}
	handler.logger.Infof("storage class name:%s", sc)
	if sc != "" {
		err := unstructured.SetNestedField(unstructuredResource.UnstructuredContent(), prefixString+sc, "spec", "storageClassName")
		if err != nil {
			handler.logger.Errorf("unable to add prefix to persistentVolumeClaim with error:%s", err)
			return nil
		}
	}
	return unstructuredResource
}

func (handler *prefixMutation) updateDeploymentContent(prefixString string, unstructuredResource *unstructured.Unstructured) *unstructured.Unstructured {
	handler.logger.Infof("started adding prefix to deployment resource")
	unstructuredResource = handler.serviceAccountSectionUpdate(prefixString, unstructuredResource)

	if unstructuredResource == nil {
		return nil
	}

	volumes, found, err := unstructured.NestedSlice(unstructuredResource.UnstructuredContent(), "spec", "template", "spec", "volumes")
	if !found || err != nil {
		handler.logger.Warningf("volumes section not found in spec")
		return unstructuredResource
	}
	return handler.volumesSectionUpdate(prefixString, volumes, unstructuredResource)
}

func (handler *prefixMutation) volumeClaimTemplatesUpdate(prefixString string, volumeClaimTemplates []interface{}, unstructuredResource *unstructured.Unstructured) *unstructured.Unstructured {

	if volumeClaimTemplates == nil {
		handler.logger.Errorf("skipping as volumeClaimTemplates section of spec is empty.")
		return nil
	}
	handler.logger.Infof("started adding prefix to volumes section")
	for _, v := range volumeClaimTemplates {
		// Update the pvc name
		pvcName, found, err := unstructured.NestedString(v.(map[string]interface{}), "metadata", "name")
		if !found || err != nil {
			handler.logger.Warningf("pvcName not found!")
			return unstructuredResource
		}
		v.(map[string]interface{})["metadata"].(map[string]interface{})["name"] = prefixString + pvcName
		err = unstructured.SetNestedSlice(unstructuredResource.UnstructuredContent(), volumeClaimTemplates, "spec", "volumeClaimTemplates")
		if err != nil {
			handler.logger.Errorf("unable to add prefix to persistentVolumeClaim with error:%s", err)
			return nil
		}
		// update storageclass
		sc, found, err := unstructured.NestedString(v.(map[string]interface{}), "spec", "storageClassName")
		if !found || err != nil {
			handler.logger.Warningf("storageclass not found!")
			return unstructuredResource
		}
		handler.logger.Infof("storage class name:%s", sc)
		v.(map[string]interface{})["spec"].(map[string]interface{})["storageClassName"] = prefixString + sc
		err = unstructured.SetNestedSlice(unstructuredResource.UnstructuredContent(), volumeClaimTemplates, "spec", "volumeClaimTemplates")
		if err != nil {
			handler.logger.Errorf("unable to add prefix to persistentVolumeClaim  sc with error:%s", err)
			return nil
		}
	}

	return unstructuredResource
}

func (handler *prefixMutation) containersSectionUpdate(prefixString string, containers []interface{}, unstructuredResource *unstructured.Unstructured) *unstructured.Unstructured {

	if containers == nil {
		handler.logger.Errorf("skipping as containers section of spec is empty.")
		return nil
	}
	handler.logger.Infof("started adding prefix to containers section")
	for _, c := range containers {
		vmounts, found, err := unstructured.NestedSlice(c.(map[string]interface{}), "volumeMounts")
		if !found || err != nil {
			handler.logger.Warningf("vmountName not found!")
			return unstructuredResource
		}
		for _, vmount := range vmounts {
			vname, found, err := unstructured.NestedString(vmount.(map[string]interface{}), "name")
			if !found || err != nil {
				handler.logger.Warningf("vname not found!")
				return unstructuredResource
			}
			vmount.(map[string]interface{})["name"] = prefixString + vname
		}
		c.(map[string]interface{})["volumeMounts"] = vmounts
		err = unstructured.SetNestedSlice(unstructuredResource.UnstructuredContent(), containers, "spec", "template", "spec", "containers")
		if err != nil {
			handler.logger.Errorf("unable to add prefix to volumeMounts with error:%s", err)
			return nil
		}
	}
	return unstructuredResource
}

func (handler *prefixMutation) updateStatefulSetContent(prefixString string, unstructuredResource *unstructured.Unstructured) *unstructured.Unstructured {
	handler.logger.Infof("started adding prefix to statefulset resource")
	unstructuredResource = handler.serviceAccountSectionUpdate(prefixString, unstructuredResource)
	if unstructuredResource == nil {
		return nil
	}

	volumeClaimTemplates, found, err := unstructured.NestedSlice(unstructuredResource.UnstructuredContent(), "spec", "volumeClaimTemplates")
	if !found || err != nil {
		handler.logger.Warningf("volumeClaimTemplates section not found in spec")
		return unstructuredResource
	}
	// get containers section
	containers, found, err := unstructured.NestedSlice(unstructuredResource.UnstructuredContent(), "spec", "template", "spec", "containers")
	if !found || err != nil {
		handler.logger.Warningf("containers section not found in spec")
		return unstructuredResource
	}
	unstructuredResource = handler.volumeClaimTemplatesUpdate(prefixString, volumeClaimTemplates, unstructuredResource)
	return handler.containersSectionUpdate(prefixString, containers, unstructuredResource)
}
