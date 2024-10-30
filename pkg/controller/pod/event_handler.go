package pod

import (
	v1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type PodEventHandler struct {
	queue workqueue.RateLimitingInterface
}

func GetNetworkStatus(obj metav1.Object) (string, bool) {
	if obj.GetAnnotations() == nil {
		return "", false
	}
	status, ok := obj.GetAnnotations()[v1.NetworkStatusAnnot]
	return status, ok
}

func HasNetworkStatus(obj metav1.Object) bool {
	_, ok := GetNetworkStatus(obj)
	return ok
}

func (p *PodEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	// Only handle ADD events during initialization, as there is no network status information at the time of Pod creation
	if isInInitialList {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			log.Errorf("expected a *Pod but got a %T", obj)
			return
		}
		// Only responsible for pods with multus network status
		if HasNetworkStatus(pod) {
			p.queue.Add(NewEvent(pod, ADD))
		}
	}
}

func (p *PodEventHandler) OnUpdate(oldObj, newObj interface{}) {
	oldPod, ok1 := oldObj.(*corev1.Pod)
	newPod, ok2 := newObj.(*corev1.Pod)
	if !ok1 || !ok2 {
		log.Errorf("expected a *Pod but got a %T", newObj)
		return
	}
	_, ok1 = GetNetworkStatus(oldPod)
	status, ok2 := GetNetworkStatus(newPod)
	if !ok1 && ok2 && status != "" {
		p.queue.Add(NewEvent(newPod, ADD))
	}
}

func (p *PodEventHandler) OnDelete(obj interface{}) {
	switch t := obj.(type) {
	case cache.DeletedFinalStateUnknown:
		pod, ok := t.Obj.(*corev1.Pod)
		if !ok {
			log.Errorf("expected a *Pod but got a %T", obj)
			return
		}
		// 删除时不需要校验网络状态，直接根据podKey回收dhcp配置
		p.queue.Add(NewEvent(pod, DELETE))
	case *corev1.Pod:
		// 删除时不需要校验网络状态，直接根据podKey回收dhcp配置
		p.queue.Add(NewEvent(t, DELETE))
	default:
		log.Errorf("expected a *Pod but got a %T", obj)
	}
}
