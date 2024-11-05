package pod

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"

	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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

func (c *Controller) getSubnetNameByProvider(object metav1.Object, multusName, multusNamespace string) string {
	// 1. from the annotations
	subnetName, ok := getKubeOVNLogicalSwitch(object, multusName, multusNamespace)
	if !ok {
		// 2. form the subnet.spec.provider
		subnets, err := c.GetSubnetsBySpecProvider(multusName + "." + multusNamespace)
		if err != nil || len(subnets) == 0 {
			return ""
		}
		// if subnet is default, return this subnet name
		// if subnet not default, return first subnet name
		index := slices.IndexFunc(subnets, func(subnet *kubeovnv1.Subnet) bool {
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

func (c *Controller) handlerAdd(ctx context.Context, podKey types.NamespacedName, pod *corev1.Pod) error {
	// 1. check pod network status
	networkStatus, ok := GetNetworkStatus(pod)
	if !ok || len(networkStatus) == 0 {
		log.Debugf("(pod.handlerAdd) Pod <%s> non-existent network status annotation, skip adding", podKey.String())
		return nil
	}

	// 2. parse networks status
	var networkStatusMap []networkv1.NetworkStatus
	err := json.Unmarshal([]byte(networkStatus), &networkStatusMap)
	if err != nil {
		log.Warningf("(pod.handlerAdd) Pod <%s> network status desialization failed: %v", podKey.String(), err)
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
		subnetName := c.getSubnetNameByProvider(pod, multusName, multusNamespace)
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
		log.Debugf("(pod.handlerAdd) Pod <%s> has no network to handle, skip adding", podKey.String())
		return nil
	}
	log.Infof("(pod.handlerAdd) Pod <%s> pending networks %+v", podKey.String(), pendingNetworkNames)

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
		log.Warnf("(pod.handlerAdd) Pod <%s> handler dhcp lease error: %s", podKey.String(), strings.Join(errs, "; "))
	}

	return nil
}

func (c *Controller) handlerDHCPV6Lease(subnetName string, network networkv1.NetworkStatus, podKey types.NamespacedName, pod *corev1.Pod) error {
	ipv6Addr := util.GetFirstIPV6Addr(network)
	if ipv6Addr == nil {
		return fmt.Errorf("network <%s>: no IPv6 address available", network.Name)
	}
	// create dhcpv6 lease
	dhcpLease := v6.DHCPLease{
		ClientIP:  ipv6Addr,
		SubnetKey: subnetName,
	}
	err := c.dhcpV6.AddPodDHCPLease(network.Mac, podKey.String(), dhcpLease)
	if err == nil {
		c.recorder.Event(pod, corev1.EventTypeNormal, "DHCPLease",
			fmt.Sprintf("Additional network <%s> DHCPv6 lease successfully added", network.Name))
	}

	return nil
}

func (c *Controller) handlerDHCPV4Lease(subnetName string, network networkv1.NetworkStatus, podKey types.NamespacedName, pod *corev1.Pod) error {
	// find ipv4 address
	ipv4Addr := util.GetFirstIPV4Addr(network)
	if ipv4Addr == nil {
		return fmt.Errorf("network <%s>: no IPv4 address available", network.Name)
	}
	// create dhcpv4 lease
	dhcpLease := v4.DHCPLease{
		ClientIP:  ipv4Addr,
		SubnetKey: subnetName,
	}
	err := c.dhcpV4.AddPodDHCPLease(network.Mac, podKey.String(), dhcpLease)
	if err == nil {
		c.recorder.Event(pod, corev1.EventTypeNormal, "DHCPLease",
			fmt.Sprintf("Additional network <%s> DHCPv4 lease successfully added", network.Name))
	}

	return nil
}

func (c *Controller) handlerDelete(ctx context.Context, podKey types.NamespacedName) error {
	// delete pod ipv4 lease
	_ = c.dhcpV4.DeletePodDHCPLease(podKey.String())

	// delete pod ipv6 lease
	_ = c.dhcpV6.DeletePodDHCPLease(podKey.String())

	return nil
}
