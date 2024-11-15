package service

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	cache2 "tydic.io/dcloud-dhcp-controller/pkg/cache"
	"tydic.io/dcloud-dhcp-controller/pkg/controller"
)

type Controller struct {
	serviceLister *ServiceLister
	queue         workqueue.RateLimitingInterface
	recorder      record.EventRecorder
	networkCache  *cache2.NetworkCache
	podCache
	subnetNotify
	controller.Worker[Event]
}

func NewController(
	namespace string,
	factory informers.SharedInformerFactory,
	networkCache *cache2.NetworkCache,
	recorder record.EventRecorder,
	podCache podCache,
	subnetNotify subnetNotify,
) *Controller {
	serviceInformer := factory.InformerFor(&corev1.Service{}, func(k kubernetes.Interface, duration time.Duration) cache.SharedIndexInformer {
		watcher := cache.NewListWatchFromClient(k.CoreV1().RESTClient(), "services", namespace, fields.Everything())
		return cache.NewSharedIndexInformer(watcher, &corev1.Service{}, duration, cache.Indexers{
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
			MappingProviderIndex: MetaMappingProviderIndexFunc,
		})
	})
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	_, _ = serviceInformer.AddEventHandler(&ServiceEventHandler{queue: queue})
	serviceLister := NewServiceLister(serviceInformer.GetIndexer())
	c := &Controller{
		serviceLister: serviceLister,
		networkCache:  networkCache,
		queue:         queue,
		recorder:      recorder,
		podCache:      podCache,
		subnetNotify:  subnetNotify,
	}
	c.Worker = controller.Worker[Event]{
		Name:     "service",
		Queue:    queue,
		SyncFunc: c.sync,
	}
	return c
}

func (c *Controller) sync(ctx context.Context, event Event) error {
	switch event.Operation {
	case ADD, UPDATE:
		svc, err := c.serviceLister.Services(event.ObjKey.Namespace).Get(event.ObjKey.Name)
		if errors.IsNotFound(err) {
			log.Infof("(service.sync) Service <%s> does not exist anymore", event.KeyString())
			return nil
		} else if err != nil {
			log.Errorf("(service.sync) fetching object with key <%s> from store failed with %v", event.KeyString(), err)
			return err
		}
		log.Infof("(service.sync) Handler %s Service %s", event.Operation, event.KeyString())
		if err = c.HandlerCreateOrUpdate(ctx, event.ObjKey, svc); err != nil {
			log.Errorf("(service.sync) Handler %s Service <%s> failed: %v", event.Operation, event.KeyString(), err)
			return err
		}
	case DELETE:
		log.Infof("(service.sync) Handler delete Service <%s>", event.KeyString())
		if err := c.HandlerDelete(ctx, event.Provider, event.ObjKey); err != nil {
			log.Errorf("(service.sync) Handler delete Service <%s> failed: %v", event.KeyString(), err)
			return err
		}
	}
	return nil
}
