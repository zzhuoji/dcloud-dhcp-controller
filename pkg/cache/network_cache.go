package cache

import (
	"fmt"
	"sync"

	greetrant "github.com/LgoLgo/geentrant"
	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
)

type NetworkCache struct {
	sync.Locker
	orgMap  map[string]networkv1.NetworkStatus
	infoMap map[string]networkv1.NetworkStatus
}

func snapshotNetworkStatus(status networkv1.NetworkStatus) networkv1.NetworkStatus {
	dns := networkv1.DNS{
		Domain:      status.DNS.Domain,
		Search:      make([]string, len(status.DNS.Search)),
		Options:     make([]string, len(status.DNS.Options)),
		Nameservers: make([]string, len(status.DNS.Nameservers)),
	}
	copy(dns.Search, status.DNS.Search)
	copy(dns.Options, status.DNS.Options)
	copy(dns.Nameservers, status.DNS.Nameservers)
	snap := networkv1.NetworkStatus{
		Name:      status.Name,
		Interface: status.Interface,
		IPs:       make([]string, len(status.IPs)),
		Mac:       status.Mac,
		Mtu:       status.Mtu,
		Default:   status.Default,
		DNS:       dns,
		Gateway:   make([]string, len(status.Gateway)),
	}
	copy(snap.IPs, status.IPs)
	copy(snap.Gateway, status.Gateway)
	return snap
}

func (c *NetworkCache) GetDefaultNetwork() (*networkv1.NetworkStatus, bool) {
	c.Lock()
	defer c.Unlock()
	for _, status := range c.orgMap {
		if status.Default {
			networkStatus := snapshotNetworkStatus(status)
			return &networkStatus, true
		}
	}
	return nil, false
}

func (c *NetworkCache) GetNetworkStatus(name string) (*networkv1.NetworkStatus, bool) {
	c.Lock()
	defer c.Unlock()
	status, ok := c.orgMap[name]
	if ok {
		networkStatus := snapshotNetworkStatus(status)
		return &networkStatus, ok
	}
	status, ok = c.infoMap[name]
	if ok {
		networkStatus := snapshotNetworkStatus(status)
		return &networkStatus, ok
	}
	return nil, ok
}

func (c *NetworkCache) SetNetworkStatus(network networkv1.NetworkStatus) error {
	c.Lock()
	defer c.Unlock()
	if network.Name == "" {
		return fmt.Errorf("network name is empty")
	}
	if network.Default {
		return fmt.Errorf("cannot set default network")
	}
	if _, ok := c.orgMap[network.Name]; ok {
		return fmt.Errorf("cannot set the original network <%s>", network.Name)
	}
	if _, ok := c.infoMap[network.Name]; ok {
		return fmt.Errorf("network name <%s> already exists", network.Name)
	}
	c.infoMap[network.Name] = network
	return nil
}

func (c *NetworkCache) UpdateNetworkStatus(network networkv1.NetworkStatus) error {
	c.Lock()
	defer c.Unlock()
	if network.Name == "" {
		return fmt.Errorf("network name is empty")
	}
	if network.Default {
		return fmt.Errorf("cannot set default network")
	}
	if _, ok := c.orgMap[network.Name]; ok {
		return fmt.Errorf("cannot update the original network <%s>", network.Name)
	}
	if _, ok := c.infoMap[network.Name]; !ok {
		return fmt.Errorf("network name <%s> non-existent", network.Name)
	}
	c.infoMap[network.Name] = network
	return nil
}

func (c *NetworkCache) HasOriginalNetwork(name string) bool {
	c.Lock()
	defer c.Unlock()
	_, ok := c.orgMap[name]
	return ok
}

func (c *NetworkCache) DeleteNetworkStatus(name string) error {
	c.Lock()
	defer c.Unlock()
	if name == "" {
		return fmt.Errorf("network name is empty")
	}
	if _, ok := c.orgMap[name]; ok {
		return fmt.Errorf("cannot delete original network <%s>", name)
	}
	if _, ok := c.infoMap[name]; !ok {
		return fmt.Errorf("network name <%s> non-existent", name)
	}
	delete(c.infoMap, name)
	return nil
}

func NewNetworkCache(infos []networkv1.NetworkStatus) *NetworkCache {
	orgMap := make(map[string]networkv1.NetworkStatus)
	for _, info := range infos {
		orgMap[info.Name] = snapshotNetworkStatus(info)
	}
	return &NetworkCache{
		Locker:  &greetrant.RecursiveMutex{},
		orgMap:  orgMap,
		infoMap: make(map[string]networkv1.NetworkStatus),
	}
}
