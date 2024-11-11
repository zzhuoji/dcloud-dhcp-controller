package controller

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
)

type KeyString interface {
	comparable
	KeyString() string
}

type Worker[T KeyString] struct {
	Name     string
	Queue    workqueue.RateLimitingInterface
	SyncFunc func(context.Context, T) error
}

func (w *Worker[T]) Run(ctx context.Context, recoveryPanic bool, workers int) {
	//defer runtime.HandleCrash()
	//defer c.queue.ShutDown()
	log.Infof("(%s.Run) starting controller", w.Name)

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, w.runWorker(recoveryPanic), time.Second)
	}

	<-ctx.Done()
	w.Queue.ShutDown()
	log.Infof("(%s.Run) stopping controller", w.Name)
}

func (w *Worker[T]) runWorker(recoveryPanic bool) func(ctx context.Context) {
	return func(ctx context.Context) {
		for w.processNextItem(ctx, recoveryPanic) {
		}
	}
}

func (w *Worker[T]) processNextItem(ctx context.Context, recoveryPanic bool) (loop bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("(%s.processNextItem) panic: %v", w.Name, r)
			if recoveryPanic {
				for _, fn := range utilruntime.PanicHandlers {
					fn(r)
				}
				log.Infof("panic: %v [recovered]", r)
				loop = recoveryPanic
				return
			}
			panic(r)
		}
	}()

	loop = w.handler(ctx)
	return
}

func (w *Worker[T]) handler(ctx context.Context) bool {
	key, quit := w.Queue.Get()
	if quit {
		return false
	}
	defer w.Queue.Done(key)

	event, ok := key.(T)
	if !ok {
		w.Queue.Forget(key)
		return true
	}

	ctx = context.WithValue(ctx, "key", event.KeyString())

	if err := w.SyncFunc(ctx, event); err != nil {
		log.Errorf("(%s.handleErr) syncing <%s>: %v", w.Name, event.KeyString(), err)
		w.Queue.AddRateLimited(event)
	} else {
		w.Queue.Forget(event)
	}

	return true
}
