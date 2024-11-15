package metrics

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
)

type MetricsAllocator struct {
	httpServer http.Server

	// dhcp v4 info
	dcloud_dhcp_v4_server_info *prometheus.GaugeVec
	// dhcp v6 info
	dcloud_dhcp_v6_server_info *prometheus.GaugeVec
	// dhcp subnet ingo
	dcloud_dhcp_subnet_info *prometheus.GaugeVec

	// vm dhcp v4 lease time
	dcloud_vm_dhcp_v4_lease_time *prometheus.GaugeVec
	// vm dhcp v6 lease time
	dcloud_vm_dhcp_v6_lease_time *prometheus.GaugeVec

	registry *prometheus.Registry
}

func NewMetricsAllocator() *MetricsAllocator {
	m := &MetricsAllocator{
		dcloud_dhcp_v4_server_info: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "dcloud_dhcp_v4_server_info",
				Help: "DCloud DHCPv4 server information",
			},
			[]string{"network", "interface", "ip", "mac", "port"},
		),
		dcloud_dhcp_v6_server_info: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "dcloud_dhcp_v6_server_info",
				Help: "DCloud DHCPv6 server information",
			},
			[]string{"network", "interface", "ip", "mac", "port"},
		),
		dcloud_dhcp_subnet_info: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "dcloud_dhcp_subnet_info",
				Help: "DCloud dhcp subnet information",
			},
			[]string{"name", "provider", "cidr", "protocol", "gateway", "dhcpv4", "dhcpv6"},
		),
		dcloud_vm_dhcp_v4_lease_time: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "dcloud_vm_dhcp_v4_lease_time",
				Help: "DCloud virtual machine DHCPv4 lease time (second)",
			},
			[]string{"vm", "subnet", "ip", "mac"},
		),
		dcloud_vm_dhcp_v6_lease_time: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "dcloud_vm_dhcp_v6_lease_time",
				Help: "DCloud virtual machine DHCPv6 lease time (second)",
			},
			[]string{"vm", "subnet", "ip", "mac"},
		),
	}

	m.registry = prometheus.NewRegistry()
	m.registry.MustRegister(m.dcloud_dhcp_v4_server_info)
	m.registry.MustRegister(m.dcloud_dhcp_v6_server_info)
	m.registry.MustRegister(m.dcloud_dhcp_subnet_info)
	m.registry.MustRegister(m.dcloud_vm_dhcp_v4_lease_time)
	m.registry.MustRegister(m.dcloud_vm_dhcp_v6_lease_time)
	return m
}

func (m *MetricsAllocator) UpdateDHCPv4ServerInfo(networkName, iface, ip, mac string) {
	m.DeleteDHCPv4ServerInfo(networkName)
	m.dcloud_dhcp_v4_server_info.WithLabelValues(networkName, iface, ip, mac, "67").Set(float64(1))
}

func (m *MetricsAllocator) DeleteDHCPv4ServerInfo(networkName string) {
	m.dcloud_dhcp_v4_server_info.DeletePartialMatch(prometheus.Labels{"network": networkName})
}

func (m *MetricsAllocator) UpdateDHCPv6ServerInfo(networkName, iface, ip, mac string) {
	m.DeleteDHCPv6ServerInfo(networkName)
	m.dcloud_dhcp_v6_server_info.WithLabelValues(networkName, iface, ip, mac, "547").Set(float64(1))
}

func (m *MetricsAllocator) DeleteDHCPv6ServerInfo(networkName string) {
	m.dcloud_dhcp_v6_server_info.DeletePartialMatch(prometheus.Labels{"network": networkName})
}

func (m *MetricsAllocator) UpdateDHCPSubnetInfo(name, provider, cidr, protocol, gateway string, dhcpv4, dhcpv6 bool) {
	m.DeleteDHCPSubnetInfo(name)
	m.dcloud_dhcp_subnet_info.WithLabelValues(name, provider, cidr, protocol, gateway,
		strconv.FormatBool(dhcpv4), strconv.FormatBool(dhcpv6)).Set(float64(1))
}

func (m *MetricsAllocator) DeleteDHCPSubnetInfo(name string) {
	m.dcloud_dhcp_subnet_info.DeletePartialMatch(prometheus.Labels{"name": name})
}

func (m *MetricsAllocator) UpdateVMDHCPv4Lease(vmKey, subnetName, ip, mac string, lease int) {
	m.DeleteVMDHCPv4Lease(vmKey, mac)
	m.dcloud_vm_dhcp_v4_lease_time.WithLabelValues(vmKey, subnetName, ip, mac).Set(float64(lease))
}

func (m *MetricsAllocator) DeleteVMDHCPv4Lease(vmKey string, mac string) {
	if mac == "" {
		m.dcloud_vm_dhcp_v4_lease_time.DeletePartialMatch(prometheus.Labels{"vm": vmKey})
	} else {
		m.dcloud_vm_dhcp_v4_lease_time.DeletePartialMatch(prometheus.Labels{"vm": vmKey, "mac": mac})
	}
}

func (m *MetricsAllocator) DeletePartialVMDHCPv4Lease(vmKey string, reservedMacs []string) {
	m.deletePartialVMDHCPLease("dcloud_vm_dhcp_v4_lease_time", vmKey, reservedMacs, m.DeleteVMDHCPv4Lease)
}

func (m *MetricsAllocator) UpdateVMDHCPv6Lease(vmKey, subnetName, ip, mac string, lease int) {
	m.DeleteVMDHCPv6Lease(vmKey, mac)
	m.dcloud_vm_dhcp_v6_lease_time.WithLabelValues(vmKey, subnetName, ip, mac).Set(float64(lease))
}

func (m *MetricsAllocator) DeleteVMDHCPv6Lease(vmKey string, mac string) {
	if mac == "" {
		m.dcloud_vm_dhcp_v6_lease_time.DeletePartialMatch(prometheus.Labels{"vm": vmKey})
	} else {
		m.dcloud_vm_dhcp_v6_lease_time.DeletePartialMatch(prometheus.Labels{"vm": vmKey, "mac": mac})
	}
}

func (m *MetricsAllocator) DeletePartialVMDHCPv6Lease(vmKey string, reservedMacs []string) {
	m.deletePartialVMDHCPLease("dcloud_vm_dhcp_v6_lease_time", vmKey, reservedMacs, m.DeleteVMDHCPv6Lease)
}

func (m *MetricsAllocator) deletePartialVMDHCPLease(gaugeName, vmKey string, reservedMacs []string, deleteFunc func(string, string)) {
	// gather all metrics so we make sure we delete all of them
	mfs, err := prometheus.Gatherer(m.registry).Gather()
	if err != nil {
		log.Errorf("(metrics.deletePartialVMDHCPLease) error while gathering metrics for vm <%s> gauge <%s>: %v", vmKey, gaugeName, err)
		return
	}
	index := slices.IndexFunc(mfs, func(mf *dto.MetricFamily) bool { return mf.GetName() == gaugeName })
	if index < 0 {
		return
	}
	var deleteMacs []string
	macSet := sets.NewString(reservedMacs...)
	for _, metric := range mfs[index].GetMetric() {
		matchVM := slices.ContainsFunc(metric.GetLabel(), func(label *dto.LabelPair) bool {
			return label.GetName() == "vm" && label.GetValue() == vmKey
		})
		if !matchVM {
			continue
		}
		var mac string
		matchMac := slices.ContainsFunc(metric.GetLabel(), func(label *dto.LabelPair) bool {
			mac = label.GetValue()
			return label.GetName() == "mac" && !macSet.Has(label.GetValue())
		})
		if matchMac {
			deleteMacs = append(deleteMacs, mac)
		}
	}
	for _, mac := range deleteMacs {
		deleteFunc(vmKey, mac)
	}
}

func (m *MetricsAllocator) Run(ctx context.Context) {
	log.Infof("(metrics.Run) starting Metrics service")

	metricsPort, err := strconv.Atoi(os.Getenv("METRICS_PORT"))
	if err != nil {
		metricsPort = 8080
	}
	listenAddress := fmt.Sprintf(":%d", metricsPort)

	m.httpServer = http.Server{
		Addr:    listenAddress,
		Handler: promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{Registry: m.registry}),
	}

	go func() {
		<-ctx.Done()
		m.stop()
	}()

	log.Infof("(metrics.Run) %v", m.httpServer.ListenAndServe())
}

func (m *MetricsAllocator) stop() {
	log.Infof("(metrics.Stop) stopping Metrics service")
	if err := m.httpServer.Shutdown(context.Background()); err != nil {
		log.Errorf("(metrics.Stop) error while stopping Metrics service: %s", err.Error())
	}
}

func New() *MetricsAllocator {
	return NewMetricsAllocator()
}
