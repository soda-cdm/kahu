package controllers

import (
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

type BaseController struct {
	Name     string
	Wq       workqueue.RateLimitingInterface
	Recorder record.EventRecorder
}

func NewBaseController(name string) *BaseController {
	c := &BaseController{
		Name: name,
		Wq:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), name),
	}

	return c
}
