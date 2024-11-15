package service

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const MappingProviderIndex = "meta.anno.mapping.provider"

var MetaMappingProviderIndexFunc = func(obj interface{}) ([]string, error) {
	var values = []string{}
	metaObj, err := meta.Accessor(obj)
	if err != nil {
		return values, fmt.Errorf("object has no meta: %v", err)
	}
	provider, ok := GetMappingProvider(metaObj)
	if ok {
		values = append(values, provider)
	}
	return values, nil
}

// ServiceLister helps list Service.
// All objects returned here must be treated as read-only.
type ServiceLister struct {
	v1.ServiceLister
	indexer cache.Indexer
}

func (s *ServiceLister) GetByIndex(indexerKey, indexedValue string) ([]*corev1.Service, error) {
	objs, err := s.indexer.ByIndex(indexerKey, indexedValue)
	if err != nil {
		return nil, err
	}
	services := make([]*corev1.Service, len(objs))
	for i, obj := range objs {
		services[i] = obj.(*corev1.Service)
	}
	return services, nil
}

// NewServiceLister returns a new ServiceLister.
func NewServiceLister(indexer cache.Indexer) *ServiceLister {
	return &ServiceLister{
		ServiceLister: v1.NewServiceLister(indexer),
		indexer:       indexer,
	}
}
