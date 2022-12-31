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
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	defaultReSyncPeriod = 5 * time.Minute
	defaultMaxRetry     = 3
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

type controllerBuilder struct {
	controller
}

func NewControllerBuilder(name string) ControllerBuilder {
	return &controllerBuilder{
		controller{
			name: name,
		},
	}
}

func (builder *controllerBuilder) SetLogger(logger log.FieldLogger) ControllerBuilder {
	builder.logger = logger
	return builder
}

func (builder *controllerBuilder) SetHandler(handler func(key string) error) ControllerBuilder {
	builder.handler = handler
	return builder
}

func (builder *controllerBuilder) SetReSyncHandler(handler func()) ControllerBuilder {
	builder.reSyncHandler = handler
	// add default re sync period
	builder.reSyncPeriod = defaultReSyncPeriod
	return builder
}

func (builder *controllerBuilder) SetReSyncPeriod(period time.Duration) ControllerBuilder {
	builder.reSyncPeriod = period
	return builder
}

func (builder *controllerBuilder) SetInformerSyncWaiters(waiters ...cache.InformerSynced) ControllerBuilder {
	builder.cacheSyncWaiters = append(builder.cacheSyncWaiters, waiters...)
	return builder
}

func (builder *controllerBuilder) Build() (Controller, error) {
	if builder.handler == nil && builder.reSyncHandler == nil {
		return nil, fmt.Errorf("both handler and resync handler canot be empty")
	}

	if builder.logger == nil {
		builder.logger = log.WithField("controller", builder.name)
	}

	return &controller{
		name: builder.name,
		queue: workqueue.NewNamedRateLimitingQueue(workqueue.NewMaxOfRateLimiter(
			workqueue.NewItemExponentialFailureRateLimiter(1*time.Second, 120*time.Second),
			// 10 qps, 100 bucket size.  This is only for retry speed and its only the overall factor (not per item)
			&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(10), 25)},
		),
			builder.name),
		logger:           builder.logger,
		handler:          builder.handler,
		reSyncHandler:    builder.reSyncHandler,
		reSyncPeriod:     builder.reSyncPeriod,
		cacheSyncWaiters: builder.cacheSyncWaiters,
	}, nil
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
	defer ctrl.queue.Done(key)

	err := ctrl.handler(key.(string))
	if err == nil {
		ctrl.queue.Forget(key)
		return true
	}

	if ctrl.queue.NumRequeues(key) < defaultMaxRetry {
		ctrl.logger.WithError(err).WithField("controller", ctrl.name).
			WithField("resource", key).Error("Re-adding item  to queue")
		ctrl.queue.AddRateLimited(key)
		return true
	}

	ctrl.logger.Infof("Dropping %s (%s) out of the queue. %s", ctrl.name, key, err)
	ctrl.queue.Forget(key)
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
