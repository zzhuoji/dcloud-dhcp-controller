package subnet

import (
	"context"
	"fmt"
	"time"

	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	dhcpv4 "tydic.io/dcloud-dhcp-controller/pkg/dhcp/v4"
	dhcpv6 "tydic.io/dcloud-dhcp-controller/pkg/dhcp/v6"
	"tydic.io/dcloud-dhcp-controller/pkg/metrics"
)

type Controller struct {
	subnetLister SubnetLister
	networkInfos map[string]networkv1.NetworkStatus
	queue        workqueue.RateLimitingInterface
	dhcpV4       *dhcpv4.DHCPAllocator
	dhcpV6       *dhcpv6.DHCPAllocator
	metrics      *metrics.MetricsAllocator
	recorder     record.EventRecorder
}

func NewController(
	scheme *runtime.Scheme,
	factory informers.SharedInformerFactory,
	config *rest.Config,
	dhcpV4 *dhcpv4.DHCPAllocator,
	dhcpV6 *dhcpv6.DHCPAllocator,
	metrics *metrics.MetricsAllocator,
	networkInfos map[string]networkv1.NetworkStatus,
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
		return cache.NewSharedIndexInformer(watcher, &kubeovnv1.Subnet{}, duration, cache.Indexers{
			NetworkProviderIndexerKey: func(obj interface{}) ([]string, error) {
				var values = []string{}
				metaObj, err := meta.Accessor(obj)
				if err != nil {
					return values, fmt.Errorf("object has no meta: %v", err)
				}
				subnet, ok := metaObj.(*kubeovnv1.Subnet)
				if ok {
					values = append(values, GetDHCPProvider(subnet))
				}
				return values, nil
			},
		})
	})
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	subnetInformer.AddEventHandler(&SubnetEventHandler{queue: queue})
	return &Controller{
		subnetLister: NewSubnetLister(subnetInformer.GetIndexer()),
		queue:        queue,
		dhcpV4:       dhcpV4,
		dhcpV6:       dhcpV6,
		metrics:      metrics,
		networkInfos: networkInfos,
		recorder:     recorder,
	}
}

func (c *Controller) getSubnetsByNetProvider(provider string) ([]*kubeovnv1.Subnet, error) {
	return c.subnetLister.GetByIndex(NetworkProviderIndexerKey, provider)
}

func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *Controller) processNextItem(ctx context.Context) (loop bool) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Errorf("(subnet.processNextItem) panic %v", rec)
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
		log.Errorf("(subnet.handleErr) syncing Subnet %s: %v", event.ObjKey.String(), err)
		c.queue.AddRateLimited(event)
	} else {
		c.queue.Forget(event)
	}

	return true
}

func (c *Controller) Run(ctx context.Context, workers int) {
	//defer runtime.HandleCrash()
	//defer c.queue.ShutDown()
	log.Infof("(subnet.Run) starting Subnet controller")

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	<-ctx.Done()
	c.queue.ShutDown()
	log.Infof("(subnet.Run) stopping Subnet controller")
}

func (c *Controller) sync(ctx context.Context, event Event) error {

	subnet, err := c.subnetLister.Get(event.ObjKey.Name)
	if errors.IsNotFound(err) && event.Operation != DELETE {
		log.Infof("(subnet.sync) Subnet %s does not exist anymore", event.ObjKey.Name)
		return nil
	} else if err != nil {
		log.Errorf("(subnet.sync) fetching object with key %s from store failed with %v", event.ObjKey.Name, err)
		return err
	}

	switch event.Operation {
	case ADD:
		log.Infof("(subnet.sync) Add Subnet %s network provider %s", event.ObjKey.Name, event.Provider)
		if err = c.CreateOrUpdateDHCPServer(ctx, subnet, event.Provider); err != nil {
			log.Errorf("(subnet.sync) Add Subnet %s network provider %s failed: %v", event.ObjKey.Name, event.Provider, err)
			return err
		}
	case UPDATE:
		log.Infof("(subnet.sync) Update Subnet %s network provider %s", event.ObjKey.Name, event.Provider)
		if err = c.CreateOrUpdateDHCPServer(ctx, subnet, event.Provider); err != nil {
			log.Errorf("(subnet.sync) Update Subnet %s network provider %s failed: %v", event.ObjKey.Name, event.Provider, err)
			return err
		}
	case DELETE:
		// 删除dhcp服务器
		log.Infof("(subnet.sync) Delete Subnet %s network provider %s", event.ObjKey.Name, event.Provider)
		if err = c.DeleteNetworkProvider(ctx, event.ObjKey, subnet, event.Provider); err != nil {
			log.Errorf("(subnet.sync) Delete Subnet %s network provider %s failed: %v", event.ObjKey.Name, event.Provider, err)
			return err
		}
	}
	return nil
}
