// SPDX-License-Identifier:Apache-2.0

package collector

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/metallb/frr-k8s/cmd/metrics/vtysh"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	evpnVNIInfoDesc       = evpnDesc("vni_info", "EVPN VNI inventory; constant 1", "vni", "vrf", "type", "interface")
	evpnL2MACsDesc        = evpnDesc("l2_macs", "Host MAC addresses zebra holds for the L2 VNI", "vni", "vrf")
	evpnL2NeighborsDesc   = evpnDesc("l2_neighbors", "ARP/ND entries zebra holds for the L2 VNI", "vni", "vrf")
	evpnL2RemoteVTEPsDesc = evpnDesc("l2_remote_vteps", "Remote VTEPs known for the L2 VNI", "vni", "vrf")
	evpnL3RouterMACsDesc  = evpnDesc("l3_router_macs", "Router MACs zebra holds for the L3 VNI", "vni", "vrf")
	evpnL3NexthopsDesc    = evpnDesc("l3_nexthops", "Next hops zebra holds for the L3 VNI", "vni", "vrf")
	evpnSuccessDesc       = scrapeSuccessDesc("evpn")
)

func evpnDesc(name, help string, labels ...string) *prometheus.Desc {
	return prometheus.NewDesc(prometheus.BuildFQName(Namespace, "evpn", name), help, labels, nil)
}

type evpn struct {
	Log    log.Logger
	frrCli vtysh.Cli
}

func NewEVPN(l log.Logger) prometheus.Collector {
	return &evpn{Log: log.With(l, "collector", "evpn"), frrCli: vtysh.Run}
}

func (c *evpn) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{evpnVNIInfoDesc, evpnL2MACsDesc, evpnL2NeighborsDesc, evpnL2RemoteVTEPsDesc, evpnL3RouterMACsDesc, evpnL3NexthopsDesc, evpnSuccessDesc} {
		ch <- d
	}
}

func (c *evpn) Collect(ch chan<- prometheus.Metric) {
	out, err := c.frrCli("show evpn vni json")
	var ms []prometheus.Metric
	if err == nil {
		ms, err = parseEVPNMetrics(out)
	}
	success := 1.0
	if err != nil {
		success = 0
		level.Error(c.Log).Log("error", err, "msg", "failed to collect EVPN VNIs from FRR")
	}
	for _, m := range ms {
		ch <- m
	}
	ch <- prometheus.MustNewConstMetric(evpnSuccessDesc, prometheus.GaugeValue, success)
}

type evpnVNI struct {
	VNI       uint32  `json:"vni"`
	Type      string  `json:"type"`
	Interface string  `json:"vxlanIf"`
	VRF       string  `json:"tenantVrf"`
	MACs      *uint64 `json:"numMacs"`
	Neighbors *uint64 `json:"numArpNd"`
	// L3 VNIs report "n/a" instead of a numeric remote-VTEP count.
	RemoteVTEPs json.RawMessage `json:"numRemoteVteps"`
}

// parseEVPNMetrics builds the complete result before any measurements are sent.
// An empty object is valid when EVPN is disabled or has no VNIs.
func parseEVPNMetrics(out string) ([]prometheus.Metric, error) {
	var vnis map[string]evpnVNI
	if err := json.Unmarshal([]byte(out), &vnis); err != nil {
		return nil, fmt.Errorf("parsing EVPN VNIs: %w", err)
	}
	if vnis == nil {
		return nil, fmt.Errorf("EVPN VNIs must be an object, not null")
	}
	var ms []prometheus.Metric
	for key, v := range vnis {
		if v.VNI == 0 || v.VNI > 16777215 || strconv.FormatUint(uint64(v.VNI), 10) != key ||
			(v.Type != "L2" && v.Type != "L3") || v.VRF == "" || v.Interface == "" {
			return nil, fmt.Errorf("invalid identity, type, VRF or interface for EVPN VNI %q", key)
		}
		ms = append(ms, prometheus.MustNewConstMetric(evpnVNIInfoDesc, prometheus.GaugeValue, 1, key, v.VRF, v.Type, v.Interface))
		macsDesc, neighborsDesc := evpnL2MACsDesc, evpnL2NeighborsDesc
		var remote *uint64
		if v.Type == "L3" {
			macsDesc, neighborsDesc = evpnL3RouterMACsDesc, evpnL3NexthopsDesc
		} else if len(v.RemoteVTEPs) != 0 {
			if err := json.Unmarshal(v.RemoteVTEPs, &remote); err != nil {
				return nil, fmt.Errorf("parsing remote VTEP count for VNI %s: %w", key, err)
			}
		}
		for _, count := range []struct {
			desc  *prometheus.Desc
			value *uint64
		}{{macsDesc, v.MACs}, {neighborsDesc, v.Neighbors}, {evpnL2RemoteVTEPsDesc, remote}} {
			if count.value != nil {
				ms = append(ms, prometheus.MustNewConstMetric(count.desc, prometheus.GaugeValue, float64(*count.value), key, v.VRF))
			}
		}
	}
	return ms, nil
}
