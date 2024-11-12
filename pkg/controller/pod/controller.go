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
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"tydic.io/dcloud-dhcp-controller/pkg/controller"
	dhcpv4 "tydic.io/dcloud-dhcp-controller/pkg/dhcp/v4"
	dhcpv6 "tydic.io/dcloud-dhcp-controller/pkg/dhcp/v6"
	"tydic.io/dcloud-dhcp-controller/pkg/metrics"
)

type Controller struct {
	podLister    listerv1.PodLister
	queue        workqueue.RateLimitingInterface
	dhcpV4       *dhcpv4.DHCPAllocator
	dhcpV6       *dhcpv6.DHCPAllocator
	metrics      *metrics.MetricsAllocator
	networkInfos map[string]networkv1.NetworkStatus
	recorder     record.EventRecorder
	controller.Worker[Event]
	subnetClient
}

func NewController(
	factory informers.SharedInformerFactory,
	dhcpV4 *dhcpv4.DHCPAllocator,
	dhcpV6 *dhcpv6.DHCPAllocator,
	metrics *metrics.MetricsAllocator,
	networkInfos map[string]networkv1.NetworkStatus,
	recorder record.EventRecorder,
	subnetClient subnetClient,
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
	_, _ = podInformer.AddEventHandler(&PodEventHandler{queue: queue})
	c := &Controller{
		podLister:    listerv1.NewPodLister(podInformer.GetIndexer()),
		queue:        queue,
		dhcpV4:       dhcpV4,
		dhcpV6:       dhcpV6,
		metrics:      metrics,
		networkInfos: networkInfos,
		recorder:     recorder,
		subnetClient: subnetClient,
	}
	c.Worker = controller.Worker[Event]{
		Name:     "pod",
		Queue:    queue,
		SyncFunc: c.sync,
	}
	return c
}

func (c *Controller) EnQueue(event Event) {
	c.queue.Add(event)
}

func (c *Controller) sync(ctx context.Context, event Event) error {
	switch event.Operation {
	case ADD, UPDATE:
		pod, err := c.podLister.Pods(event.ObjKey.Namespace).Get(event.ObjKey.Name)
		if errors.IsNotFound(err) {
			log.Infof("(pod.sync) Pod <%s> does not exist anymore", event.KeyString())
			return nil
		} else if err != nil {
			log.Errorf("(pod.sync) fetching object with key <%s> from store failed with %v", event.KeyString(), err)
			return err
		}
		log.Infof("(pod.sync) Handler %s Pod %s", event.Operation, event.KeyString())
		if err = c.HandlerAddOrUpdatePod(ctx, event.ObjKey, pod); err != nil {
			log.Errorf("(pod.sync) Handler %s Pod <%s> failed: %v", event.Operation, event.KeyString(), err)
			return err
		}
	case DELETE:
		log.Infof("(pod.sync) Handler delete Pod <%s>", event.KeyString())
		if err := c.HandlerDeletePod(ctx, event.ObjKey); err != nil {
			log.Errorf("(pod.sync) Handler delete Pod <%s> failed: %v", event.KeyString(), err)
			return err
		}
	}
	return nil
}
