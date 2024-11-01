package pod

import (
	"context"
	"fmt"
	"net"
	"strings"

	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"tydic.io/dcloud-dhcp-controller/pkg/dhcp/v4"
	v6 "tydic.io/dcloud-dhcp-controller/pkg/dhcp/v6"
	"tydic.io/dcloud-dhcp-controller/pkg/util"
)

func GetKubeOVNLogicalSwitch(object metav1.Object, multusName, multusNamespace string) (string, bool) {
	if object.GetAnnotations() == nil {
		return "", false
	}
	anno := fmt.Sprintf("%s.%s.kubernetes.io/logical_switch", multusName, multusNamespace)
	subnetName, ok := object.GetAnnotations()[anno]
	return subnetName, ok
}

func (c *Controller) handlerAdd(ctx context.Context, podKey types.NamespacedName, pod *corev1.Pod) error {
	// 1. check pod network status
	networkStatus, ok := GetNetworkStatus(pod)
	if !ok || len(networkStatus) == 0 {
		log.Debugf("(pod.handlerAdd) Pod %s non-existent network status annotation, skip adding", podKey.String())
		return nil
	}

	// 2. parse networks status
	var networkStatusMap []networkv1.NetworkStatus
	err := json.Unmarshal([]byte(networkStatus), &networkStatusMap)
	if err != nil {
		log.Warningf("(pod.handlerAdd) Pod %s network status desialization failed: %v", podKey.String(), err)
		c.recorder.Event(pod, corev1.EventTypeWarning, "DHCPLeaseError",
			fmt.Sprintf("annotation %s desialization failed: %v", networkv1.NetworkStatusAnnot, err))
		return err
	}

	// 3. filter out the pending networks status
	var pendingNetworks []networkv1.NetworkStatus
	var pendingNetworkNames []string
	for _, netwrok := range networkStatusMap {
		// Filter out non multus attached networks like `kube-ovn`
		split := strings.Split(netwrok.Name, "/")
		if len(split) != 2 {
			continue
		}
		multusName, multusNamespace := split[1], split[0]
		_, ok := GetKubeOVNLogicalSwitch(pod, multusName, multusNamespace)
		if !ok {
			continue
		}
		if _, ok := c.networkInfos[netwrok.Name]; ok {
			pendingNetworks = append(pendingNetworks, netwrok)
			pendingNetworkNames = append(pendingNetworkNames, netwrok.Name)
		}
	}
	if len(pendingNetworks) == 0 {
		log.Debugf("(pod.handlerAdd) Pod %s has no network to handle, skip adding", podKey.String())
		return nil
	}
	log.Infof("(pod.handlerAdd) Pod %s pending networks %+v", podKey.String(), pendingNetworkNames)

	var errs []error

	// 4. Handling networks dhcp
	for _, network := range pendingNetworks {
		// Collect network information with incorrect MAC addresses
		if _, err := net.ParseMAC(network.Mac); err != nil {
			errs = append(errs, fmt.Errorf("networkName %s: hwaddr %s is not valid", network.Name, network.Mac))
			continue
		}

		split := strings.Split(network.Name, "/")
		multusName, multusNamespace := split[1], split[0]
		subnetName, _ := GetKubeOVNLogicalSwitch(pod, multusName, multusNamespace)
		// handling IPv4 leases
		if err := c.handlerDHCPV4Lease(subnetName, network, podKey, pod); err != nil {
			errs = append(errs, err)
			continue
		}
		// handling IPv6 leases
		if err := c.handlerDHCPV6Lease(subnetName, network, podKey, pod); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		messages := make([]string, len(errs))
		for i, err := range errs {
			messages[i] = err.Error()
		}
		log.Errorf("(pod.handlerAdd) Pod %s handler network errors: %s", podKey.String(), strings.Join(messages, "; "))
	}

	return nil
}

func (c *Controller) handlerDHCPV6Lease(subnetName string, network networkv1.NetworkStatus, podKey types.NamespacedName, pod *corev1.Pod) error {
	ipv6Addr := util.GetFirstIPV6Addr(network)
	if ipv6Addr == nil {
		return fmt.Errorf("networkName %s: invalid IPv6 address", network.Name)
	}
	// create dhcpv6 lease
	dhcpLease := v6.DHCPLease{
		ClientIP:  ipv6Addr,
		SubnetKey: subnetName,
		PodKey:    podKey.String(),
	}
	_ = c.dhcpV6.AddDHCPLease(network.Mac, dhcpLease)

	c.recorder.Event(pod, corev1.EventTypeNormal, "DHCPLease", fmt.Sprintf("Additional network %s DHCPv6 lease successfully added", network.Name))

	return nil
}

func (c *Controller) handlerDHCPV4Lease(subnetName string, network networkv1.NetworkStatus, podKey types.NamespacedName, pod *corev1.Pod) error {
	// find ipv4 address
	ipv4Addr := util.GetFirstIPV4Addr(network)
	if ipv4Addr == nil {
		return fmt.Errorf("networkName %s: invalid IPv4 address", network.Name)
	}
	// create dhcpv4 lease
	dhcpLease := v4.DHCPLease{
		ClientIP:  ipv4Addr,
		SubnetKey: subnetName,
		PodKey:    podKey.String(),
	}
	_ = c.dhcpV4.AddDHCPLease(network.Mac, dhcpLease)

	c.recorder.Event(pod, corev1.EventTypeNormal, "DHCPLease", fmt.Sprintf("Additional network %s DHCPv4 lease successfully added", network.Name))

	return nil
}

func (c *Controller) handlerDelete(ctx context.Context, podKey types.NamespacedName) error {
	// delete pod ipv4 lease
	c.dhcpV4.DeletePodDHCPLease(podKey.String())
	// delete pod ipv6 lease
	c.dhcpV6.DeletePodDHCPLease(podKey.String())

	return nil
}
