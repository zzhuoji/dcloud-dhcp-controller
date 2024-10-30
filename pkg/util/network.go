package util

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	log "github.com/sirupsen/logrus"
	kubeovnv1 "tydic.io/dcloud-dhcp-controller/pkg/apis/kubeovn/v1"
	"tydic.io/dcloud-dhcp-controller/pkg/dhcp"
)

func BuildOVNSubnetByIPV4Options(
	subnet *kubeovnv1.Subnet,
	networkStatus networkv1.NetworkStatus,
	dhcpv4OptionsMap map[string]string) (*dhcp.OVNSubnet, error) {

	ovnSubnet := &dhcp.OVNSubnet{}
	_, err := net.ParseMAC(networkStatus.Mac)
	if err != nil {
		return nil, fmt.Errorf("Conversion of multus %s network interface %s MAC address failed: %v", networkStatus.Name, networkStatus.Interface, err)
	}
	ovnSubnet.ServerMac = networkStatus.Mac
	serverIP := GetFirstIPV4Addr(networkStatus)
	if serverIP == nil {
		return nil, fmt.Errorf("Unable to find multus %s network interface %s IPv4 address", networkStatus.Name, networkStatus.Interface)
	}

	ovnSubnet.ServerIP = serverIP

	leaseTime, err := strconv.Atoi(dhcpv4OptionsMap["lease_time"])
	if err != nil || leaseTime <= 0 {
		leaseTime = 3600
	}
	ovnSubnet.LeaseTime = leaseTime
	var routers []net.IP
	for _, ipstr := range strings.Split(dhcpv4OptionsMap["router"], ",") {
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
		ntpIP := net.ParseIP(ipstr)
		if ntpIP != nil && ntpIP.To4() != nil {
			ntp = append(ntp, ntpIP)
			continue
		}
		// If NTP is a domain name, convert it to IP from the local network
		hostIPs, err := net.LookupIP(ipstr)
		if err != nil {
			log.Errorf("(subnet.handlerAdd) cannot get any ip addresses from ntp domainname entry %s: %s", ipstr, err)
		}
		for _, ip := range hostIPs {
			if ip.To4() != nil {
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
		if IsIPv4(ipstr) {
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

//func CheckProtocol(address string) string {
//	if address == "" {
//		return ""
//	}
//
//	ips := strings.Split(address, ",")
//	if len(ips) == 2 {
//		IP1 := net.ParseIP(strings.Split(ips[0], "/")[0])
//		IP2 := net.ParseIP(strings.Split(ips[1], "/")[0])
//		if IP1.To4() != nil && IP2.To4() == nil && IP2.To16() != nil {
//			return kubeovnv1.ProtocolDual
//		}
//		if IP2.To4() != nil && IP1.To4() == nil && IP1.To16() != nil {
//			return kubeovnv1.ProtocolDual
//		}
//		err := fmt.Errorf("invalid address %q", address)
//		klog.Error(err)
//		return ""
//	}
//
//	address = strings.Split(address, "/")[0]
//	ip := net.ParseIP(address)
//	if ip.To4() != nil {
//		return kubeovnv1.ProtocolIPv4
//	} else if ip.To16() != nil {
//		return kubeovnv1.ProtocolIPv6
//	}
//
//	// cidr format error
//	err := fmt.Errorf("invalid address %q", address)
//	klog.Error(err)
//	return ""
//}

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
