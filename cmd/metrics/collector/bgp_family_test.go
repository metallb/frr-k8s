// SPDX-License-Identifier:Apache-2.0

package collector

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCollectFamilyPrefixes(t *testing.T) {
	c := mocknewBGP(log.NewNopLogger())
	c.frrCli = func(command string) (string, error) {
		switch command {
		case "show bgp vrf all json":
			return `{"red":{}}`, nil
		case "show bgp vrf red neighbors json":
			return `{"net1":{"addressFamilyInfo":{
				"ipv4Unicast":{"sentPrefixCounter":0},
				"ipv4Vpn":{"sentPrefixCounter":5,"acceptedPrefixCounter":7},
				"l2VpnEvpn":{"acceptedPrefixCounter":11}
			}}}`, nil
		default:
			return "", fmt.Errorf("unexpected command %q", command)
		}
	}
	expected := `
# HELP frrk8s_bgp_family_announced_prefixes Number of prefixes currently being advertised on the BGP session for one address family
# TYPE frrk8s_bgp_family_announced_prefixes gauge
frrk8s_bgp_family_announced_prefixes{afi="ipv4",peer="net1",safi="unicast",vrf="red"} 0
# HELP frrk8s_bgp_family_received_prefixes Number of prefixes currently being received on the BGP session for one address family
# TYPE frrk8s_bgp_family_received_prefixes gauge
frrk8s_bgp_family_received_prefixes{afi="l2vpn",peer="net1",safi="evpn",vrf="red"} 11
# HELP frrk8s_bgp_announced_prefixes_total Number of prefixes currently being advertised on the BGP session
# TYPE frrk8s_bgp_announced_prefixes_total gauge
frrk8s_bgp_announced_prefixes_total{peer="net1",vrf="red"} 5
# HELP frrk8s_bgp_received_prefixes_total Number of prefixes currently being received on the BGP session
# TYPE frrk8s_bgp_received_prefixes_total gauge
frrk8s_bgp_received_prefixes_total{peer="net1",vrf="red"} 18
` + scrapeSuccessText("bgp", 1)
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"frrk8s_bgp_family_announced_prefixes", "frrk8s_bgp_family_received_prefixes",
		"frrk8s_bgp_announced_prefixes_total", "frrk8s_bgp_received_prefixes_total", "frrk8s_scrape_success"); err != nil {
		t.Fatal(err)
	}
}
