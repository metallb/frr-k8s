// SPDX-License-Identifier:Apache-2.0

package collector

import (
	"strings"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const evpnCapture = `{
"100":{"vni":100,"type":"L2","vxlanIf":"vxlan100","numMacs":2,"numRemoteVteps":0,"tenantVrf":"default"},
"10100":{"vni":10100,"type":"L2","vxlanIf":"vxlan10100","numMacs":2,"numArpNd":0,"numRemoteVteps":1,"tenantVrf":"default"},
"50001":{"vni":50001,"type":"L3","vxlanIf":"vxlan50001","numMacs":3,"numArpNd":4,"tenantVrf":"red"},
"50002":{"vni":50002,"type":"L3","vxlanIf":"vxlan50002","numMacs":0,"numArpNd":0,"tenantVrf":"red"}}
`

func TestEVPN(t *testing.T) {
	expected := `# HELP frrk8s_evpn_l2_macs Host MAC addresses zebra holds for the L2 VNI
# TYPE frrk8s_evpn_l2_macs gauge
frrk8s_evpn_l2_macs{vni="100",vrf="default"} 2
frrk8s_evpn_l2_macs{vni="10100",vrf="default"} 2
# HELP frrk8s_evpn_l2_neighbors ARP/ND entries zebra holds for the L2 VNI
# TYPE frrk8s_evpn_l2_neighbors gauge
frrk8s_evpn_l2_neighbors{vni="10100",vrf="default"} 0
# HELP frrk8s_evpn_l2_remote_vteps Remote VTEPs known for the L2 VNI
# TYPE frrk8s_evpn_l2_remote_vteps gauge
frrk8s_evpn_l2_remote_vteps{vni="100",vrf="default"} 0
frrk8s_evpn_l2_remote_vteps{vni="10100",vrf="default"} 1
# HELP frrk8s_evpn_l3_router_macs Router MACs zebra holds for the L3 VNI
# TYPE frrk8s_evpn_l3_router_macs gauge
frrk8s_evpn_l3_router_macs{vni="50001",vrf="red"} 3
frrk8s_evpn_l3_router_macs{vni="50002",vrf="red"} 0
# HELP frrk8s_evpn_l3_nexthops Next hops zebra holds for the L3 VNI
# TYPE frrk8s_evpn_l3_nexthops gauge
frrk8s_evpn_l3_nexthops{vni="50001",vrf="red"} 4
frrk8s_evpn_l3_nexthops{vni="50002",vrf="red"} 0
# HELP frrk8s_evpn_vni_info EVPN VNI inventory; constant 1
# TYPE frrk8s_evpn_vni_info gauge
frrk8s_evpn_vni_info{interface="vxlan100",type="L2",vni="100",vrf="default"} 1
frrk8s_evpn_vni_info{interface="vxlan10100",type="L2",vni="10100",vrf="default"} 1
frrk8s_evpn_vni_info{interface="vxlan50001",type="L3",vni="50001",vrf="red"} 1
frrk8s_evpn_vni_info{interface="vxlan50002",type="L3",vni="50002",vrf="red"} 1
` + scrapeSuccessText("evpn", 1)
	checkEVPN(t, evpnCapture, expected)
}

func TestEVPNMissingCounts(t *testing.T) {
	for _, vniType := range []string{"L2", "L3"} {
		out := `{"100":{"vni":100,"type":"` + vniType + `","vxlanIf":"vxlan100","tenantVrf":"default"}}`
		expected := `# HELP frrk8s_evpn_vni_info EVPN VNI inventory; constant 1
# TYPE frrk8s_evpn_vni_info gauge
frrk8s_evpn_vni_info{interface="vxlan100",type="` + vniType + `",vni="100",vrf="default"} 1
` + scrapeSuccessText("evpn", 1)
		checkEVPN(t, out, expected)
	}
}

func checkEVPN(t *testing.T, out string, expected string) {
	t.Helper()
	c := &evpn{Log: log.NewNopLogger(), frrCli: func(command string) (string, error) {
		if command != "show evpn vni json" {
			t.Errorf("unexpected command %q", command)
		}
		return out, nil
	}}
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected)); err != nil {
		t.Fatal(err)
	}
}
