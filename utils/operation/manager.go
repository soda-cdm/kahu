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

package operation

import (
	"fmt"
	"sync"
)

// IsAlreadyExists returns true if an error returned from operation manager already exist
func IsAlreadyExists(err error) bool {
	switch err.(type) {
	case alreadyExistsError:
		return true
	default:
		return false
	}
}

func NewAlreadyExistErr(index string) error {
	return alreadyExistsError{
		operationName: index,
	}
}

// alreadyExistsError is the error returned by operation when a new operation
// can not be started because an operation with the same operation name is
// already exist.
type alreadyExistsError struct {
	operationName string
}

func (err alreadyExistsError) Error() string {
	return fmt.Sprintf(
		"Failed to create operation with name %q. An operation with that name is already exist.",
		err.operationName)
}

type Manager interface {
	Add(key string)
	Delete(key string)
	Exist(key string) bool
}

type operations struct {
	handlers map[string]struct{}
	lock     sync.Mutex
}

func NewOperationManager() Manager {
	return &operations{
		handlers: make(map[string]struct{}, 0),
	}
}

func (op *operations) Add(key string) {
	op.lock.Lock()
	defer op.lock.Unlock()
	op.handlers[key] = struct{}{}
}

func (op *operations) Delete(key string) {
	op.lock.Lock()
	defer op.lock.Unlock()
	delete(op.handlers, key)
}

func (op *operations) Exist(key string) bool {
	op.lock.Lock()
	defer op.lock.Unlock()
	_, ok := op.handlers[key]
	return ok
}
