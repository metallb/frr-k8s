// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/metallb/frr-k8s/internal/ipfamily"
	"k8s.io/utils/ptr"
)

func TestMergeRouters(t *testing.T) {
	tests := []struct {
		name     string
		curr     *frr.RouterConfig
		toMerge  *frr.RouterConfig
		expected *frr.RouterConfig
		err      error
	}{
		{
			name: "Full - Multiple neigbors",
			curr: &frr.RouterConfig{
				MyASN:    65001,
				RouterID: "192.0.2.1",
				VRF:      "",
				Neighbors: []*frr.NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						Name:     "65040@192.0.1.20",
						ASN:      65040,
						Addr:     "192.0.1.20",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv4,
									Prefix:           "192.0.2.0/24",
									Communities:      []string{"10:100", "10:102", "10:108"},
									LargeCommunities: []string{"large:123:456:7890"},
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.0.3.0/24",
								},
							},
							PrefixesV6: []frr.OutgoingFilter{
								{
									IPFamily:    ipfamily.IPv6,
									Prefix:      "2001:db8::/64",
									Communities: []string{"10:108"},
								},
							},
						},
						Incoming: frr.AllowedIn{
							All: false,
							PrefixesV4: []frr.IncomingFilter{
								{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
							},
							PrefixesV6: []frr.IncomingFilter{
								{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
							},
						},
					},
					{
						IPFamily: ipfamily.IPv4,
						Name:     "65040@192.0.1.21",
						ASN:      65040,
						Addr:     "192.0.1.21",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv4,
									Prefix:           "192.0.2.0/24",
									Communities:      []string{"10:100", "10:102", "10:108"},
									LargeCommunities: []string{"large:123:456:7890"},
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.0.3.0/24",
								},
							},
							PrefixesV6: []frr.OutgoingFilter{
								{
									IPFamily:    ipfamily.IPv6,
									Prefix:      "2001:db8::/64",
									Communities: []string{"10:108"},
								},
							},
						},
						Incoming: frr.AllowedIn{
							All: false,
							PrefixesV4: []frr.IncomingFilter{
								{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
							},
							PrefixesV6: []frr.IncomingFilter{
								{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
							},
						},
					},
					{
						IPFamily: ipfamily.IPv4,
						Name:     "65040@192.0.1.22",
						ASN:      65040,
						Addr:     "192.0.1.22",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv4,
									Prefix:           "192.0.2.0/24",
									Communities:      []string{"10:100", "10:102", "10:108"},
									LargeCommunities: []string{"large:123:456:7890"},
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.0.3.0/24",
								},
							},
							PrefixesV6: []frr.OutgoingFilter{
								{
									IPFamily:    ipfamily.IPv6,
									Prefix:      "2001:db8::/64",
									Communities: []string{"10:108"},
								},
							},
						},
						Incoming: frr.AllowedIn{
							All: false,
							PrefixesV4: []frr.IncomingFilter{
								{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
							},
							PrefixesV6: []frr.IncomingFilter{
								{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
							},
						},
					},
				},
				IPV4Prefixes: []string{"192.0.2.0/24"},
				IPV6Prefixes: []string{"2001:db8::/64"},
			},
			toMerge: &frr.RouterConfig{
				MyASN:        65001,
				RouterID:     "192.0.2.1",
				VRF:          "",
				IPV4Prefixes: []string{"192.0.3.0/24", "192.0.4.0/24"},
				IPV6Prefixes: []string{"2001:db9::/64"},
				Neighbors: []*frr.NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						Name:     "65040@192.0.1.20",
						ASN:      65040,
						Addr:     "192.0.1.20",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv4,
									Prefix:           "192.0.2.0/24",
									Communities:      []string{"20:200"},
									LargeCommunities: []string{"large:123:456:7892"},
								},
								{
									IPFamily:  ipfamily.IPv4,
									Prefix:    "192.0.3.0/24",
									LocalPref: 150,
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.0.4.0/24",
								},
							},
							PrefixesV6: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv6,
									Prefix:           "2001:db8::/64",
									Communities:      []string{"20:200"},
									LargeCommunities: []string{"large:123:456:7890"},
									LocalPref:        200,
								},
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "2001:db9::/64",
								},
							},
						},
						Incoming: frr.AllowedIn{
							All: false,
							PrefixesV4: []frr.IncomingFilter{
								{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
							},
							PrefixesV6: []frr.IncomingFilter{
								{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
							},
						},
					},
					{
						IPFamily: ipfamily.IPv4,
						Name:     "65040@192.0.1.21",
						ASN:      65040,
						Addr:     "192.0.1.21",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv4,
									Prefix:           "192.0.2.0/24",
									Communities:      []string{"20:200"},
									LargeCommunities: []string{"large:123:456:7892"},
								},
								{
									IPFamily:  ipfamily.IPv4,
									Prefix:    "192.0.3.0/24",
									LocalPref: 150,
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.0.4.0/24",
								},
							},
							PrefixesV6: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv6,
									Prefix:           "2001:db8::/64",
									Communities:      []string{"20:200"},
									LargeCommunities: []string{"large:123:456:7890"},
									LocalPref:        200,
								},
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "2001:db9::/64",
								},
							},
						},
						Incoming: frr.AllowedIn{
							All: false,
							PrefixesV4: []frr.IncomingFilter{
								{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
							},
							PrefixesV6: []frr.IncomingFilter{
								{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
							},
						},
					},
					{
						IPFamily: ipfamily.IPv4,
						Name:     "65040@192.0.1.23",
						ASN:      65040,
						Addr:     "192.0.1.23",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv4,
									Prefix:           "192.0.2.0/24",
									Communities:      []string{"20:200"},
									LargeCommunities: []string{"large:123:456:7892"},
								},
								{
									IPFamily:  ipfamily.IPv4,
									Prefix:    "192.0.3.0/24",
									LocalPref: 150,
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.0.4.0/24",
								},
							},
							PrefixesV6: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv6,
									Prefix:           "2001:db8::/64",
									Communities:      []string{"20:200"},
									LargeCommunities: []string{"large:123:456:7890"},
									LocalPref:        200,
								},
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "2001:db9::/64",
								},
							},
						},
						Incoming: frr.AllowedIn{
							All: false,
							PrefixesV4: []frr.IncomingFilter{
								{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
							},
							PrefixesV6: []frr.IncomingFilter{
								{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
							},
						},
					},
				},
			},
			expected: &frr.RouterConfig{
				MyASN:        65001,
				RouterID:     "192.0.2.1",
				VRF:          "",
				IPV4Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
				IPV6Prefixes: []string{"2001:db8::/64", "2001:db9::/64"},
				Neighbors: []*frr.NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						Name:     "65040@192.0.1.20",
						ASN:      65040,
						Addr:     "192.0.1.20",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv4,
									Prefix:           "192.0.2.0/24",
									Communities:      []string{"10:100", "10:102", "10:108", "20:200"},
									LargeCommunities: []string{"large:123:456:7890", "large:123:456:7892"},
								},
								{
									IPFamily:  ipfamily.IPv4,
									Prefix:    "192.0.3.0/24",
									LocalPref: 150,
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.0.4.0/24",
								},
							},
							PrefixesV6: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv6,
									Prefix:           "2001:db8::/64",
									Communities:      []string{"10:108", "20:200"},
									LargeCommunities: []string{"large:123:456:7890"},
									LocalPref:        200,
								},
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "2001:db9::/64",
								},
							},
						},
						Incoming: frr.AllowedIn{
							All: false,
							PrefixesV4: []frr.IncomingFilter{
								{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
							},
							PrefixesV6: []frr.IncomingFilter{
								{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
								{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
							},
						},
					},
					{
						IPFamily: ipfamily.IPv4,
						Name:     "65040@192.0.1.21",
						ASN:      65040,
						Addr:     "192.0.1.21",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv4,
									Prefix:           "192.0.2.0/24",
									Communities:      []string{"10:100", "10:102", "10:108", "20:200"},
									LargeCommunities: []string{"large:123:456:7890", "large:123:456:7892"},
								},
								{
									IPFamily:  ipfamily.IPv4,
									Prefix:    "192.0.3.0/24",
									LocalPref: 150,
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.0.4.0/24",
								},
							},
							PrefixesV6: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv6,
									Prefix:           "2001:db8::/64",
									Communities:      []string{"10:108", "20:200"},
									LargeCommunities: []string{"large:123:456:7890"},
									LocalPref:        200,
								},
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "2001:db9::/64",
								},
							},
						},
						Incoming: frr.AllowedIn{
							All: false,
							PrefixesV4: []frr.IncomingFilter{
								{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
							},
							PrefixesV6: []frr.IncomingFilter{
								{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
								{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
							},
						},
					},
					{
						IPFamily: ipfamily.IPv4,
						Name:     "65040@192.0.1.22",
						ASN:      65040,
						Addr:     "192.0.1.22",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv4,
									Prefix:           "192.0.2.0/24",
									Communities:      []string{"10:100", "10:102", "10:108"},
									LargeCommunities: []string{"large:123:456:7890"},
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.0.3.0/24",
								},
							},
							PrefixesV6: []frr.OutgoingFilter{
								{
									IPFamily:    ipfamily.IPv6,
									Prefix:      "2001:db8::/64",
									Communities: []string{"10:108"},
								},
							},
						},
						Incoming: frr.AllowedIn{
							All: false,
							PrefixesV4: []frr.IncomingFilter{
								{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
							},
							PrefixesV6: []frr.IncomingFilter{
								{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
							},
						},
					},
					{
						IPFamily: ipfamily.IPv4,
						Name:     "65040@192.0.1.23",
						ASN:      65040,
						Addr:     "192.0.1.23",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv4,
									Prefix:           "192.0.2.0/24",
									Communities:      []string{"20:200"},
									LargeCommunities: []string{"large:123:456:7892"},
								},
								{
									IPFamily:  ipfamily.IPv4,
									Prefix:    "192.0.3.0/24",
									LocalPref: 150,
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.0.4.0/24",
								},
							},
							PrefixesV6: []frr.OutgoingFilter{
								{
									IPFamily:         ipfamily.IPv6,
									Prefix:           "2001:db8::/64",
									Communities:      []string{"20:200"},
									LargeCommunities: []string{"large:123:456:7890"},
									LocalPref:        200,
								},
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "2001:db9::/64",
								},
							},
						},
						Incoming: frr.AllowedIn{
							All: false,
							PrefixesV4: []frr.IncomingFilter{
								{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
								{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
							},
							PrefixesV6: []frr.IncomingFilter{
								{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
							},
						},
					},
				},
			},
			err: nil,
		},
		{
			name: "Same VRF, different ASN",
			curr: &frr.RouterConfig{
				MyASN:        65001,
				RouterID:     "192.0.2.1",
				VRF:          "",
				IPV4Prefixes: []string{},
				IPV6Prefixes: []string{},
			},
			toMerge: &frr.RouterConfig{
				MyASN:        65002,
				RouterID:     "192.0.2.1",
				VRF:          "",
				IPV4Prefixes: []string{},
				IPV6Prefixes: []string{},
			},
			err: fmt.Errorf("different asns (%d != %d) specified for same vrf: %s", 65001, 65002, ""),
		},
		{
			name: "Same VRF+ASN, different RouterIDs",
			curr: &frr.RouterConfig{
				MyASN:        65001,
				RouterID:     "192.0.2.1",
				VRF:          "",
				IPV4Prefixes: []string{},
				IPV6Prefixes: []string{},
			},
			toMerge: &frr.RouterConfig{
				MyASN:        65001,
				RouterID:     "192.0.2.20",
				VRF:          "",
				IPV4Prefixes: []string{},
				IPV6Prefixes: []string{},
			},
			err: fmt.Errorf("different router ids (%s != %s) specified for same vrf: %s", "192.0.2.1", "192.0.2.20", ""),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			merged, err := mergeRouterConfigs(test.curr, test.toMerge)
			if test.err != nil && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if test.err == nil && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if diff := cmp.Diff(merged, test.expected); diff != "" {
				t.Fatalf("config different from expected: %s", diff)
			}
		})
	}
}

func TestMergeNeighbors(t *testing.T) {
	tests := []struct {
		name     string
		curr     []*frr.NeighborConfig
		toMerge  []*frr.NeighborConfig
		expected []*frr.NeighborConfig
		err      error
	}{
		{
			name: "One neighbor, multiple configs",
			curr: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"10:100", "10:102", "10:108"},
								LargeCommunities: []string{"large:123:456:7890"},
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.3.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:    ipfamily.IPv6,
								Prefix:      "2001:db8::/64",
								Communities: []string{"10:108"},
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
						},
					},
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"20:200"},
								LargeCommunities: []string{"large:123:456:7892"},
							},
							{
								IPFamily:  ipfamily.IPv4,
								Prefix:    "192.0.3.0/24",
								LocalPref: 150,
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.4.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv6,
								Prefix:           "2001:db8::/64",
								Communities:      []string{"20:200"},
								LargeCommunities: []string{"large:123:456:7890"},
								LocalPref:        200,
							},
							{
								IPFamily: ipfamily.IPv6,
								Prefix:   "2001:db9::/64",
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
						},
					},
				},
			},
			expected: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"10:100", "10:102", "10:108", "20:200"},
								LargeCommunities: []string{"large:123:456:7890", "large:123:456:7892"},
							},
							{
								IPFamily:  ipfamily.IPv4,
								Prefix:    "192.0.3.0/24",
								LocalPref: 150,
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.4.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv6,
								Prefix:           "2001:db8::/64",
								Communities:      []string{"10:108", "20:200"},
								LargeCommunities: []string{"large:123:456:7890"},
								LocalPref:        200,
							},
							{
								IPFamily: ipfamily.IPv6,
								Prefix:   "2001:db9::/64",
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
							{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
						},
					},
				},
			},
			err: nil,
		},
		{
			name: "Multiple neighbors, multiple configs",
			curr: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"10:100", "10:102", "10:108"},
								LargeCommunities: []string{"large:123:456:7890"},
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.3.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:    ipfamily.IPv6,
								Prefix:      "2001:db8::/64",
								Communities: []string{"10:108"},
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
						},
					},
				},
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.21",
					ASN:      65040,
					Addr:     "192.0.1.21",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"10:100", "10:102", "10:108"},
								LargeCommunities: []string{"large:123:456:7890"},
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.3.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:    ipfamily.IPv6,
								Prefix:      "2001:db8::/64",
								Communities: []string{"10:108"},
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
						},
					},
				},
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.22",
					ASN:      65040,
					Addr:     "192.0.1.22",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"10:100", "10:102", "10:108"},
								LargeCommunities: []string{"large:123:456:7890"},
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.3.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:    ipfamily.IPv6,
								Prefix:      "2001:db8::/64",
								Communities: []string{"10:108"},
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
						},
					},
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"20:200"},
								LargeCommunities: []string{"large:123:456:7892"},
							},
							{
								IPFamily:  ipfamily.IPv4,
								Prefix:    "192.0.3.0/24",
								LocalPref: 150,
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.4.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv6,
								Prefix:           "2001:db8::/64",
								Communities:      []string{"20:200"},
								LargeCommunities: []string{"large:123:456:7890"},
								LocalPref:        200,
							},
							{
								IPFamily: ipfamily.IPv6,
								Prefix:   "2001:db9::/64",
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
						},
					},
				},
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.21",
					ASN:      65040,
					Addr:     "192.0.1.21",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"20:200"},
								LargeCommunities: []string{"large:123:456:7892"},
							},
							{
								IPFamily:  ipfamily.IPv4,
								Prefix:    "192.0.3.0/24",
								LocalPref: 150,
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.4.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv6,
								Prefix:           "2001:db8::/64",
								Communities:      []string{"20:200"},
								LargeCommunities: []string{"large:123:456:7890"},
								LocalPref:        200,
							},
							{
								IPFamily: ipfamily.IPv6,
								Prefix:   "2001:db9::/64",
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
						},
					},
				},
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.23",
					ASN:      65040,
					Addr:     "192.0.1.23",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"20:200"},
								LargeCommunities: []string{"large:123:456:7892"},
							},
							{
								IPFamily:  ipfamily.IPv4,
								Prefix:    "192.0.3.0/24",
								LocalPref: 150,
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.4.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv6,
								Prefix:           "2001:db8::/64",
								Communities:      []string{"20:200"},
								LargeCommunities: []string{"large:123:456:7890"},
								LocalPref:        200,
							},
							{
								IPFamily: ipfamily.IPv6,
								Prefix:   "2001:db9::/64",
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
						},
					},
				},
			},
			expected: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"10:100", "10:102", "10:108", "20:200"},
								LargeCommunities: []string{"large:123:456:7890", "large:123:456:7892"},
							},
							{
								IPFamily:  ipfamily.IPv4,
								Prefix:    "192.0.3.0/24",
								LocalPref: 150,
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.4.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv6,
								Prefix:           "2001:db8::/64",
								Communities:      []string{"10:108", "20:200"},
								LargeCommunities: []string{"large:123:456:7890"},
								LocalPref:        200,
							},
							{
								IPFamily: ipfamily.IPv6,
								Prefix:   "2001:db9::/64",
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
							{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
						},
					},
				},
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.21",
					ASN:      65040,
					Addr:     "192.0.1.21",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"10:100", "10:102", "10:108", "20:200"},
								LargeCommunities: []string{"large:123:456:7890", "large:123:456:7892"},
							},
							{
								IPFamily:  ipfamily.IPv4,
								Prefix:    "192.0.3.0/24",
								LocalPref: 150,
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.4.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv6,
								Prefix:           "2001:db8::/64",
								Communities:      []string{"10:108", "20:200"},
								LargeCommunities: []string{"large:123:456:7890"},
								LocalPref:        200,
							},
							{
								IPFamily: ipfamily.IPv6,
								Prefix:   "2001:db9::/64",
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
							{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
						},
					},
				},
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.22",
					ASN:      65040,
					Addr:     "192.0.1.22",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"10:100", "10:102", "10:108"},
								LargeCommunities: []string{"large:123:456:7890"},
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.3.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:    ipfamily.IPv6,
								Prefix:      "2001:db8::/64",
								Communities: []string{"10:108"},
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.11.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee8::/64"},
						},
					},
				},
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.23",
					ASN:      65040,
					Addr:     "192.0.1.23",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv4,
								Prefix:           "192.0.2.0/24",
								Communities:      []string{"20:200"},
								LargeCommunities: []string{"large:123:456:7892"},
							},
							{
								IPFamily:  ipfamily.IPv4,
								Prefix:    "192.0.3.0/24",
								LocalPref: 150,
							},
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.0.4.0/24",
							},
						},
						PrefixesV6: []frr.OutgoingFilter{
							{
								IPFamily:         ipfamily.IPv6,
								Prefix:           "2001:db8::/64",
								Communities:      []string{"20:200"},
								LargeCommunities: []string{"large:123:456:7890"},
								LocalPref:        200,
							},
							{
								IPFamily: ipfamily.IPv6,
								Prefix:   "2001:db9::/64",
							},
						},
					},
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
						},
					},
				},
			},
			err: nil,
		},
		{
			name: "Incoming: first config has All, the other specific",
			curr: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Incoming: frr.AllowedIn{
						All:        true,
						PrefixesV4: []frr.IncomingFilter{},
						PrefixesV6: []frr.IncomingFilter{},
					},
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
						},
					},
				},
			},
			expected: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{},
						PrefixesV6: []frr.OutgoingFilter{},
					},
					Incoming: frr.AllowedIn{
						All:        true,
						PrefixesV4: []frr.IncomingFilter{},
						PrefixesV6: []frr.IncomingFilter{},
					},
				},
			},
			err: nil,
		},
		{
			name: "Incoming: first config specific, the other All",
			curr: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Incoming: frr.AllowedIn{
						All: false,
						PrefixesV4: []frr.IncomingFilter{
							{IPFamily: "ipv4", Prefix: "192.0.10.0/24"},
							{IPFamily: "ipv4", Prefix: "192.0.12.0/24"},
						},
						PrefixesV6: []frr.IncomingFilter{
							{IPFamily: "ipv6", Prefix: "2001:ee9::/64"},
						},
					},
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Incoming: frr.AllowedIn{
						All:        true,
						PrefixesV4: []frr.IncomingFilter{},
						PrefixesV6: []frr.IncomingFilter{},
					},
				},
			},
			expected: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{},
						PrefixesV6: []frr.OutgoingFilter{},
					},
					Incoming: frr.AllowedIn{
						All:        true,
						PrefixesV4: []frr.IncomingFilter{},
						PrefixesV6: []frr.IncomingFilter{},
					},
				},
			},
			err: nil,
		},
		{
			name: "Multiple localPrefs for a prefix",
			curr: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:  ipfamily.IPv4,
								Prefix:    "192.0.2.0/24",
								LocalPref: 100,
							},
						},
						PrefixesV6: []frr.OutgoingFilter{},
					},
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{
							{
								IPFamily:  ipfamily.IPv4,
								Prefix:    "192.0.2.0/24",
								LocalPref: 150,
							},
						},
						PrefixesV6: []frr.OutgoingFilter{},
					},
				},
			},
			err: fmt.Errorf("multiple local prefs specified for prefix %s", "192.0.2.0/24"),
		},
		{
			name: "HoldTime / KeepAlive time, one nil, the other specifies the default",
			curr: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily:      ipfamily.IPv4,
					Name:          "65040@192.0.1.20",
					ASN:           65040,
					Addr:          "192.0.1.20",
					HoldTime:      ptr.To(uint64(180)),
					KeepaliveTime: ptr.To(uint64(60)),
					ConnectTime:   ptr.To(uint64(60)),
				},
			},
			expected: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{},
						PrefixesV6: []frr.OutgoingFilter{},
					},
					Incoming: frr.AllowedIn{
						PrefixesV4: []frr.IncomingFilter{},
						PrefixesV6: []frr.IncomingFilter{},
					},
				},
			},
			err: nil,
		},
		{
			// This test is similar to the previous, to ensure conversion stability
			name: "HoldTime / KeepAlive time, one set to the default, the other set to nil",
			curr: []*frr.NeighborConfig{
				{
					IPFamily:      ipfamily.IPv4,
					Name:          "65040@192.0.1.20",
					ASN:           65040,
					Addr:          "192.0.1.20",
					HoldTime:      ptr.To(uint64(180)),
					KeepaliveTime: ptr.To(uint64(60)),
					ConnectTime:   ptr.To(uint64(60)),
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
				},
			},
			expected: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      65040,
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []frr.OutgoingFilter{},
						PrefixesV6: []frr.OutgoingFilter{},
					},
					Incoming: frr.AllowedIn{
						PrefixesV4: []frr.IncomingFilter{},
						PrefixesV6: []frr.IncomingFilter{},
					},
				},
			},
			err: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			merged, err := mergeNeighbors(test.curr, test.toMerge)
			if test.err != nil && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if test.err == nil && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if diff := cmp.Diff(merged, test.expected); diff != "" {
				t.Fatalf("config different from expected: %s", diff)
			}
		})
	}
}
