package service

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"

	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"tydic.io/dcloud-dhcp-controller/pkg/controller/subnet"
)

func checkNetworkProvider(provider string, svcKey types.NamespacedName) (*types.NamespacedName, error) {
	nadKey := types.NamespacedName{
		Name:      provider,
		Namespace: svcKey.Namespace,
	}
	split := strings.Split(provider, ".")
	switch len(split) {
	case 1:
	case 2:
		nadKey.Name, nadKey.Namespace = split[0], split[1]
	default:
		return nil, fmt.Errorf("unsupported network provider format: %s", provider)
	}
	return &nadKey, nil
}

func (c *Controller) HandlerCreateOrUpdate(ctx context.Context, svcKey types.NamespacedName, svc *corev1.Service) error {
	// check svc type is LoadBalancer
	if !IsLoadBalancer(svc) {
		log.Debugf("(service.HandlerCreateOrUpdate) Service <%s> not load balancing type", svcKey.String())
		return nil
	}
	// check svc has mapping provider
	provider, ok := GetMappingProvider(svc)
	if !ok {
		log.Debugf("(service.HandlerCreateOrUpdate) Service <%s> has no mapped provider", svcKey.String())
		return nil
	}
	selfPod := c.GetSelfPod()
	if !MatchLabels(svc, selfPod) {
		log.Warnf("(service.HandlerCreateOrUpdate) Service <%s> selector is not the current DHCP service, skip it", svcKey.String())
		return nil
	}

	nadKey, err := checkNetworkProvider(provider, svcKey)
	if err != nil {
		c.recorder.Event(svc, corev1.EventTypeWarning, "ValidateProviderError", err.Error())
		log.Errorf("(service.HandlerCreateOrUpdate) Service <%s> check network provider error: %v", svcKey.String(), err)
		return nil
	}
	services, err := c.serviceLister.GetByIndex(MappingProviderIndex, provider)
	if err != nil {
		return fmt.Errorf("GetByIndex error: %v", err)
	}
	if len(services) > 1 {
		err = fmt.Errorf("detected multiple services using the same provider <%v>", provider)
		c.recorder.Event(svc, corev1.EventTypeWarning, "ValidateProviderError", err.Error())
		return err
	}

	if c.networkCache.HasOriginalNetwork(nadKey.String()) {
		msg := fmt.Sprintf("Unable to use the built-in original network provider <%s> as a mapping object", provider)
		c.recorder.Event(svc, corev1.EventTypeWarning, "ValidateProviderError", msg)
		log.Errorf("(service.HandlerCreateOrUpdate) Service <%s> %s", svcKey.String(), msg)
		return nil
	}

	// get lb ips
	var loadBalancerIPs []string
	for _, ingress := range svc.Status.LoadBalancer.Ingress {
		if ingress.IP != "" && net.ParseIP(ingress.IP) != nil {
			loadBalancerIPs = append(loadBalancerIPs, ingress.IP)
		}
	}
	if len(loadBalancerIPs) == 0 {
		c.recorder.Event(svc, corev1.EventTypeWarning, "WaitingLoadBalancer", "Waiting for LoadBalancer initialization")
		return nil
	}
	log.Tracef("(service.HandlerCreateOrUpdate) Service <%s> detected load balancing IPs %+v", svcKey.String(), loadBalancerIPs)

	var (
		update   bool
		original networkv1.NetworkStatus
		network  networkv1.NetworkStatus
	)

	if status, ok := c.networkCache.GetNetworkStatus(nadKey.String()); ok {
		original = *status
		network = *status
		update = true
	} else if status, ok = c.networkCache.GetDefaultNetwork(); ok {
		network = *status
	} else {
		err = fmt.Errorf("default network not found in network state cache")
		c.recorder.Event(svc, corev1.EventTypeWarning, "InternalError", err.Error())
		return err
	}
	// Modify network configuration
	MutationNetwork(*nadKey, loadBalancerIPs, &network)
	if update && reflect.DeepEqual(original, network) {
		log.Debugf("(service.HandlerCreateOrUpdate) Service <%s> no need to update any configuration", svcKey.String())
		return nil
	}

	if update {
		err = c.networkCache.UpdateNetworkStatus(network)
	} else {
		err = c.networkCache.SetNetworkStatus(network)
	}
	if err != nil {
		log.Errorf("(service.HandlerCreateOrUpdate) Service <%s> failed to modify network cache: %v", svcKey.String(), err)
		c.recorder.Event(svc, corev1.EventTypeWarning, "InternalError", err.Error())
		return err
	}
	// notify related provider subnet
	provider = fmt.Sprintf("%s.%s", nadKey.Name, nadKey.Namespace)
	subnets, err := c.GetSubnetsByDHCPProvider(provider)
	if err != nil {
		return fmt.Errorf("GetSubnetsByDHCPProvider error: %v", err)
	}
	var notifySubnets []string
	for _, sub := range subnets {
		if sub.DeletionTimestamp != nil {
			continue
		}
		c.EnQueue(subnet.NewEvent(sub, provider, subnet.UPDATE))
		notifySubnets = append(notifySubnets, sub.Name)
	}
	if len(notifySubnets) > 0 {
		log.Debugf("(service.HandlerCreateOrUpdate) Service <%s> notify to update subnets %+v", svcKey.String(), notifySubnets)
	}

	return nil
}

func MutationNetwork(nadKey types.NamespacedName, loadBalancerIPs []string, network *networkv1.NetworkStatus) {
	network.Name = nadKey.String()
	network.IPs = loadBalancerIPs
	network.Default = false
	network.Gateway = []string{}
	network.DNS = networkv1.DNS{}
}

func (c *Controller) HandlerDelete(ctx context.Context, provider string, svcKey types.NamespacedName) error {
	// check provider
	nadKey, err := checkNetworkProvider(provider, svcKey)
	if err != nil {
		log.Warnf("(service.HandlerDelete) Service <%s> %v, ignore it", svcKey.String(), err)
		return nil
	}

	// skip original network
	if c.networkCache.HasOriginalNetwork(nadKey.String()) {
		log.Warnf("(service.HandlerDelete) Service <%s> original network state cannot be deleted, ignore it", svcKey.String())
		return nil
	}

	c.networkCache.Lock()
	defer c.networkCache.Unlock()

	// check network cache
	if _, ok := c.networkCache.GetNetworkStatus(nadKey.String()); !ok {
		log.Warnf("(service.HandlerDelete) Service <%s> provider <%s> does not exist in the local network cache", svcKey.String(), provider)
		return nil
	}

	// delete related provider subnet
	subnets, err := c.GetSubnetsByDHCPProvider(provider)
	if err != nil {
		return fmt.Errorf("GetSubnetsByDHCPProvider error: %v", err)
	}
	provider = fmt.Sprintf("%s.%s", nadKey.Name, nadKey.Namespace)
	for _, sub := range subnets {
		err = c.DeleteNetworkProvider(ctx, types.NamespacedName{Name: sub.Name}, nil, provider)
		if err != nil {
			return err
		}
		if sub.Spec.EnableDHCP {
			c.recorder.Event(sub, corev1.EventTypeWarning, "DHCPServer", "Stop provider's DHCP service due to LoadBalancer shutdown")
		}
	}

	_ = c.networkCache.DeleteNetworkStatus(nadKey.String())

	return nil
}
