package metrics

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type MetricsAllocator struct {
	httpServer http.Server

	// dhcp v4 info
	dcloud_dhcp_v4_server_info *prometheus.GaugeVec
	// dhcp v6 info
	dcloud_dhcp_v6_server_info *prometheus.GaugeVec

	registry *prometheus.Registry
}

func NewMetricsAllocator() *MetricsAllocator {
	m := &MetricsAllocator{
		dcloud_dhcp_v4_server_info: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "dcloud_dhcp_v4_server_info",
				Help: "DCloud dhcp v4 server information",
			},
			[]string{"network", "interface", "ip", "mac", "port"},
		),
		dcloud_dhcp_v6_server_info: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "dcloud_dhcp_v6_server_info",
				Help: "DCloud dhcp v6 server information",
			},
			[]string{"network", "interface", "ip", "mac", "port"},
		),
	}

	m.registry = prometheus.NewRegistry()
	m.registry.MustRegister(m.dcloud_dhcp_v4_server_info)
	m.registry.MustRegister(m.dcloud_dhcp_v6_server_info)
	return m
}

func (m *MetricsAllocator) UpdateDHCPV4Info(networkName, iface, ip, mac string) {
	m.dcloud_dhcp_v4_server_info.WithLabelValues(networkName, iface, ip, mac, "67").Set(float64(1))
}

func (m *MetricsAllocator) DeleteDHCPV4Info(networkName, iface, ip, mac string) {
	m.dcloud_dhcp_v4_server_info.DeleteLabelValues(networkName, iface, ip, mac, "67")
}

func (m *MetricsAllocator) UpdateDHCPV6Info(networkName, iface, ip, mac string) {
	m.dcloud_dhcp_v6_server_info.WithLabelValues(networkName, iface, ip, mac, "547").Set(float64(1))
}

func (m *MetricsAllocator) DeleteDHCPV6Info(networkName, iface, ip, mac string) {
	m.dcloud_dhcp_v6_server_info.DeleteLabelValues(networkName, iface, ip, mac, "547")
}

//func (m *MetricsAllocator) DeleteVmNetCfgStatus(vmName string) {
//	var vmnetCfgMetrics []prometheus.Labels
//	var labelFound bool
//
//	// gather all metrics so we make sure we delete all of them
//	gatherer := prometheus.Gatherer(m.registry)
//	mfs, err := gatherer.Gather()
//	if err != nil {
//		log.Errorf("(metrics.DeleteVmNetCfgStatus) error while gathering metrics for vm [%s]: %s",
//			vmName, err.Error())
//
//		return
//	}
//	for _, mf := range mfs {
//		if mf.GetName() == "kubevirtiphelper_vmnetcfg_status" {
//			for _, m := range mf.GetMetric() {
//				labelFound = false
//				pLabel := make(map[string]string)
//				for _, l := range m.GetLabel() {
//					pLabel[l.GetName()] = l.GetValue()
//					if l.GetName() == LabelVMName && l.GetValue() == vmName {
//						labelFound = true
//					}
//				}
//				if labelFound {
//					vmnetCfgMetrics = append(vmnetCfgMetrics, pLabel)
//				}
//			}
//		}
//	}
//
//	// delete the metrics which contain the vm name
//	for _, pl := range vmnetCfgMetrics {
//		m.kubevirtiphelperVmNetCfgStatus.Delete(pl)
//	}
//}

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

	log.Infof("(metrics.Run) %s", m.httpServer.ListenAndServe())
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
