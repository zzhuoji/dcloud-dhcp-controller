package subnet

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeovnv1 "tydic.io/dcloud-dhcp-controller/pkg/apis/kubeovn/v1"
	"tydic.io/dcloud-dhcp-controller/pkg/util"
)

func (c *Controller) handlerDHCPV4(subnet *kubeovnv1.Subnet, networkStatus networkv1.NetworkStatus) error {
	// 1. parse dhcpv4 options
	dhcpv4Options := strings.ReplaceAll(subnet.Spec.DHCPv4Options, " ", "")
	dhcpv4OptionsMap := util.ParseDHCPOptions(dhcpv4Options)

	// 2. build ovn subnet
	ovnSubnet, err := util.BuildOVNSubnetByIPV4Options(subnet, networkStatus, dhcpv4OptionsMap)
	if err != nil {
		log.Errorf("(subnet.handlerDHCPV4) %s", err.Error())
		c.recorder.Event(subnet, corev1.EventTypeWarning, "SubnetError", err.Error())
		return err
	}

	// 3. add or update subnet
	oldOVNSubnet, ok := c.dhcpV4.GetSubnet(subnet.Name)
	c.dhcpV4.AddOrUpdateSubnet(subnet.Name, *ovnSubnet)

	if ok && !reflect.DeepEqual(oldOVNSubnet, *ovnSubnet) { // if update dhcpv4 options, send recorder event
		c.recorder.Event(subnet, corev1.EventTypeNormal, "DHCPServer", "DHCP v4 options updated successfully")
	}

	// 4. check dhcpv4 server already exists
	if exist := c.dhcpV4.HasDHCPServer(networkStatus.Interface); exist {
		log.Warnf("(subnet.handlerDHCPV4) Subnet %s network provider %s DHCP service already exists", subnet.Name, subnet.Spec.Provider)
		return nil
	}

	// 5. if dhcpv4 server non-existent, add and run
	if err := c.dhcpV4.AddAndRun(networkStatus.Interface); err != nil {
		log.Warnf("(subnet.handlerDHCPV4) Subnet %s network provider %s DHCP service Startup failed: %v",
			subnet.Name, subnet.Spec.Provider, err)
		c.recorder.Event(subnet, corev1.EventTypeWarning, "DHCPServerError",
			fmt.Sprintf("The DHCP v4 server of network provider %s failed to start", subnet.Spec.Provider))
		return err
	}

	// 6. update dhcp v4 server gauge
	c.metrics.UpdateDHCPV4Info(networkStatus.Name, networkStatus.Interface, ovnSubnet.ServerIP.String(), ovnSubnet.ServerMac)

	c.recorder.Event(subnet, corev1.EventTypeNormal, "DHCPServer",
		fmt.Sprintf("The DHCP v4 server of network provider %s has been successfully started", subnet.Spec.Provider))

	return nil
}

func (c *Controller) handlerDHCPV6(subnet *kubeovnv1.Subnet, networkStatus networkv1.NetworkStatus) error {
	dhcpv6Options := strings.ReplaceAll(subnet.Spec.DHCPv6Options, " ", "")
	if dhcpv6Options != "" {
		//dhcpv6OptionsMap := util.ParseDHCPOptions(dhcpv6Options)
		log.Warnf("(subnet.handlerDHCPV6) DHCP v6 server is temporarily not supported")
	}
	return nil
}

func (c *Controller) handlerAdd(ctx context.Context, subnet *kubeovnv1.Subnet) error {
	// 1.check enable dhcp
	if !subnet.Spec.EnableDHCP {
		log.Infof("(subnet.handlerAdd) Subnet %s did not open DHCP service", subnet.Name)
		return nil
	}

	// 2.check provider
	split := strings.Split(subnet.Spec.Provider, ".")
	if len(split) != 2 {
		log.Infof("(subnet.handlerAdd) Subnet %s Invalid network provider %s", subnet.Name, subnet.Spec.Provider)
		return nil
	}
	multusName, multusNamespace := split[0], split[1]
	networkStatus, ok := c.networkInfos[fmt.Sprintf("%s/%s", multusNamespace, multusName)]
	if !ok {
		log.Warningf("(subnet.handlerAdd) Unsupported network providers %s", subnet.Spec.Provider)
		return nil
	}

	// 3.handler dhcp v4
	if err := c.handlerDHCPV4(subnet, networkStatus); err != nil {
		return err
	}

	// 4.handler dhcp v6
	if err := c.handlerDHCPV6(subnet, networkStatus); err != nil {
		return err
	}

	return nil
}

func (c *Controller) handlerUpdate(ctx context.Context, subnet *kubeovnv1.Subnet) error {
	// 1.check enable dhcp
	if !subnet.Spec.EnableDHCP {
		log.Infof("(subnet.handlerUpdate) Subnet %s did not open DHCP service", subnet.Name)
		return nil
	}

	// 2.check provider
	split := strings.Split(subnet.Spec.Provider, ".")
	if len(split) != 2 {
		log.Infof("(subnet.handlerUpdate) Subnet %s Invalid network provider %s", subnet.Name, subnet.Spec.Provider)
		return nil
	}
	multusName, multusNamespace := split[0], split[1]
	networkStatus, ok := c.networkInfos[fmt.Sprintf("%s/%s", multusNamespace, multusName)]
	if !ok {
		log.Warningf("(subnet.handlerUpdate) Unsupported network providers %s", subnet.Spec.Provider)
		return nil
	}

	// 3.update dhcp v4
	if err := c.handlerDHCPV4(subnet, networkStatus); err != nil {
		return err
	}

	// 4.update dhcp v6
	if err := c.handlerDHCPV6(subnet, networkStatus); err != nil {
		return err
	}

	return nil
}

func (c *Controller) handlerDelete(ctx context.Context, subnetKey types.NamespacedName, provider string) error {
	// 1.check provider
	split := strings.Split(provider, ".")
	if len(split) != 2 {
		log.Infof("(subnet.handlerDelete) Subnet %s Invalid network provider %s", subnetKey.Name, provider)
		return nil
	}
	multusName, multusNamespace := split[0], split[1]
	networkStatus, ok := c.networkInfos[fmt.Sprintf("%s/%s", multusNamespace, multusName)]
	if !ok {
		log.Infof("(subnet.handlerDelete) Unsupported network providers %s, Skip deletion", provider)
		return nil
	}

	// 2. remove dhcp ovn subnet
	c.dhcpV4.DeleteSubnet(subnetKey.Name)

	// 3. check Other subnet references
	subnets, err := c.subnetLister.GetByIndex(NetworkProviderIndexerKey, provider)
	if err != nil {
		log.Errorf("(subnet.handlerDelete) subnetLister.GetByIndex provider %s error: %v", provider, err)
		return err
	}
	if len(subnets) == 1 && subnets[0].Name == subnetKey.Name {
	} else if len(subnets) > 0 {
		log.Errorf("(subnet.handlerDelete) Network provider %s has other subnets in use and cannot delete the DHCP service", provider)
		return nil
	}

	// 4. delete and stop dhcp v4 server
	if err = c.dhcpV4.DelAndStop(networkStatus.Interface); err != nil {
		log.Errorf("(subnet.handlerDelete) Network provider %s stop dhcpv4 server error", provider)
		return err
	}

	// 5. delete dhcp v4 server gauge
	serverIP := util.GetFirstIPV4Addr(networkStatus)
	c.metrics.DeleteDHCPV4Info(networkStatus.Name, networkStatus.Interface, serverIP.String(), networkStatus.Mac)

	// 6. delete and stop dhcp v6 server

	return nil
}
