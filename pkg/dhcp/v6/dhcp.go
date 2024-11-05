package v6

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/server6"
	"github.com/insomniacslk/dhcp/iana"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
)

type OVNSubnet struct {
	ServerMac string   // dhcp服务器mac
	ServerIP  net.IP   // dhcp服务器ip
	NTP       []net.IP // ipv6 ntp地址
	DNS       []net.IP // ipv6 dns地址
	LeaseTime int      // 租约：秒 默认值：3600
}

type DHCPLease struct {
	ClientIP  net.IP
	SubnetKey string
	//PodKey    string
}

type DHCPAllocator struct {
	subnets map[string]OVNSubnet
	leases  map[string]DHCPLease
	indices map[string]sets.String // mac    -> podKey     mapping
	indexer map[string]sets.String // podKey -> macAddress mapping
	servers map[string]*server6.Server
	mutex   sync.RWMutex
}

func New() *DHCPAllocator {
	return NewDHCPAllocator()
}

func NewDHCPAllocator() *DHCPAllocator {
	subnets := make(map[string]OVNSubnet)
	leases := make(map[string]DHCPLease)
	indices := make(map[string]sets.String)
	indexer := make(map[string]sets.String)
	servers := make(map[string]*server6.Server)

	return &DHCPAllocator{
		subnets: subnets,
		leases:  leases,
		indices: indices,
		indexer: indexer,
		servers: servers,
	}
}

func (a *DHCPAllocator) GetSubnet(name string) (OVNSubnet, bool) {
	a.mutex.RLock()
	subnet, ok := a.subnets[name]
	a.mutex.RUnlock()
	return subnet, ok
}

func (a *DHCPAllocator) AddOrUpdateSubnet(
	name string,
	subnet OVNSubnet,
) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	_, ok := a.subnets[name]
	a.subnets[name] = subnet

	if ok {
		log.Debugf("(dhcpv6.AddOrUpdateSubnet) Subnet <%s> updated", name)
	} else {
		log.Debugf("(dhcpv6.AddOrUpdateSubnet) Subnet <%s> added", name)
	}

	return
}

func (a *DHCPAllocator) DeleteSubnet(name string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if name == "" {
		return fmt.Errorf("subnet name is empty")
	}

	if _, ok := a.subnets[name]; ok {
		delete(a.subnets, name)
		log.Debugf("(dhcpv6.DeleteSubnet) Subnet <%s> deleted", name)
	} else {
		log.Debugf("(dhcpv6.DeleteSubnet) Subnet <%s> is not found", name)
		return fmt.Errorf("subnet <%s> is not found", name)
	}
	return nil
}

func (a *DHCPAllocator) GetDHCPLease(hwAddr string) (DHCPLease, bool) {
	a.mutex.RLock()
	lease, ok := a.leases[hwAddr]
	a.mutex.RUnlock()
	return lease, ok
}

func (a *DHCPAllocator) AddPodDHCPLease(hwAddr, podKey string, dhcpLease DHCPLease) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if hwAddr == "" {
		return fmt.Errorf("hwaddr is empty")
	}

	if podKey == "" {
		return fmt.Errorf("pod key is empty")
	}

	if _, err := net.ParseMAC(hwAddr); err != nil {
		return fmt.Errorf("hwaddr <%s> is not valid", hwAddr)
	}

	a.leases[hwAddr] = dhcpLease

	// add mac to pod keys mapping
	if keySet, ok := a.indices[hwAddr]; ok {
		a.indices[hwAddr] = keySet.Insert(podKey)
	} else {
		a.indices[hwAddr] = sets.NewString(podKey)
	}

	// add pod key to macs mapping
	if macSet, ok := a.indexer[podKey]; ok {
		a.indexer[podKey] = macSet.Insert(hwAddr)
	} else {
		a.indexer[podKey] = sets.NewString(hwAddr)
	}

	log.Debugf("(dhcpv6.AddDHCPLease) lease added for hardware address: %s", hwAddr)

	return nil
}

func (a *DHCPAllocator) DeletePodDHCPLease(podKey string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if podKey == "" {
		return fmt.Errorf("pod key is empty")
	}

	macSet, ok := a.indexer[podKey]
	if !ok {
		log.Debugf("(dhcpv6.DeletePodDHCPLease) Pod <%s> not found in indexer", podKey)
		return fmt.Errorf("pod <%s> not found in indexer", podKey)
	}

	var delMacList []string
	for _, macAddr := range macSet.List() {
		keySet, ok := a.indices[macAddr]
		if ok && keySet.Equal(sets.NewString(podKey)) {
			delete(a.leases, macAddr)
			delete(a.indices, macAddr)
			delMacList = append(delMacList, macAddr)
		} else if ok {
			a.indices[macAddr] = keySet.Delete(podKey)
		}
	}
	log.Debugf("(dhcpv6.DeletePodDHCPLease) Pod <%s> lease deleted for hardware address: %+v", podKey, delMacList)

	delete(a.indexer, podKey)

	log.Debugf("(dhcpv6.AddDHCPLease) lease deleted for pod <%s>", podKey)

	return nil
}

func (a *DHCPAllocator) dhcpHandler(conn net.PacketConn, peer net.Addr, m dhcpv6.DHCPv6) {

	if m == nil {
		log.Errorf("(dhcpv6.dhcpHandler) packet is nil!")
		return
	}

	log.Tracef("(dhcpv6.dhcpHandler) INCOMING PACKET=%s", m.Summary())

	msg, err := m.GetInnerMessage()
	if err != nil {
		log.Errorf("(dhcpv6.dhcpHandler) failed loading inner message: %s", err)
		return
	}

	hwaddr, err := dhcpv6.ExtractMAC(m)
	if err != nil {
		log.Errorf("(dhcpv6.dhcpHandler) error extracting hwaddr: %s", err)
		return
	}

	lease, ok := a.GetDHCPLease(hwaddr.String())
	if !ok || lease.ClientIP == nil {
		log.Warnf("(dhcpv6.dhcpHandler) NO LEASE FOUND: hwaddr=%s", hwaddr.String())
		return
	}

	subnet, ok := a.GetSubnet(lease.SubnetKey)
	if !ok {
		log.Warnf("(dhcpv6.dhcpHandler) NO MATCHED SUBNET FOUND FOR LEASE: hwaddr=%s", hwaddr.String())
		return
	}

	log.Debugf("(dhcpv6.dhcpHandler) LEASE FOUND: hwaddr=%s, serverip=%s, serverid=%s, clientip=%s, dns=%+v, leasetime=%d",
		hwaddr.String(),
		subnet.ServerIP.String(),
		subnet.ServerMac,
		lease.ClientIP.String(),
		subnet.DNS,
		subnet.LeaseTime,
	)
	serverMac, _ := net.ParseMAC(subnet.ServerMac)

	modifiers := []dhcpv6.Modifier{
		dhcpv6.WithIANA(dhcpv6.OptIAAddress{ // set ip lease
			IPv6Addr:          lease.ClientIP,
			PreferredLifetime: time.Duration(subnet.LeaseTime) * time.Second,
			ValidLifetime:     time.Duration(subnet.LeaseTime) * time.Second,
		}),
		dhcpv6.WithServerID(&dhcpv6.DUIDLLT{ // set server mac
			HWType:        iana.HWTypeEthernet,
			Time:          dhcpv6.GetTime(),
			LinkLayerAddr: serverMac,
		}),
	}

	if len(subnet.DNS) > 0 {
		modifiers = append(modifiers, dhcpv6.WithDNS(subnet.DNS...))
	}
	if len(subnet.NTP) > 0 {
		so := dhcpv6.NTPSuboptionSrvAddr(subnet.NTP[0])
		modifiers = append(modifiers, dhcpv6.WithOption(&so))
	}

	//if match.Hostname != "" {
	//	modifiers = append(modifiers,
	//		dhcpv6.WithFQDN(0, match.Hostname),
	//	)
	//}

	var resp *dhcpv6.Message

	switch msg.MessageType { //nolint:exhaustive
	case dhcpv6.MessageTypeSolicit:
		if msg.GetOneOption(dhcpv6.OptionRapidCommit) == nil {
			log.Debugf("(dhcpv6.dhcpHandler) DHCPSOLICIT: %+v", msg)
			resp, err = dhcpv6.NewAdvertiseFromSolicit(msg, modifiers...)
			log.Debugf("(dhcpv6.dhcpHandler) DHCPADVERTISE: %+v", resp)
		} else {
			// for DHCP clients that support fast allocation, simply return a reply
			log.Debugf("(dhcpv6.dhcpHandler) DHCPRAPIDCOMMIT: %+v", msg)
			resp, err = dhcpv6.NewReplyFromMessage(msg, modifiers...)
			log.Debugf("(dhcpv6.dhcpHandler) DHCPREPLY: %+v", resp)
		}
	default:
		log.Debugf("(dhcpv6.dhcpHandler) DHCPREQUEST: %+v", msg)
		resp, err = dhcpv6.NewReplyFromMessage(msg, modifiers...)
		log.Debugf("(dhcpv6.dhcpHandler) DHCPREPLY: %+v", resp)
	}

	if err != nil {
		log.Errorf("(dhcpv6.dhcpHandler) Failure building response: %s", err)
		return
	}

	ianaRequest := msg.Options.OneIANA()
	if ianaRequest != nil {
		ianaResponse := resp.Options.OneIANA()
		ianaResponse.IaId = ianaRequest.IaId
		resp.UpdateOption(ianaResponse)
	}

	_, err = conn.WriteTo(resp.ToBytes(), peer)
	if err != nil {
		log.Errorf("(dhcpv6.dhcpHandler) Failure sending response: %s", err)
	}
}

func (a *DHCPAllocator) HasDHCPServer(nic string) bool {
	a.mutex.RLock()
	_, exist := a.servers[nic]
	a.mutex.RUnlock()
	return exist
}

func (a *DHCPAllocator) AddAndRun(nic string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	log.Infof("(dhcpv6.AddAndRun) starting DHCP service on nic <%s>", nic)

	laddr := net.UDPAddr{
		IP:   net.IPv6unspecified,
		Port: dhcpv6.DefaultServerPort,
	}

	server, err := server6.NewServer(nic, &laddr, a.dhcpHandler)
	if err != nil {
		return err
	}

	go server.Serve()

	a.servers[nic] = server

	return nil
}

func (a *DHCPAllocator) DelAndStop(nic string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	log.Infof("(dhcpv6.DelAndStop) stopping DHCP service on nic <%s>", nic)

	server, ok := a.servers[nic]
	if ok {
		if err := server.Close(); err != nil {
			return err
		}
		delete(a.servers, nic)
	}

	return nil
}
