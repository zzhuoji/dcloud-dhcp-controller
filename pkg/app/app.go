package app

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"tydic.io/dcloud-dhcp-controller/pkg/controller/pod"
	"tydic.io/dcloud-dhcp-controller/pkg/controller/subnet"
	dhcpv4 "tydic.io/dcloud-dhcp-controller/pkg/dhcp/v4"
	dhcpv6 "tydic.io/dcloud-dhcp-controller/pkg/dhcp/v6"
	"tydic.io/dcloud-dhcp-controller/pkg/metrics"
	"tydic.io/dcloud-dhcp-controller/pkg/util"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(k8sscheme.AddToScheme(scheme))
	utilruntime.Must(kubeovnv1.AddToScheme(scheme))
}

type handler struct {
	scheme         *runtime.Scheme
	kubeConfigFile string
	kubeContext    string
	podName        string
	podNamespace   string
	networkInfos   map[string]networkv1.NetworkStatus
	kubeClient     *kubernetes.Clientset
	dhcpV4         *dhcpv4.DHCPAllocator
	dhcpV6         *dhcpv6.DHCPAllocator
	lock           *resourcelock.LeaseLock
	leaderId       string
}

func Register() *handler {
	return &handler{}
}

func (h *handler) getKubeConfig() (config *rest.Config, err error) {
	if !util.FileExists(h.kubeConfigFile) {
		return rest.InClusterConfig()
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: h.kubeConfigFile},
		&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{}, CurrentContext: h.kubeContext},
	).ClientConfig()
}

func (h *handler) Init() {
	h.scheme = scheme
	h.kubeConfigFile = os.Getenv("KUBECONFIG")
	if h.kubeConfigFile == "" {
		homedir := os.Getenv("HOME")
		h.kubeConfigFile = filepath.Join(homedir, ".kube", "config")
	}

	h.kubeContext = os.Getenv("KUBECONTEXT")

	h.podName = os.Getenv("POD_NAME")
	h.podNamespace = os.Getenv("POD_NAMESPACE")

	config, err := h.getKubeConfig()
	handleErr(err)
	h.kubeClient, err = kubernetes.NewForConfig(config)
	handleErr(err)

	// make sure the leader label is removed in case the pod crashed
	h.RemoveLeaderPodLabel()

	networkStatus, err := util.NetworkStatusFromFile(util.NetworkStatusFilePath)
	handleErr(err)

	if len(networkStatus) == 0 {
		handleErr(fmt.Errorf("no Multus network status information available"))
	}

	h.networkInfos = make(map[string]networkv1.NetworkStatus)
	for i, status := range networkStatus {
		h.networkInfos[status.Name] = networkStatus[i]
	}

	h.leaderId = uuid.NewString()
	log.Infof("(app.Run) generated leader id: %s", h.leaderId)

	h.lock = &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      "dcloud-dhcp-controller",
			Namespace: h.podNamespace,
		},
		Client: h.kubeClient.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: h.leaderId,
		},
	}
}

func (h *handler) Run(mainCtx context.Context) {
	// create a new context for this, otherwise it will be cancelled during pool updates (this need to be the same as the main context)
	leaderelection.RunOrDie(mainCtx, leaderelection.LeaderElectionConfig{
		Lock:            h.lock,
		ReleaseOnCancel: true,
		LeaseDuration:   60 * time.Second,
		RenewDeadline:   15 * time.Second,
		RetryPeriod:     5 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(mainCtx context.Context) {
				ctx, cancelFunc := context.WithCancel(mainCtx)
				defer cancelFunc()
				h.RunServices(ctx)
				<-mainCtx.Done()
			},
			OnStoppedLeading: func() {
				log.Infof("(app.Run) leader lost: %s", h.leaderId)
				h.RemoveLeaderPodLabel()
			},
			OnNewLeader: func(identity string) {
				if identity == h.leaderId {
					return
				}
				log.Infof("(app.Run) new leader elected: %s", identity)
			},
		},
	})
}

// resyncPeriod computes the time interval a shared informer waits before resyncing with the api server
func resyncPeriod(minResyncPeriod time.Duration) time.Duration {
	factor := rand.Float64() + 1
	return time.Duration(float64(minResyncPeriod.Nanoseconds()) * factor)
}

func (h *handler) RunServices(ctx context.Context) {

	// initialize the dhcp v4 service
	h.dhcpV4 = dhcpv4.New()
	// initialize the dhcp v6 service
	h.dhcpV6 = dhcpv6.New()

	// initialize the metrics service
	metricsServer := metrics.New()
	go metricsServer.Run(ctx)

	// add the network.dcloud.tydic.io/leader pod label
	h.addLeaderPodLabel()

	config, err := h.getKubeConfig()
	handleErr(err)
	kubeClient, err := kubernetes.NewForConfig(config)
	handleErr(err)

	// Trim ManagedFields reduce memory usage
	transform := informers.WithTransform(func(in any) (any, error) {
		// Nilcheck managed fields to avoid hitting https://github.com/kubernetes/kubernetes/issues/124337
		if obj, err := meta.Accessor(in); err == nil && obj.GetManagedFields() != nil {
			obj.SetManagedFields(nil)
		}
		return in, nil
	})
	// Increase random jitter to stagger synchronization time
	resyncConfig := informers.WithCustomResyncConfig(map[metav1.Object]time.Duration{
		&corev1.Pod{}:       resyncPeriod(12 * time.Hour),
		&kubeovnv1.Subnet{}: resyncPeriod(12 * time.Hour),
	})
	factory := informers.NewSharedInformerFactoryWithOptions(kubeClient, 0, transform, resyncConfig)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&typedv1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	recorder := broadcaster.NewRecorder(h.scheme, corev1.EventSource{Component: "dcloud-dhcp-controller"})

	podController := pod.NewController(factory, h.dhcpV4, h.dhcpV6, metricsServer, h.networkInfos, recorder)
	subnetController := subnet.NewController(h.scheme, factory, config, h.dhcpV4, h.dhcpV6, metricsServer, h.networkInfos, recorder)

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(wait.NeverStop)

	// Ensure a coroutine sequence for handling subnet events
	go subnetController.Run(ctx, 1)
	// Allow multiple coroutines to process pod events in parallel
	go podController.Run(ctx, 1)

}

// The addLeaderPodLabel and removeLeaderPodLabel funtions are managing the dcloud.tydic.io/leader label.
// This label is used by the metrics-service to determine the active leader.
// If the function(s) fail the application should ignore it and still service DHCP requests.
func (h *handler) addLeaderPodLabel() {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		curPod, err := h.kubeClient.CoreV1().Pods(h.podNamespace).Get(context.TODO(), h.podName, metav1.GetOptions{})
		if err != nil {
			log.Errorf("(app.addLeaderPodLabel) cannot get current pod object: %s", err.Error())
			return err
		}
		// try update
		labels := map[string]string{util.LabelDCloudLeader: "active"}
		return util.PatchPodLabels(h.kubeClient, curPod, labels)
	})
	if err != nil {
		log.Errorf("(app.addLeaderPodLabel) try patch pod labels failed: %s", err.Error())
	}
}

func (h *handler) RemoveLeaderPodLabel() {
	if h.kubeClient == nil {
		kubeRestConfig, err := h.getKubeConfig()
		if err != nil {
			log.Errorf("(app.RemoveLeaderPodLabel) cannot get kubeRestConfig: %s", err.Error())
			return
		}
		h.kubeClient, err = kubernetes.NewForConfig(kubeRestConfig)
		if err != nil {
			log.Errorf("(app.RemoveLeaderPodLabel) cannot get kihClientset: %s", err.Error())
			return
		}
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		curPod, err := h.kubeClient.CoreV1().Pods(h.podNamespace).Get(context.TODO(), h.podName, metav1.GetOptions{})
		if err != nil {
			log.Errorf("(app.RemoveLeaderPodLabel) cannot get current pod object: %s", err.Error())
			return err
		}
		// try update
		labels := map[string]string{util.LabelDCloudLeader: ""}
		return util.PatchPodLabels(h.kubeClient, curPod, labels)
	})
	if err != nil {
		log.Errorf("(app.RemoveLeaderPodLabel) try patch pod labels failed: %s", err.Error())
	}

}

func handleErr(err error) {
	if err != nil {
		log.Panicf("(app.handleErr) %s", err.Error())
	}
}
