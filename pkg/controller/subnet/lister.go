package subnet

import (
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

const (
	NetworkProviderIndexerKey = "networkProvider"
)

// SubnetLister helps list Subnets.
// All objects returned here must be treated as read-only.
type SubnetLister interface {
	// List lists all Subnets in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*kubeovnv1.Subnet, err error)
	// Get retrieves the Subnet from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*kubeovnv1.Subnet, error)

	GetByIndex(indexerKey, indexedValue string) ([]*kubeovnv1.Subnet, error)
}

// subnetLister implements the SubnetLister interface.
type subnetLister struct {
	indexer cache.Indexer
}

// NewSubnetLister returns a new SubnetLister.
func NewSubnetLister(indexer cache.Indexer) SubnetLister {
	return &subnetLister{indexer: indexer}
}

// List lists all Pods in the indexer.
func (s *subnetLister) List(selector labels.Selector) (ret []*kubeovnv1.Subnet, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*kubeovnv1.Subnet))
	})
	return ret, err
}

// Get retrieves the Subnet from the indexer for a given namespace and name.
func (s *subnetLister) Get(name string) (*kubeovnv1.Subnet, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(kubeovnv1.Resource("subnet"), name)
	}
	return obj.(*kubeovnv1.Subnet), nil
}

// GetByIndex
func (s *subnetLister) GetByIndex(indexerKey, indexedValue string) ([]*kubeovnv1.Subnet, error) {
	objs, err := s.indexer.ByIndex(indexerKey, indexedValue)
	if err != nil {
		return nil, err
	}
	subnets := make([]*kubeovnv1.Subnet, len(objs))
	for i, obj := range objs {
		subnets[i] = obj.(*kubeovnv1.Subnet)
	}
	return subnets, nil
}
