package util

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	log "github.com/sirupsen/logrus"
	"tydic.io/dcloud-dhcp-controller/pkg/dhcp/v4"
	v6 "tydic.io/dcloud-dhcp-controller/pkg/dhcp/v6"
)

// BuildOVNSubnetByIPV4Options
// parameters: lease_time \ router \ ntp_server \ dns_server
// example :
//
//	dhcpOptions: "lease_time=3600,router={192.168.1.1;192.168.2.1},ntp_server=10.20.10.19,dns_server={8.8.8.8;8.8.4.4}"
func BuildOVNSubnetByIPV4Options(
	subnet *kubeovnv1.Subnet,
	networkStatus networkv1.NetworkStatus,
	dhcpv4OptionsMap map[string]string) (*v4.OVNSubnet, error) {

	ovnSubnet := &v4.OVNSubnet{}
	_, err := net.ParseMAC(networkStatus.Mac)
	if err != nil {
		return nil, fmt.Errorf("conversion of multus network <%s> interface <%s> MAC address failed: %v", networkStatus.Name, networkStatus.Interface, err)
	}
	ovnSubnet.ServerMac = networkStatus.Mac
	serverIP := GetFirstIPV4Addr(networkStatus)
	if serverIP == nil {
		return nil, fmt.Errorf("unable to find multus network <%s> interface <%s> IPv4 address", networkStatus.Name, networkStatus.Interface)
	}
	ovnSubnet.ServerIP = serverIP

	ovnSubnet.MTU = subnet.Spec.Mtu
	mtu, err := strconv.ParseUint(dhcpv4OptionsMap["mtu"], 10, 32)
	if err == nil {
		ovnSubnet.MTU = uint32(mtu)
	}
	leaseTime, err := strconv.Atoi(dhcpv4OptionsMap["lease_time"])
	if err != nil || leaseTime <= 0 {
		leaseTime = 3600
	}
	ovnSubnet.LeaseTime = leaseTime
	var routers []net.IP
	for _, ipstr := range strings.Split(dhcpv4OptionsMap["router"], ",") {
		if ipstr == "" {
			continue
		}
		if IsIPv4(ipstr) {
			routers = append(routers, net.ParseIP(ipstr))
		}
	}
	// There are no available routers with default IPv4 gateway settings
	if len(routers) == 0 {
		ipv4Gateway := strings.Split(subnet.Spec.Gateway, ",")[0]
		if IsIPv4(ipv4Gateway) {
			routers = append(routers, net.ParseIP(ipv4Gateway))
		}
	}
	ovnSubnet.Routers = routers
	var ntp []net.IP
	for _, ipstr := range strings.Split(dhcpv4OptionsMap["ntp_server"], ",") {
		if ipstr == "" {
			continue
		}
		if IsIPv4(ipstr) {
			ntp = append(ntp, net.ParseIP(ipstr))
			continue
		}
		// If NTP is a domain name, convert it to IP from the local network
		hostIPs, err := net.LookupIP(ipstr)
		if err != nil {
			log.Debugf("cannot get any ip addresses from ntp domainname entry <%s>: %s", ipstr, err)
		}
		for _, ip := range hostIPs {
			if ip != nil && ip.To4() != nil {
				ntp = append(ntp, ip)
			}
		}
	}
	ovnSubnet.NTP = ntp
	var subnetMask net.IPMask
	ipv4Cidr := strings.Split(subnet.Spec.CIDRBlock, ",")[0]
	_, ipNet, err := net.ParseCIDR(strings.TrimSpace(ipv4Cidr))
	if err != nil {
		// 默认24
		subnetMask = net.CIDRMask(24, 32)
	} else {
		subnetMask = ipNet.Mask
	}
	ovnSubnet.SubnetMask = subnetMask

	var dns []net.IP
	for _, ipstr := range strings.Split(dhcpv4OptionsMap["dns_server"], ",") {
		if ipstr == "" {
			continue
		}
		if IsIPv4(ipstr) {
			dns = append(dns, net.ParseIP(ipstr))
		}
	}
	ovnSubnet.DNS = dns
	return ovnSubnet, nil
}

// BuildOVNSubnetByIPV6Options
// parameters: lease_time \ ntp_server \ dns_server
func BuildOVNSubnetByIPV6Options(
	networkStatus networkv1.NetworkStatus,
	dhcpv6OptionsMap map[string]string) (*v6.OVNSubnet, error) {

	ovnSubnet := &v6.OVNSubnet{}
	_, err := net.ParseMAC(networkStatus.Mac)
	if err != nil {
		return nil, fmt.Errorf("conversion of multus network <%s> interface <%s> MAC address failed: %v", networkStatus.Name, networkStatus.Interface, err)
	}
	ovnSubnet.ServerMac = networkStatus.Mac
	serverIP := GetFirstIPV6Addr(networkStatus)
	if serverIP == nil {
		return nil, fmt.Errorf("unable to find multus network <%s> interface <%s> IPv6 address", networkStatus.Name, networkStatus.Interface)
	}
	ovnSubnet.ServerIP = serverIP

	leaseTime, err := strconv.Atoi(dhcpv6OptionsMap["lease_time"])
	if err != nil || leaseTime <= 0 {
		leaseTime = 3600
	}
	ovnSubnet.LeaseTime = leaseTime
	var ntp []net.IP
	for _, ipstr := range strings.Split(dhcpv6OptionsMap["ntp_server"], ",") {
		if ipstr == "" {
			continue
		}
		if IsIPv6(ipstr) {
			ntp = append(ntp, net.ParseIP(ipstr))
			continue
		}
		// If NTP is a domain name, convert it to IP from the local network
		hostIPs, err := net.LookupIP(ipstr)
		if err != nil {
			log.Debugf("cannot get any ip addresses from ntp domainname entry <%s>: %s", ipstr, err)
		}
		for _, ip := range hostIPs {
			if ip != nil && ip.To16() != nil {
				ntp = append(ntp, ip)
			}
		}
	}

	var dns []net.IP
	for _, ipstr := range strings.Split(dhcpv6OptionsMap["dns_server"], ",") {
		if ipstr == "" {
			continue
		}
		if IsIPv6(ipstr) {
			dns = append(dns, net.ParseIP(ipstr))
		}
	}
	ovnSubnet.DNS = dns
	return ovnSubnet, nil
}

func IsIPv4(ipAddr string) bool {
	ip := net.ParseIP(ipAddr)
	return ip != nil && strings.Contains(ipAddr, ".")
}

func IsIPv6(ipAddr string) bool {
	ip := net.ParseIP(ipAddr)
	return ip != nil && strings.Contains(ipAddr, ":")
}

func GetFirstIPV6Addr(status networkv1.NetworkStatus) net.IP {
	for _, ip := range status.IPs {
		if IsIPv6(ip) {
			return net.ParseIP(ip)
		}
	}
	return nil
}

func GetFirstIPV4Addr(status networkv1.NetworkStatus) net.IP {
	for _, ip := range status.IPs {
		if IsIPv4(ip) {
			return net.ParseIP(ip)
		}
	}
	return nil
}

// ParseDHCPOptions 提取 DHCP 选项值，并支持包含 {} 的值。
func ParseDHCPOptions(dhcpStr string) map[string]string {
	dhcpOptions := make(map[string]string)

	var key, value string
	var inKey, inValue bool
	var depth int

	// 遍历每个字符
	for _, char := range dhcpStr {
		if char == '{' {
			depth++
		} else if char == '}' {
			depth--
		}

		if depth == 0 && char == '=' {
			if inKey {
				key = strings.TrimSpace(key)
				inKey = false
				inValue = true
			}
		} else if depth == 0 && char == ',' {
			if inValue {
				value = strings.TrimSpace(value)
				dhcpOptions[key] = value
				key, value = "", ""
				inValue = false
				inKey = false
			}
		} else {
			if inValue || inKey {
				if inKey {
					key += string(char)
				} else if inValue {
					value += string(char)
				}
			} else if char != ' ' {
				inKey = true
				key += string(char)
			}
		}
	}

	// 处理最后一个键值对
	if inValue {
		value = strings.TrimSpace(value)
		dhcpOptions[key] = value
	}

	// 处理包含 {} 的值
	for k, v := range dhcpOptions {
		if strings.HasPrefix(v, "{") && strings.HasSuffix(v, "}") {
			v = strings.Trim(v, "{}")
		}
		v = strings.ReplaceAll(v, ";", ",")
		dhcpOptions[k] = v
	}

	return dhcpOptions
}
