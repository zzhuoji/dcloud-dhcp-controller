package subnet

import (
	"strings"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// TODO Filter out if the network provider is OVN's own subnet
func filterSubnetProvider(subnet *kubeovnv1.Subnet) bool {
	return subnet.Spec.Provider != "" && subnet.Spec.Provider != "ovn" &&
		!strings.HasSuffix(subnet.Spec.Provider, ".ovn")
}

func filterSubnetDHCPEnable(oldSubnet, newSubnet *kubeovnv1.Subnet) bool {
	return !oldSubnet.Spec.EnableDHCP && newSubnet.Spec.EnableDHCP
}

func filterSubnetProviderChange(oldSubnet, newSubnet *kubeovnv1.Subnet) bool {
	return oldSubnet.Spec.Provider != newSubnet.Spec.Provider
}

func filterSubnetDHCPDisable(oldSubnet, newSubnet *kubeovnv1.Subnet) bool {
	return oldSubnet.Spec.EnableDHCP && !newSubnet.Spec.EnableDHCP
}

func filterSubnetDHCPChange(oldSubnet, newSubnet *kubeovnv1.Subnet) bool {
	return (oldSubnet.Spec.DHCPv4Options != newSubnet.Spec.DHCPv4Options) ||
		(oldSubnet.Spec.DHCPv6Options != newSubnet.Spec.DHCPv6Options)
}

func filterSubnetCIDRChange(oldSubnet, newSubnet *kubeovnv1.Subnet) bool {
	return oldSubnet.Spec.CIDRBlock != newSubnet.Spec.CIDRBlock
}

func filterSubnetGatewayChange(oldSubnet, newSubnet *kubeovnv1.Subnet) bool {
	return oldSubnet.Spec.Gateway != newSubnet.Spec.Gateway
}

type SubnetEventHandler struct {
	queue workqueue.RateLimitingInterface
}

func (s *SubnetEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	subnet, ok := obj.(*kubeovnv1.Subnet)
	if !ok {
		log.Errorf("expected a *Subnet but got a %T", obj)
		return
	}
	// 在add事件时，校验subnet provider符合要求并且打开了dhcp服务
	if filterSubnetProvider(subnet) && subnet.Spec.EnableDHCP {
		s.queue.Add(NewEvent(subnet, subnet.Spec.Provider, ADD))
	}
}

func (s *SubnetEventHandler) OnUpdate(oldObj, newObj interface{}) {
	oldSubnet, ok1 := oldObj.(*kubeovnv1.Subnet)
	newSubnet, ok2 := newObj.(*kubeovnv1.Subnet)
	if !ok1 || !ok2 {
		log.Errorf("expected a *Subnet but got a %T", newObj)
		return
	}

	switch {
	case filterSubnetDHCPEnable(oldSubnet, newSubnet): // 打开dhcp
		if filterSubnetProvider(newSubnet) { // provider 符合要求
			s.queue.Add(NewEvent(newSubnet, newSubnet.Spec.Provider, ADD))
		}
	case filterSubnetProviderChange(oldSubnet, newSubnet): // provider发生变化
		if filterSubnetProvider(oldSubnet) { // 旧的 provider 符合要求
			s.queue.Add(NewEvent(oldSubnet, oldSubnet.Spec.Provider, DELETE)) // 删除旧的
		}
		if filterSubnetProvider(newSubnet) { // 新的 provider 符合要求
			s.queue.Add(NewEvent(newSubnet, newSubnet.Spec.Provider, ADD)) // 添加新的
		}
	case filterSubnetDHCPDisable(oldSubnet, newSubnet): // 关闭DHCP 删除事件
		if filterSubnetProvider(newSubnet) { // provider 符合要求
			s.queue.Add(NewEvent(newSubnet, newSubnet.Spec.Provider, DELETE))
		}
	case filterSubnetDHCPChange(oldSubnet, newSubnet) ||
		filterSubnetGatewayChange(oldSubnet, newSubnet) ||
		filterSubnetCIDRChange(oldSubnet, newSubnet): // dhcpOptions or gateway or cidr changed
		if filterSubnetProvider(newSubnet) { // provider 符合要求
			s.queue.Add(NewEvent(newSubnet, newSubnet.Spec.Provider, UPDATE))
		}
	}
}

func (s *SubnetEventHandler) OnDelete(obj interface{}) {
	switch t := obj.(type) {
	case cache.DeletedFinalStateUnknown:
		subnet, ok := t.Obj.(*kubeovnv1.Subnet)
		if !ok {
			log.Errorf("expected a *Subnet but got a %T", obj)
			return
		}
		if filterSubnetProvider(subnet) {
			s.queue.Add(NewEvent(subnet, subnet.Spec.Provider, DELETE))
		}
	case *kubeovnv1.Subnet:
		if filterSubnetProvider(t) {
			s.queue.Add(NewEvent(t, t.Spec.Provider, DELETE))
		}
	default:
		log.Errorf("expected a *Subnet but got a %T", obj)
	}
}
