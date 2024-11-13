package pod

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"

	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovnutil "github.com/kubeovn/kube-ovn/pkg/util"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"tydic.io/dcloud-dhcp-controller/pkg/dhcp/v4"
	v6 "tydic.io/dcloud-dhcp-controller/pkg/dhcp/v6"
	"tydic.io/dcloud-dhcp-controller/pkg/util"
)

func getKubeOVNLogicalSwitch(object metav1.Object, multusName, multusNamespace string) (string, bool) {
	if object.GetAnnotations() == nil {
		return "", false
	}
	anno := fmt.Sprintf("%s.%s.kubernetes.io/logical_switch", multusName, multusNamespace)
	subnetName, ok := object.GetAnnotations()[anno]
	return subnetName, ok
}

func getKubeOVNIPAddress(object metav1.Object, multusName, multusNamespace string) (string, bool) {
	if object.GetAnnotations() == nil {
		return "", false
	}
	anno := fmt.Sprintf("%s.%s.kubernetes.io/ip_address", multusName, multusNamespace)
	subnetName, ok := object.GetAnnotations()[anno]
	return subnetName, ok
}

func (c *Controller) getSubnetNameByProvider(object metav1.Object, multusName, multusNamespace, ips string) string {
	// 1. from the annotations
	subnetName, ok := getKubeOVNLogicalSwitch(object, multusName, multusNamespace)
	if !ok {
		// 2. form the subnet.spec.provider
		subnets, err := c.GetSubnetsBySpecProvider(multusName + "." + multusNamespace)
		if err != nil || len(subnets) == 0 {
			return ""
		}
		// 3. filter invalid subnets based on subnet CIDR
		var filterSubnets []*kubeovnv1.Subnet
		for i, subnet := range subnets {
			if ovnutil.CIDRContainIP(subnet.Spec.CIDRBlock, ips) {
				filterSubnets = append(filterSubnets, subnets[i])
			}
		}
		if len(filterSubnets) == 0 {
			return ""
		}
		// if subnet is default, return this subnet name
		// if subnet not default, return first subnet name
		index := slices.IndexFunc(filterSubnets, func(subnet *kubeovnv1.Subnet) bool {
			return subnet.Spec.Default
		})
		if index >= 0 {
			subnetName = subnets[index].Name
		} else {
			subnetName = subnets[0].Name
		}
	}
	return subnetName
}

func (c *Controller) HandlerAddOrUpdatePod(ctx context.Context, podKey types.NamespacedName, pod *corev1.Pod) error {
	// 1. check pod network status
	networkStatus, ok := GetNetworkStatus(pod)
	if !ok || len(networkStatus) == 0 {
		log.Debugf("(pod.HandlerAddOrUpdatePod) Pod <%s> non-existent network status annotation, skip adding", podKey.String())
		return nil
	}

	// 2. parse networks status
	var networkStatusMap []networkv1.NetworkStatus
	err := json.Unmarshal([]byte(networkStatus), &networkStatusMap)
	if err != nil {
		log.Warningf("(pod.HandlerAddOrUpdatePod) Pod <%s> network status desialization failed: %v", podKey.String(), err)
		c.recorder.Event(pod, corev1.EventTypeWarning, "DHCPLeaseError",
			fmt.Sprintf("annotation '%s' desialization failed: %v", networkv1.NetworkStatusAnnot, err))
		return err
	}

	// 3. filter out the pending networks status
	var pendingNetworks []PendingNetwork
	var pendingNetworkNames []string
	for _, netwrokStatus := range networkStatusMap {
		// Filter out non multus attached networks like `kube-ovn`
		split := strings.Split(netwrokStatus.Name, "/")
		if len(split) != 2 {
			continue
		}
		multusName, multusNamespace := split[1], split[0]
		ips, _ := getKubeOVNIPAddress(pod, multusName, multusNamespace)
		if ips == "" {
			ips = strings.Join(netwrokStatus.IPs, ",")
		}
		subnetName := c.getSubnetNameByProvider(pod, multusName, multusNamespace, ips)
		if subnetName == "" {
			continue
		}
		//if _, ok := c.networkInfos[netwrok.Name]; ok {
		pendingNetwork := PendingNetwork{
			SubnetName:      subnetName,
			MultusName:      multusName,
			MultusNamespace: multusNamespace,
			NetworkStatus:   netwrokStatus,
		}
		pendingNetworks = append(pendingNetworks, pendingNetwork)
		pendingNetworkNames = append(pendingNetworkNames, netwrokStatus.Name)
		//}
	}
	if len(pendingNetworks) == 0 {
		log.Debugf("(pod.HandlerAddOrUpdatePod) Pod <%s> has no network to handle, skip adding", podKey.String())
		return nil
	}
	log.Infof("(pod.HandlerAddOrUpdatePod) Pod <%s> pending networks %+v", podKey.String(), pendingNetworkNames)

	var errs []string

	// 4. Handling networks dhcp
	for _, pendingNetwork := range pendingNetworks {
		// Collect network information with incorrect MAC addresses
		if _, err := net.ParseMAC(pendingNetwork.Mac); err != nil {
			errs = append(errs, fmt.Sprintf("network <%s>: hwaddr <%s> is not valid",
				pendingNetwork.Name, pendingNetwork.Mac))
			continue
		}

		// handling IPv4 leases
		if err := c.handlerDHCPV4Lease(pendingNetwork.SubnetName, pendingNetwork.NetworkStatus, podKey, pod); err != nil {
			errs = append(errs, err.Error())
		}

		// handling IPv6 leases
		if err := c.handlerDHCPV6Lease(pendingNetwork.SubnetName, pendingNetwork.NetworkStatus, podKey, pod); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		log.Warnf("(pod.HandlerAddOrUpdatePod) Pod <%s> handler dhcp lease error: %s", podKey.String(), strings.Join(errs, "; "))
	}

	return nil
}

func (c *Controller) handlerDHCPV6Lease(subnetName string, network networkv1.NetworkStatus, podKey types.NamespacedName, pod *corev1.Pod) error {
	// find ipv6 address
	var ipv6Address net.IP
	if ipv6Address = util.GetFirstIPV6Addr(network); ipv6Address == nil {
		return fmt.Errorf("network <%s>: no IPv6 address available", network.Name)
	}
	// add dhcpv6 lease
	dhcpLease := v6.DHCPLease{ClientIP: ipv6Address, SubnetKey: subnetName}
	existLease := c.dhcpV6.HasPodDHCPLease(network.Mac, podKey.String(), dhcpLease)
	if err := c.dhcpV6.AddPodDHCPLease(network.Mac, podKey.String(), dhcpLease); err == nil {
		// update vm dhcpv6 lease gauge
		vmKey := util.GetVMKeyByPodKey(podKey)
		if subnet, ok := c.dhcpV6.GetSubnet(subnetName); ok {
			c.metrics.UpdateVMDHCPv6Lease(vmKey, subnetName, ipv6Address.String(), network.Mac, subnet.LeaseTime)
		} else {
			c.metrics.DeleteVMDHCPv6Lease(vmKey, network.Mac)
		}
		if !existLease {
			c.recorder.Event(pod, corev1.EventTypeNormal, "DHCPLease",
				fmt.Sprintf("Additional network <%s> DHCPv6 lease successfully added", network.Name))
		}
	}

	return nil
}

func (c *Controller) handlerDHCPV4Lease(subnetName string, network networkv1.NetworkStatus, podKey types.NamespacedName, pod *corev1.Pod) error {
	// find ipv4 address
	var ipv4Address net.IP
	if ipv4Address = util.GetFirstIPV4Addr(network); ipv4Address == nil {
		return fmt.Errorf("network <%s>: no IPv4 address available", network.Name)
	}
	// add dhcpv4 lease
	dhcpLease := v4.DHCPLease{ClientIP: ipv4Address, SubnetKey: subnetName}
	existLease := c.dhcpV4.HasPodDHCPLease(network.Mac, podKey.String(), dhcpLease)
	if err := c.dhcpV4.AddPodDHCPLease(network.Mac, podKey.String(), dhcpLease); err == nil {
		// update vm dhcpv4 lease gauge
		vmKey := util.GetVMKeyByPodKey(podKey)
		if subnet, ok := c.dhcpV4.GetSubnet(subnetName); ok {
			c.metrics.UpdateVMDHCPv4Lease(vmKey, subnetName, ipv4Address.String(), network.Mac, subnet.LeaseTime)
		} else {
			c.metrics.DeleteVMDHCPv4Lease(vmKey, network.Mac)
		}
		if !existLease {
			c.recorder.Event(pod, corev1.EventTypeNormal, "DHCPLease",
				fmt.Sprintf("Additional network <%s> DHCPv4 lease successfully added", network.Name))
		}
	}

	return nil
}

func (c *Controller) HandlerDeletePod(ctx context.Context, podKey types.NamespacedName) error {
	// delete pod ipv4 lease
	_ = c.dhcpV4.DeletePodDHCPLease(podKey.String())
	// delete vm dhcpv4 lease gauge
	c.deleteVMDHCPv4Lease(podKey)

	// delete pod ipv6 lease
	_ = c.dhcpV6.DeletePodDHCPLease(podKey.String())
	// delete vm dhcpv6 lease gauge
	c.deleteVMDHCPv6Lease(podKey)

	return nil
}

func (c *Controller) deleteVMDHCPv4Lease(podKey types.NamespacedName) {
	macs, ok := c.dhcpV4.GetPodMacAddress(podKey.String())
	if ok {
		c.metrics.DeletePartialVMDHCPv4Lease(util.GetVMKeyByPodKey(podKey), macs)
	} else {
		c.metrics.DeleteVMDHCPv4Lease(util.GetVMKeyByPodKey(podKey), "")
	}
}

func (c *Controller) deleteVMDHCPv6Lease(podKey types.NamespacedName) {
	macs, ok := c.dhcpV6.GetPodMacAddress(podKey.String())
	if ok {
		c.metrics.DeletePartialVMDHCPv6Lease(util.GetVMKeyByPodKey(podKey), macs)
	} else {
		c.metrics.DeleteVMDHCPv6Lease(util.GetVMKeyByPodKey(podKey), "")
	}
}
