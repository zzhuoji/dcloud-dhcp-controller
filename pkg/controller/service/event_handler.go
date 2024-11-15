package service

import (
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"tydic.io/dcloud-dhcp-controller/pkg/util"
)

type ServiceEventHandler struct {
	queue workqueue.RateLimitingInterface
}

func GetMappingProvider(svc metav1.Object) (string, bool) {
	if svc.GetAnnotations() != nil {
		val, ok := svc.GetAnnotations()[util.AnnoDCloudMappingProvider]
		return val, ok
	}
	return "", false
}

func MatchLabels(service *corev1.Service, pod *corev1.Pod) bool {
	svcSelector := labels.Set(service.Spec.Selector).AsSelector()
	return svcSelector.Matches(labels.Set(pod.Labels))
}

func IsLoadBalancer(service *corev1.Service) bool {
	return service.Spec.Type == corev1.ServiceTypeLoadBalancer
}

func (s *ServiceEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		log.Errorf("expected a *Service but got a %T", obj)
		return
	}
	if provider, ok := GetMappingProvider(svc); ok {
		if isInInitialList && IsLoadBalancer(svc) {
			s.queue.Add(NewEvent(svc, provider, ADD))
		} else if !isInInitialList {
			s.queue.Add(NewEvent(svc, provider, ADD))
		}
	}
}

func (s *ServiceEventHandler) OnUpdate(oldObj, newObj interface{}) {
	oldSvc, ok1 := oldObj.(*corev1.Service)
	newSvc, ok2 := newObj.(*corev1.Service)
	if !ok1 || !ok2 {
		log.Errorf("expected a *Service but got a %T", newObj)
		return
	}
	if oldSvc.ResourceVersion == newSvc.ResourceVersion {
		return
	}
	oldProvider, oldHasProvider := GetMappingProvider(oldSvc)
	newProvider, newHasProvider := GetMappingProvider(newSvc)
	switch {
	case !newHasProvider && oldHasProvider: // delete annotation
		s.queue.Add(NewEvent(newSvc, oldProvider, DELETE)) // delete old provider
	case oldHasProvider && oldProvider != newProvider: // update annotation
		s.queue.Add(NewEvent(newSvc, oldProvider, DELETE)) // delete old provider
	case newHasProvider:
		s.queue.Add(NewEvent(newSvc, newProvider, UPDATE))
	}
}

func (s *ServiceEventHandler) OnDelete(obj interface{}) {
	switch t := obj.(type) {
	case cache.DeletedFinalStateUnknown:
		svc, ok := t.Obj.(*corev1.Service)
		if !ok {
			log.Errorf("expected a *Service but got a %T", obj)
			return
		}
		if provider, ok := GetMappingProvider(svc); ok {
			s.queue.Add(NewEvent(svc, provider, DELETE))
		}
	case *corev1.Service:
		if provider, ok := GetMappingProvider(t); ok {
			s.queue.Add(NewEvent(t, provider, DELETE))
		}
	default:
		log.Errorf("expected a *Service but got a %T", obj)
	}
}
