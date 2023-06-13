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

package group

import (
	"crypto/md5"
	"encoding/hex"
	"k8s.io/apimachinery/pkg/util/sets"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/soda-cdm/kahu/utils/k8sresource"
)

type Interface interface {
	GetResources() []k8sresource.Resource
	GetGroupName() string
	GetProvisionerName() string
}

type group struct {
	resources   []k8sresource.Resource
	provisioner string
	groupName   string
}

func newGroup(provisioner string,
	resources []k8sresource.Resource) Interface {

	g := &group{
		resources:   resources,
		provisioner: provisioner,
	}

	// sort group by name
	names := sets.NewString()
	for _, resource := range g.resources {
		names.Insert(resource.GetName())
	}
	sort.Strings(names.List())
	// concat names for GUID
	concatNames := strings.Join(names.List(), "")
	g.groupName = deterministicGUID(concatNames)

	return g
}

func (g *group) GetResources() []k8sresource.Resource {
	// always return true as volume selector not supported yet
	return g.resources
}

func (g *group) GetProvisionerName() string {
	return g.provisioner
}

func (g *group) GetGroupName() string {
	// use only first 10 resource names for UID
	return g.groupName
}

func deterministicGUID(names string) string {
	// calculate the MD5 hash of the resources names
	md5hash := md5.New()
	md5hash.Write([]byte(names))

	// convert the hash value to a string
	md5string := hex.EncodeToString(md5hash.Sum(nil))

	// generate the UUID from the
	// first 16 bytes of the MD5 hash
	guid, err := uuid.FromBytes([]byte(md5string[0:16]))
	if err != nil {
		return md5string[0:16]
	}

	return guid.String()
}
