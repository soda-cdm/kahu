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

package utils

const (
	BackupLocationServiceAnnotation = "kahu.io/provider-service"
)

const (
	Pod         string = "Pod"
	Service     string = "Service"
	Deployment  string = "Deployment"
	Replicaset  string = "ReplicaSet"
	Statefulset string = "StatefulSet"
	Daemonset   string = "DaemonSet"
	Configmap   string = "ConfigMap"
	Secret      string = "Secret"
	Pvc         string = "PersistentVolumeClaim"
	Endpoint    string = "Endpoint"
	Sc          string = "StorageClass"
)

var SupportedResourceList = []string{Pod, Service, Deployment, Replicaset, Statefulset,
	Daemonset, Configmap, Secret, Pvc, Endpoint, Sc,
}
