package v4

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
)

type OVNSubnet struct {
	ServerMac  string     // dhcp服务器mac
	ServerIP   net.IP     // dhcp服务器ip
	SubnetMask net.IPMask // 子网掩码
	MTU        uint32
	Routers    []net.IP // 默认值 router=$ipv4_gateway
	NTP        []net.IP //
	DNS        []net.IP
	LeaseTime  int // 租约：秒 默认值：3600
}

type DHCPLease struct {
	ClientIP  net.IP
	SubnetKey string
}

type DHCPAllocator struct {
	subnets map[string]OVNSubnet
	leases  map[string]DHCPLease
	indices map[string]sets.String // mac    -> podKey     mapping
	indexer map[string]sets.String // podKey -> macAddress mapping
	servers map[string]*server4.Server
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
	servers := make(map[string]*server4.Server)

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
		log.Debugf("(dhcpv4.AddOrUpdateSubnet) subnet %s updated", name)
	} else {
		log.Debugf("(dhcpv4.AddOrUpdateSubnet) subnet %s added", name)
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
		log.Debugf("(dhcpv4.DeleteSubnet) subnet %s deleted", name)
	} else {
		log.Debugf("(dhcpv4.DeleteSubnet) subnet %s is not found", name)
		return fmt.Errorf("subnet %s is not found", name)
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
		return fmt.Errorf("hwaddr %s is not valid", hwAddr)
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

	log.Debugf("(dhcpv4.AddDHCPLease) lease added for hardware address: %s", hwAddr)

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
		log.Debugf("(dhcpv4.DeletePodDHCPLease) Pod %s not found in indexer", podKey)
		return fmt.Errorf("pod %s not found in indexer", podKey)
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
	log.Debugf("(dhcpv4.DeletePodDHCPLease) Pod %s lease deleted for hardware address: %+v", podKey, delMacList)

	delete(a.indexer, podKey)

	log.Debugf("(dhcpv4.AddDHCPLease) lease deleted for podKey: %s", podKey)

	return nil
}

func (a *DHCPAllocator) dhcpHandler(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4) {
	if m == nil {
		log.Errorf("(dhcpv4.dhcpHandler) packet is nil!")
		return
	}

	log.Tracef("(dhcpv4.dhcpHandler) INCOMING PACKET=%s", m.Summary())

	if m.OpCode != dhcpv4.OpcodeBootRequest {
		log.Errorf("(dhcpv4.dhcpHandler) not a BootRequest!")
		return
	}

	reply, err := dhcpv4.NewReplyFromRequest(m)
	if err != nil {
		log.Errorf("(dhcpv4.dhcpHandler) NewReplyFromRequest failed: %v", err)
		return
	}

	lease, ok := a.GetDHCPLease(m.ClientHWAddr.String())
	if !ok || lease.ClientIP == nil {
		log.Warnf("(dhcpv4.dhcpHandler) NO LEASE FOUND: hwaddr=%s", m.ClientHWAddr.String())
		return
	}

	subnet, ok := a.GetSubnet(lease.SubnetKey)
	if !ok {
		log.Warnf("(dhcpv4.dhcpHandler) NO MATCHED SUBNET FOUND FOR LEASE: hwaddr=%s", m.ClientHWAddr.String())
		return
	}

	log.Debugf("(dhcpv4.dhcpHandler) LEASE FOUND: hwaddr=%s, serverip=%s, clientip=%s, mask=%s, router=%+v, dns=%+v, ntp=%+v, leasetime=%d",
		m.ClientHWAddr.String(),
		subnet.ServerIP.String(),
		lease.ClientIP.String(),
		subnet.SubnetMask.String(),
		subnet.Routers,
		subnet.DNS,
		subnet.NTP,
		subnet.LeaseTime,
	)

	reply.ClientIPAddr = lease.ClientIP
	reply.ServerIPAddr = subnet.ServerIP
	reply.YourIPAddr = lease.ClientIP
	reply.TransactionID = m.TransactionID
	reply.ClientHWAddr = m.ClientHWAddr
	reply.Flags = m.Flags
	reply.GatewayIPAddr = m.GatewayIPAddr

	reply.UpdateOption(dhcpv4.OptServerIdentifier(subnet.ServerIP))
	reply.UpdateOption(dhcpv4.OptSubnetMask(subnet.SubnetMask))
	reply.UpdateOption(dhcpv4.OptRouter(subnet.Routers...))

	if subnet.MTU > 0 && reply.IsOptionRequested(dhcpv4.OptionInterfaceMTU) {
		reply.UpdateOption(dhcpv4.OptGeneric(
			dhcpv4.OptionInterfaceMTU, dhcpv4.Uint16(subnet.MTU).ToBytes()))
	}
	if len(subnet.DNS) > 0 {
		reply.UpdateOption(dhcpv4.OptDNS(subnet.DNS...))
	}

	//if pool.DomainName != "" {
	//	reply.UpdateOption(dhcpv4.OptDomainName(pool.DomainName))
	//}
	//
	//if len(pool.DomainSearch) > 0 {
	//	dsl := rfc1035label.NewLabels()
	//	dsl.Labels = append(dsl.Labels, pool.DomainSearch...)
	//
	//	reply.UpdateOption(dhcpv4.OptDomainSearch(dsl))
	//}
	//
	if len(subnet.NTP) > 0 {
		reply.UpdateOption(dhcpv4.OptNTPServers(subnet.NTP...))
	}

	reply.UpdateOption(dhcpv4.OptIPAddressLeaseTime(time.Duration(subnet.LeaseTime) * time.Second))

	switch mt := m.MessageType(); mt {
	case dhcpv4.MessageTypeDiscover:
		log.Debugf("(dhcpv4.dhcpHandler) DHCPDISCOVER: %+v", m)
		reply.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeOffer))
		log.Debugf("(dhcpv4.dhcpHandler) DHCPOFFER: %+v", reply)
	case dhcpv4.MessageTypeRequest:
		log.Debugf("(dhcpv4.dhcpHandler) DHCPREQUEST: %+v", m)
		reply.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeAck))
		log.Debugf("(dhcpv4.dhcpHandler) DHCPACK: %+v", reply)
	default:
		log.Warnf("(dhcpv4.dhcpHandler) Unhandled message type for hwaddr [%s]: %v", m.ClientHWAddr.String(), mt)
		return
	}

	if _, err := conn.WriteTo(reply.ToBytes(), peer); err != nil {
		log.Errorf("(dhcpv4.dhcpHandler) Cannot reply to client: %v", err)
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

	log.Infof("(dhcpv4.AddAndRun) starting DHCP service on nic %s", nic)

	// we need to listen on 0.0.0.0 otherwise client discovers will not be answered
	laddr := net.UDPAddr{
		IP:   net.ParseIP("0.0.0.0"),
		Port: 67,
	}

	server, err := server4.NewServer(nic, &laddr, a.dhcpHandler)
	if err != nil {
		return err
	}

	go server.Serve()

	a.servers[nic] = server

	log.Debugf("(dhcpv4.AddAndRun) DHCP server on nic %s has started", nic)

	return nil
}

func (a *DHCPAllocator) DelAndStop(nic string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	log.Infof("(dhcp.DelAndStop) stopping DHCP service on nic %s", nic)

	server, ok := a.servers[nic]
	if ok {
		if err := server.Close(); err != nil {
			return err
		}
		delete(a.servers, nic)

		log.Debugf("(dhcpv4.DelAndStop) DHCP server on nic %s has stopped", nic)
	}

	return nil
}
