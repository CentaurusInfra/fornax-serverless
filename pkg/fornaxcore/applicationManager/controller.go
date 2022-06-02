package applicationManager

import (
	v1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"time"
)

type controller struct {
	queue workqueue.RateLimitingInterface
}

func (c *controller) enqueue(app *v1.Application) {
	key := genKey(app.Namespace, app.Name)
	c.queue.Add(key)
}

func (c *controller) Run(ctx context.Context, workers int) {
	defer c.queue.ShutDown()
	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	<-ctx.Done()
}

func (c *controller) runWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

// processNextWorkItem deals with one key off the queue.  It returns false when it's time to quit.
func (c *controller) processNextWorkItem(ctx context.Context) bool {
	dsKey, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(dsKey)

	err := c.syncHandler(ctx, dsKey.(string))
	if err == nil {
		c.queue.Forget(dsKey)
		return true
	}

	c.queue.AddRateLimited(dsKey)
	return true
}

func (c *controller) syncHandler(ctx context.Context, s string) error {
	return fmt.Errorf("to impl")
}

func NewController() *controller {
	return &controller{
		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "fleet"),
	}
}
