package pod

import (
	"context"
	"time"

	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	dhcpv4 "tydic.io/dcloud-dhcp-controller/pkg/dhcp"
	"tydic.io/dcloud-dhcp-controller/pkg/metrics"
)

type Controller struct {
	podLister    listerv1.PodLister
	queue        workqueue.RateLimitingInterface
	dhcpV4       *dhcpv4.DHCPAllocator
	metrics      *metrics.MetricsAllocator
	networkInfos map[string]networkv1.NetworkStatus
	recorder     record.EventRecorder
}

func NewController(
	factory informers.SharedInformerFactory,
	dhcpV4 *dhcpv4.DHCPAllocator,
	metrics *metrics.MetricsAllocator,
	networkInfos map[string]networkv1.NetworkStatus,
	recorder record.EventRecorder,
) *Controller {
	podInformer := factory.InformerFor(&corev1.Pod{}, func(k kubernetes.Interface, duration time.Duration) cache.SharedIndexInformer {
		watcher := cache.NewFilteredListWatchFromClient(k.CoreV1().RESTClient(), "pods", metav1.NamespaceAll, func(options *metav1.ListOptions) {
			options.FieldSelector = fields.Everything().String()
			// Only complex watch of VM pods
			options.LabelSelector = labels.Set{"kubevirt.io": "virt-launcher"}.AsSelector().String()
		})
		return cache.NewSharedIndexInformer(watcher, &corev1.Pod{}, duration, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	})
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	podInformer.AddEventHandler(&PodEventHandler{queue: queue})
	return &Controller{
		podLister:    listerv1.NewPodLister(podInformer.GetIndexer()),
		queue:        queue,
		dhcpV4:       dhcpV4,
		metrics:      metrics,
		networkInfos: networkInfos,
		recorder:     recorder,
	}
}

func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *Controller) processNextItem(ctx context.Context) (loop bool) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Errorf("(pod.processNextItem) panic %v", rec)
			loop = true
		}
	}()

	key, quit := c.queue.Get()
	if quit {
		return false
	}

	defer c.queue.Done(key)

	event, ok := key.(Event)
	if !ok {
		c.queue.Forget(key)
		return true
	}

	if err := c.sync(ctx, event); err != nil {
		log.Errorf("(pod.handleErr) syncing Pod %s: %v", event.ObjKey.String(), err)
		c.queue.AddRateLimited(event)
	} else {
		c.queue.Forget(event)
	}

	return true
}

func (c *Controller) Run(ctx context.Context, workers int) {
	log.Infof("(pod.Run) starting Pod controller")

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	<-ctx.Done()
	c.queue.ShutDown()
	log.Infof("(subnet.Run) stopping Subnet controller")
}

func (c *Controller) sync(ctx context.Context, event Event) error {
	switch event.Operation {
	case ADD:
		pod, err := c.podLister.Pods(event.ObjKey.Namespace).Get(event.ObjKey.Name)
		if errors.IsNotFound(err) {
			log.Infof("(pod.sync) Pod %s does not exist anymore", event.ObjKey.String())
			return nil
		} else if err != nil {
			log.Errorf("(pod.sync) fetching object with key %s from store failed with %v", event.ObjKey.String(), err)
			return err
		}
		log.Infof("(pod.sync) handlerAdd Pod %s", event.ObjKey.String())
		if err = c.handlerAdd(ctx, event.ObjKey, pod); err != nil {
			log.Errorf("(subnet.sync) handlerAdd Pod %s failed: %v", event.ObjKey.Name, err)
			return err
		}
	case DELETE:
		log.Infof("(pod.sync) handlerDelete Pod %s", event.ObjKey.String())
		if err := c.handlerDelete(ctx, event.ObjKey); err != nil {
			log.Errorf("(subnet.sync) handlerDelete Pod %s failed: %v", event.ObjKey.Name, err)
			return err
		}
	}
	return nil
}
