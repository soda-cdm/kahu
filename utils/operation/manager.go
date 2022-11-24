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
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	managerKey   = "context"
	managerValue = "operation-manager"

	defaultPollTime = 5 * time.Second
)

type Handler struct {
	Operation func(index string) (success bool, err error)
	OnFailure func(index string, err error)
	OnSuccess func(index string)
}

type Manager interface {
	Run(index string, handlers Handler) error
}

type manager struct {
	logger       log.FieldLogger
	ctx          context.Context
	ops          *operations
	pollInterval time.Duration
}

func NewOperationManager(ctx context.Context, logger log.FieldLogger) Manager {
	return &manager{
		logger:       logger.WithField(managerKey, managerValue),
		ctx:          ctx,
		ops:          newOperations(),
		pollInterval: defaultPollTime,
	}
}

func (mgr *manager) Run(index string, handler Handler) error {
	if _, ok := mgr.ops.get(index); ok {
		return NewAlreadyExistErr(index)
	}
	mgr.ops.add(index, handler)

	go func(index string, handler Handler) {
		mgr.operate(index, handler)
		mgr.ops.delete(index)
	}(index, handler)
	return nil
}

func (mgr *manager) operate(operationName string, handler Handler) {
	tick := time.NewTicker(mgr.pollInterval)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			opContinue := operationHandler(operationName, handler)
			if !opContinue {
				return
			}
		}
	}

}

func operationHandler(operationName string, handler Handler) bool {
	successful, err := handler.Operation(operationName)
	if err != nil {
		if handler.OnFailure != nil {
			handler.OnFailure(operationName, err)
		}
		return false
	}
	if successful {
		if handler.OnSuccess != nil {
			handler.OnSuccess(operationName)
		}
		return false
	}

	return true
}

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

// alreadyExistsError is the error returned by operation manager when a new operation
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

type operations struct {
	handlers map[string]Handler
	lock     sync.Mutex
}

func newOperations() *operations {
	return &operations{
		handlers: make(map[string]Handler, 0),
	}
}

func (op *operations) add(key string, handler Handler) {
	op.lock.Lock()
	defer op.lock.Unlock()
	op.handlers[key] = handler
}

func (op *operations) delete(key string) {
	op.lock.Lock()
	defer op.lock.Unlock()
	delete(op.handlers, key)
}

func (op *operations) get(key string) (Handler, bool) {
	op.lock.Lock()
	defer op.lock.Unlock()
	handler, ok := op.handlers[key]
	return handler, ok
}
