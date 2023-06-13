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

package restorer

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
)

const (
	defaultMaxRetry = 3
)

type Interface interface {
	Run(ctx context.Context, workers int)
	Enqueue(obj string)
}

type processor struct {
	queue   workqueue.RateLimitingInterface
	logger  log.FieldLogger
	handler func(key string) error
}

func NewRestorer(logger log.FieldLogger, handler func(key string) error) Interface {
	return &processor{
		logger:  logger,
		handler: handler,
		queue: workqueue.NewNamedRateLimitingQueue(workqueue.NewMaxOfRateLimiter(
			workqueue.NewItemExponentialFailureRateLimiter(10*time.Second, 120*time.Second),
			// 10 qps, 100 bucket size.  This is only for retry speed and its only the overall factor (not per item)
			&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
		), "restorer")}
}

func (ctrl *processor) Run(ctx context.Context, workers int) {
	var wg sync.WaitGroup

	defer func() {
		ctrl.logger.Info("Waiting for workers to finish their work")

		// shut down worker queue
		ctrl.queue.ShutDown()

		// wait for workers to shut down
		wg.Wait()
		ctrl.logger.Info("All workers have finished")
	}()

	ctrl.logger.Info("Starting backup processor")
	defer ctrl.logger.Info("Shutting backup processor")

	if ctrl.handler != nil {
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func() {
				wait.Until(ctrl.runWorker, time.Second, ctx.Done())
				wg.Done()
			}()
		}
	}

	<-ctx.Done()
}

func (ctrl *processor) runWorker() {
	// continually take items off the queue
	for ctrl.processNextItem() {
	}
}

func (ctrl *processor) processNextItem() bool {
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
		ctrl.logger.WithError(err).WithField("restore", key).Error("Re-adding item to queue")
		ctrl.queue.AddRateLimited(key)
		return true
	}

	ctrl.logger.Infof("Dropping restore (%s) out of the queue. %s", key, err)
	ctrl.queue.Forget(key)
	return true
}

func (ctrl *processor) Enqueue(backupName string) {
	ctrl.queue.Add(backupName)
}
