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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1beta1 "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeBackupLocations implements BackupLocationInterface
type FakeBackupLocations struct {
	Fake *FakeKahuV1beta1
}

var backuplocationsResource = schema.GroupVersionResource{Group: "kahu.io", Version: "v1beta1", Resource: "backuplocations"}

var backuplocationsKind = schema.GroupVersionKind{Group: "kahu.io", Version: "v1beta1", Kind: "BackupLocation"}

// Get takes name of the backupLocation, and returns the corresponding backupLocation object, and an error if there is any.
func (c *FakeBackupLocations) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1beta1.BackupLocation, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(backuplocationsResource, name), &v1beta1.BackupLocation{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta1.BackupLocation), err
}

// List takes label and field selectors, and returns the list of BackupLocations that match those selectors.
func (c *FakeBackupLocations) List(ctx context.Context, opts v1.ListOptions) (result *v1beta1.BackupLocationList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(backuplocationsResource, backuplocationsKind, opts), &v1beta1.BackupLocationList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1beta1.BackupLocationList{ListMeta: obj.(*v1beta1.BackupLocationList).ListMeta}
	for _, item := range obj.(*v1beta1.BackupLocationList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested backupLocations.
func (c *FakeBackupLocations) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(backuplocationsResource, opts))
}

// Create takes the representation of a backupLocation and creates it.  Returns the server's representation of the backupLocation, and an error, if there is any.
func (c *FakeBackupLocations) Create(ctx context.Context, backupLocation *v1beta1.BackupLocation, opts v1.CreateOptions) (result *v1beta1.BackupLocation, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(backuplocationsResource, backupLocation), &v1beta1.BackupLocation{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta1.BackupLocation), err
}

// Delete takes name of the backupLocation and deletes it. Returns an error if one occurs.
func (c *FakeBackupLocations) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteActionWithOptions(backuplocationsResource, name, opts), &v1beta1.BackupLocation{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeBackupLocations) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(backuplocationsResource, listOpts)

	_, err := c.Fake.Invokes(action, &v1beta1.BackupLocationList{})
	return err
}
