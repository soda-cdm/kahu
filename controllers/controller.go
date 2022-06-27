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

package controllers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	defaultReSyncPeriod = 5 * time.Minute
)

type Controller interface {
	Run(ctx context.Context, workers int) error
	Name() string
	Enqueue(obj interface{})
}

type ControllerBuilder interface {
	SetHandler(handler func(key string) error) ControllerBuilder
	SetLogger(logger log.FieldLogger) ControllerBuilder
	SetReSyncHandler(handler func()) ControllerBuilder
	SetReSyncPeriod(period time.Duration) ControllerBuilder
	Build() (Controller, error)
}

type controller struct {
	name             string
	queue            workqueue.RateLimitingInterface
	logger           log.FieldLogger
	handler          func(key string) error
	reSyncHandler    func()
	reSyncPeriod     time.Duration
	cacheSyncWaiters []cache.InformerSynced
}

func NewControllerBuilder(name string) ControllerBuilder {
	return &controller{
		name:   name,
		queue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), name),
		logger: log.StandardLogger(),
	}
}

func (ctrl *controller) SetLogger(logger log.FieldLogger) ControllerBuilder {
	ctrl.logger = logger
	return ctrl
}

func (ctrl *controller) SetHandler(handler func(key string) error) ControllerBuilder {
	ctrl.handler = handler
	return ctrl
}

func (ctrl *controller) SetReSyncHandler(handler func()) ControllerBuilder {
	ctrl.reSyncHandler = handler
	// add default re sync period
	ctrl.reSyncPeriod = defaultReSyncPeriod
	return ctrl
}

func (ctrl *controller) SetReSyncPeriod(period time.Duration) ControllerBuilder {
	ctrl.reSyncPeriod = period
	return ctrl
}

func (ctrl *controller) SetInformerSyncWaiters(waiters ...cache.InformerSynced) ControllerBuilder {
	ctrl.cacheSyncWaiters = append(ctrl.cacheSyncWaiters, waiters...)
	return ctrl
}

func (ctrl *controller) Build() (Controller, error) {
	if ctrl.handler == nil && ctrl.reSyncHandler == nil {
		return nil, fmt.Errorf("both handler and resync handler canot be empty")
	}
	return ctrl, nil
}

func (ctrl *controller) Name() string {
	return ctrl.name
}

func (ctrl *controller) Run(ctx context.Context, workers int) error {
	var wg sync.WaitGroup

	defer func() {
		ctrl.logger.Info("Waiting for workers to finish their work")

		// shut down worker queue
		ctrl.queue.ShutDown()

		// wait for workers to shut down
		wg.Wait()
		ctrl.logger.Info("All workers have finished")
	}()

	ctrl.logger.Infof("Starting controller %s", ctrl.name)
	defer ctrl.logger.Infof("Shutting down controller %s", ctrl.name)

	// only want to log about cache sync waiters if there are any
	if len(ctrl.cacheSyncWaiters) > 0 {
		ctrl.logger.Info("Waiting for caches to sync")
		if !cache.WaitForCacheSync(ctx.Done(), ctrl.cacheSyncWaiters...) {
			return errors.New("timed out waiting for caches to sync")
		}
		ctrl.logger.Info("Caches are synced")
	}

	if ctrl.handler != nil {
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func() {
				wait.Until(ctrl.runWorker, time.Second, ctx.Done())
				wg.Done()
			}()
		}
	}

	if ctrl.reSyncHandler != nil {
		if ctrl.reSyncPeriod == 0 {
			// Programmer error
			panic("non-zero resyncPeriod is required")
		}

		wg.Add(1)
		go func() {
			wait.Until(ctrl.reSyncHandler, ctrl.reSyncPeriod, ctx.Done())
			wg.Done()
		}()
	}

	<-ctx.Done()

	return nil
}

func (ctrl *controller) runWorker() {
	// continually take items off the queue
	for ctrl.processNextItem() {
	}
}

func (ctrl *controller) processNextItem() bool {
	key, quit := ctrl.queue.Get()
	if quit {
		return false
	}
	// always call done on this item, since if it fails we'll add
	// it back with rate-limiting below
	defer ctrl.queue.Done(key)

	err := ctrl.handler(key.(string))
	if err == nil {
		ctrl.queue.Forget(key)
		return true
	}

	ctrl.logger.WithError(err).WithField("key", key).Error("Error in handler, " +
		"re-adding item to queue")
	ctrl.queue.AddRateLimited(key)

	return true
}

func (ctrl *controller) Enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		ctrl.logger.WithError(errors.WithStack(err)).
			Error("Error creating worker queue key, item not added to queue")
		return
	}

	ctrl.queue.Add(key)
}
