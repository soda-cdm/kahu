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

// FakeRestores implements RestoreInterface
type FakeRestores struct {
	Fake *FakeKahuV1beta1
}

var restoresResource = schema.GroupVersionResource{Group: "kahu.io", Version: "v1beta1", Resource: "restores"}

var restoresKind = schema.GroupVersionKind{Group: "kahu.io", Version: "v1beta1", Kind: "Restore"}

// Get takes name of the restore, and returns the corresponding restore object, and an error if there is any.
func (c *FakeRestores) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1beta1.Restore, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(restoresResource, name), &v1beta1.Restore{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta1.Restore), err
}

// List takes label and field selectors, and returns the list of Restores that match those selectors.
func (c *FakeRestores) List(ctx context.Context, opts v1.ListOptions) (result *v1beta1.RestoreList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(restoresResource, restoresKind, opts), &v1beta1.RestoreList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1beta1.RestoreList{ListMeta: obj.(*v1beta1.RestoreList).ListMeta}
	for _, item := range obj.(*v1beta1.RestoreList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested restores.
func (c *FakeRestores) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(restoresResource, opts))
}

// Create takes the representation of a restore and creates it.  Returns the server's representation of the restore, and an error, if there is any.
func (c *FakeRestores) Create(ctx context.Context, restore *v1beta1.Restore, opts v1.CreateOptions) (result *v1beta1.Restore, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(restoresResource, restore), &v1beta1.Restore{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta1.Restore), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeRestores) UpdateStatus(ctx context.Context, restore *v1beta1.Restore, opts v1.UpdateOptions) (*v1beta1.Restore, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(restoresResource, "status", restore), &v1beta1.Restore{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta1.Restore), err
}

// Delete takes name of the restore and deletes it. Returns an error if one occurs.
func (c *FakeRestores) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteAction(restoresResource, name), &v1beta1.Restore{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeRestores) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(restoresResource, listOpts)

	_, err := c.Fake.Invokes(action, &v1beta1.RestoreList{})
	return err
}
