package cache

import (
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	informerscorev1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type PodCache struct {
	podKey     types.NamespacedName
	cacheStore cache.Store
	HasSynced  func() bool
	Run        func(stopCh <-chan struct{})
}

func (p *PodCache) GetSelfPod() *corev1.Pod {
	item, exists, err := p.cacheStore.GetByKey(p.podKey.String())
	if err != nil {
		log.Fatalf("Error reading local pod cache: %v", err)
	}
	if !exists {
		log.Fatalf("Panic: Read local cache. the self pod does not exist.")
	}
	return item.(*corev1.Pod).DeepCopy()
}

func NewPodCache(kubeClient kubernetes.Interface, name, namespace string) *PodCache {
	podKey := types.NamespacedName{Name: name, Namespace: namespace}
	informer := informerscorev1.NewFilteredPodInformer(kubeClient, namespace,
		0, cache.Indexers{}, func(options *metav1.ListOptions) {
			options.FieldSelector = fields.OneTermEqualSelector("metadata.name", name).String()
		})
	return &PodCache{
		podKey:     podKey,
		cacheStore: informer.GetStore(),
		HasSynced:  informer.HasSynced,
		Run:        informer.Run,
	}
}
