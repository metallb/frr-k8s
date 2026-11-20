// SPDX-License-Identifier:Apache-2.0

package collector

import (
	"strconv"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/metallb/frr-k8s/cmd/metrics/vtysh"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/prometheus/client_golang/prometheus"
)

const evpnSubsystem = "evpn"

var (
	evpnVNIInfoDesc       = evpnDesc("vni_info", "EVPN VNI inventory; constant 1", "vni", "vrf", "type", "interface")
	evpnL2MACsDesc        = evpnDesc("l2_macs", "Host MAC addresses zebra holds for the L2 VNI", "vni", "vrf")
	evpnL2NeighborsDesc   = evpnDesc("l2_neighbors", "ARP/ND entries zebra holds for the L2 VNI", "vni", "vrf")
	evpnL2RemoteVTEPsDesc = evpnDesc("l2_remote_vteps", "Remote VTEPs known for the L2 VNI", "vni", "vrf")
	evpnL3RouterMACsDesc  = evpnDesc("l3_router_macs", "Router MACs zebra holds for the L3 VNI", "vni", "vrf")
	evpnL3NexthopsDesc    = evpnDesc("l3_nexthops", "Next hops zebra holds for the L3 VNI", "vni", "vrf")
	evpnSuccessDesc       = scrapeSuccessDesc(evpnSubsystem)
)

func evpnDesc(name, help string, labels ...string) *prometheus.Desc {
	return prometheus.NewDesc(prometheus.BuildFQName(Namespace, evpnSubsystem, name), help, labels, nil)
}

type evpn struct {
	Log    log.Logger
	frrCli vtysh.Cli
}

func NewEVPN(l log.Logger, cli vtysh.Cli) prometheus.Collector {
	log := log.With(l, "collector", evpnSubsystem)
	return &evpn{Log: log, frrCli: cli}
}

func (c *evpn) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{evpnVNIInfoDesc, evpnL2MACsDesc, evpnL2NeighborsDesc, evpnL2RemoteVTEPsDesc, evpnL3RouterMACsDesc, evpnL3NexthopsDesc, evpnSuccessDesc} {
		ch <- d
	}
}

func (c *evpn) Collect(ch chan<- prometheus.Metric) {
	success := 0.0
	defer func() {
		ch <- prometheus.MustNewConstMetric(evpnSuccessDesc, prometheus.GaugeValue, success)
	}()

	vnis, err := vtysh.GetEVPNVNIs(c.frrCli)
	if err != nil {
		level.Error(c.Log).Log("error", err, "msg", "failed to collect EVPN VNIs from FRR")
		return
	}

	updateEVPNMetrics(ch, vnis)
	success = 1
}

func updateEVPNMetrics(ch chan<- prometheus.Metric, vnis []frr.EVPNVNI) {
	for _, v := range vnis {
		key := strconv.FormatUint(uint64(v.VNI), 10)
		ch <- prometheus.MustNewConstMetric(evpnVNIInfoDesc, prometheus.GaugeValue, 1, key, v.VRF, v.Type, v.Interface)
		macsDesc, neighborsDesc := evpnL2MACsDesc, evpnL2NeighborsDesc
		if v.Type == "L3" {
			macsDesc, neighborsDesc = evpnL3RouterMACsDesc, evpnL3NexthopsDesc
		}
		for _, count := range []struct {
			desc  *prometheus.Desc
			value *uint64
		}{{macsDesc, v.MACs}, {neighborsDesc, v.Neighbors}, {evpnL2RemoteVTEPsDesc, v.RemoteVTEPs}} {
			if count.value != nil {
				ch <- prometheus.MustNewConstMetric(count.desc, prometheus.GaugeValue, float64(*count.value), key, v.VRF)
			}
		}
	}
}
