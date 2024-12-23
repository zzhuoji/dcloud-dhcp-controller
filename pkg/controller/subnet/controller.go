package subnet

import (
	"context"
	"time"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	cache2 "tydic.io/dcloud-dhcp-controller/pkg/cache"
	"tydic.io/dcloud-dhcp-controller/pkg/controller"
	dhcpv4 "tydic.io/dcloud-dhcp-controller/pkg/dhcp/v4"
	dhcpv6 "tydic.io/dcloud-dhcp-controller/pkg/dhcp/v6"
	"tydic.io/dcloud-dhcp-controller/pkg/metrics"
)

type Controller struct {
	subnetLister SubnetLister
	networkCache *cache2.NetworkCache
	queue        workqueue.RateLimitingInterface
	dhcpV4       *dhcpv4.DHCPAllocator
	dhcpV6       *dhcpv6.DHCPAllocator
	metrics      *metrics.MetricsAllocator
	recorder     record.EventRecorder
	podNotify    podNotify
	controller.Worker[Event]
}

func NewController(
	scheme *runtime.Scheme,
	factory informers.SharedInformerFactory,
	config *rest.Config,
	networkCache *cache2.NetworkCache,
	dhcpV4 *dhcpv4.DHCPAllocator,
	dhcpV6 *dhcpv6.DHCPAllocator,
	metrics *metrics.MetricsAllocator,
	recorder record.EventRecorder,
) *Controller {
	subnetInformer := factory.InformerFor(&kubeovnv1.Subnet{}, func(k kubernetes.Interface, duration time.Duration) cache.SharedIndexInformer {
		configShallowCopy := *config
		configShallowCopy.GroupVersion = &kubeovnv1.SchemeGroupVersion
		if configShallowCopy.UserAgent == "" {
			configShallowCopy.UserAgent = rest.DefaultKubernetesUserAgent()
		}
		configShallowCopy.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{
			CodecFactory: serializer.NewCodecFactory(scheme),
		}
		configShallowCopy.APIPath = "/apis"
		configShallowCopy.ContentType = runtime.ContentTypeJSON
		restClient, _ := rest.RESTClientFor(&configShallowCopy)
		watcher := cache.NewListWatchFromClient(restClient, "subnets", metav1.NamespaceAll, fields.Everything())
		return cache.NewSharedIndexInformer(watcher, &kubeovnv1.Subnet{}, duration, subnetIndexers)
	})
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	_, _ = subnetInformer.AddEventHandler(&SubnetEventHandler{queue: queue})

	c := &Controller{
		subnetLister: NewSubnetLister(subnetInformer.GetIndexer()),
		queue:        queue,
		dhcpV4:       dhcpV4,
		dhcpV6:       dhcpV6,
		metrics:      metrics,
		networkCache: networkCache,
		recorder:     recorder,
	}
	c.Worker = controller.Worker[Event]{
		Name:     "subnet",
		Queue:    queue,
		SyncFunc: c.sync,
	}
	return c
}

func (c *Controller) SetPodNotify(notify podNotify) {
	c.podNotify = notify
}

func (c *Controller) EnQueue(event Event) {
	c.queue.Add(event)
}

func (c *Controller) GetSubnetsByDHCPProvider(provider string) ([]*kubeovnv1.Subnet, error) {
	return c.subnetLister.GetByIndex(DHCPProviderIndexerKey, provider)
}

func (c *Controller) GetSubnetsBySpecProvider(provider string) ([]*kubeovnv1.Subnet, error) {
	return c.subnetLister.GetByIndex(SpecProviderIndexerKey, provider)
}

func (c *Controller) sync(ctx context.Context, event Event) error {
	subnet, err := c.subnetLister.Get(event.ObjKey.Name)
	if errors.IsNotFound(err) && event.Operation != DELETE {
		log.Infof("(subnet.sync) Subnet <%s> does not exist anymore", event.KeyString())
		return nil
	} else if err != nil {
		log.Errorf("(subnet.sync) fetching object with key <%s> from store failed with %v", event.KeyString(), err)
		return err
	}

	switch event.Operation {
	case ADD:
		log.Infof("(subnet.sync) Add Subnet <%s> network provider <%s>", event.KeyString(), event.Provider)
		if err = c.CreateOrUpdateDHCPServer(ctx, subnet, event.Provider); err != nil {
			log.Errorf("(subnet.sync) Add Subnet <%s> network provider <%s> failed: %v", event.KeyString(), event.Provider, err)
			return err
		}
	case UPDATE:
		log.Infof("(subnet.sync) Update Subnet <%s> network provider <%s>", event.KeyString(), event.Provider)
		if err = c.CreateOrUpdateDHCPServer(ctx, subnet, event.Provider); err != nil {
			log.Errorf("(subnet.sync) Update Subnet <%s> network provider <%s> failed: %v", event.KeyString(), event.Provider, err)
			return err
		}
	case DELETE:
		// 删除dhcp服务器
		log.Infof("(subnet.sync) Delete Subnet <%s> network provider <%s>", event.KeyString(), event.Provider)
		if err = c.DeleteNetworkProvider(ctx, event.ObjKey, subnet, event.Provider); err != nil {
			log.Errorf("(subnet.sync) Delete Subnet <%s> network provider <%s> failed: %v", event.KeyString(), event.Provider, err)
			return err
		}
	}
	return nil
}
