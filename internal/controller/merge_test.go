// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/metallb/frr-k8s/internal/community"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/metallb/frr-k8s/internal/ipfamily"
	"k8s.io/apimachinery/pkg/util/sets"
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
						ASN:      "65040",
						Addr:     "192.0.1.20",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24"},
							PrefixesV6: []string{"2001:db8::/64"},
							CommunityPrefixesModifiers: []frr.CommunityPrefixList{
								communityPrefixListFor("65040@192.0.1.20", "10:100", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "10:102", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "10:108", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "large:123:456:7890", "ipv6", []string{"2001:db8::/64"}),
							},
							LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
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
						ASN:      "65040",
						Addr:     "192.0.1.21",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24"},
							PrefixesV6: []string{"2001:db8::/64"},
							CommunityPrefixesModifiers: []frr.CommunityPrefixList{
								communityPrefixListFor("65040@192.0.1.21", "10:100", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.21", "10:102", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.21", "10:108", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.21", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.21", "10:108", "ipv6", []string{"2001:db8::/64"}),
							},
							LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
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
						ASN:      "65040",
						Addr:     "192.0.1.22",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24"},
							PrefixesV6: []string{"2001:db8::/64"},
							CommunityPrefixesModifiers: []frr.CommunityPrefixList{
								communityPrefixListFor("65040@192.0.1.22", "10:100", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.22", "10:102", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.22", "10:108", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.22", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.22", "10:108", "ipv6", []string{"2001:db8::/64"}),
							},
							LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
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
						ASN:      "65040",
						Addr:     "192.0.1.20",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
							PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
							CommunityPrefixesModifiers: []frr.CommunityPrefixList{
								communityPrefixListFor("65040@192.0.1.20", "20:200", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "20:200", "ipv6", []string{"2001:db8::/64"}),

								communityPrefixListFor("65040@192.0.1.20", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "large:123:456:7890", "ipv6", []string{"2001:db8::/64"}),
							},
							LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
								localPrefPrefixListFor("65040@192.0.1.20", 150, "ip", []string{"192.0.3.0/24"}),
								localPrefPrefixListFor("65040@192.0.1.20", 200, "ipv6", []string{"2001:db8::/64"}),
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
						ASN:      "65040",
						Addr:     "192.0.1.21",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
							PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
							CommunityPrefixesModifiers: []frr.CommunityPrefixList{
								communityPrefixListFor("65040@192.0.1.21", "20:200", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.21", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),

								communityPrefixListFor("65040@192.0.1.21", "20:200", "ipv6", []string{"2001:db8::/64"}),

								communityPrefixListFor("65040@192.0.1.21", "large:123:456:7890", "ipv6", []string{"2001:db8::/64"}),
							},
							LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
								localPrefPrefixListFor("65040@192.0.1.21", 150, "ip", []string{"192.0.3.0/24"}),
								localPrefPrefixListFor("65040@192.0.1.21", 200, "ipv6", []string{"2001:db8::/64"}),
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
						ASN:      "65040",
						Addr:     "192.0.1.23",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
							PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
							CommunityPrefixesModifiers: []frr.CommunityPrefixList{
								communityPrefixListFor("65040@192.0.1.23", "20:200", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.23", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),

								communityPrefixListFor("65040@192.0.1.23", "20:200", "ipv6", []string{"2001:db8::/64"}),

								communityPrefixListFor("65040@192.0.1.23", "large:123:456:7890", "ipv6", []string{"2001:db8::/64"}),
							},
							LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
								localPrefPrefixListFor("65040@192.0.1.23", 150, "ip", []string{"192.0.3.0/24"}),
								localPrefPrefixListFor("65040@192.0.1.23", 200, "ipv6", []string{"2001:db8::/64"}),
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
						ASN:      "65040",
						Addr:     "192.0.1.20",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
							PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
							CommunityPrefixesModifiers: []frr.CommunityPrefixList{
								communityPrefixListFor("65040@192.0.1.20", "10:100", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "10:102", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "10:108", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "20:200", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "20:200", "ipv6", []string{"2001:db8::/64"}),
								communityPrefixListFor("65040@192.0.1.20", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.20", "large:123:456:7890", "ipv6", []string{"2001:db8::/64"}),
							},
							LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
								localPrefPrefixListFor("65040@192.0.1.20", 150, "ip", []string{"192.0.3.0/24"}),
								localPrefPrefixListFor("65040@192.0.1.20", 200, "ipv6", []string{"2001:db8::/64"}),
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
						ASN:      "65040",
						Addr:     "192.0.1.21",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
							PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
							CommunityPrefixesModifiers: []frr.CommunityPrefixList{
								communityPrefixListFor("65040@192.0.1.21", "10:100", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.21", "10:102", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.21", "10:108", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.21", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.21", "10:108", "ipv6", []string{"2001:db8::/64"}),
								communityPrefixListFor("65040@192.0.1.21", "20:200", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.21", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.21", "20:200", "ipv6", []string{"2001:db8::/64"}),
								communityPrefixListFor("65040@192.0.1.21", "large:123:456:7890", "ipv6", []string{"2001:db8::/64"}),
							},
							LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
								localPrefPrefixListFor("65040@192.0.1.21", 150, "ip", []string{"192.0.3.0/24"}),
								localPrefPrefixListFor("65040@192.0.1.21", 200, "ipv6", []string{"2001:db8::/64"}),
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
						ASN:      "65040",
						Addr:     "192.0.1.22",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24"},
							PrefixesV6: []string{"2001:db8::/64"},
							CommunityPrefixesModifiers: []frr.CommunityPrefixList{
								communityPrefixListFor("65040@192.0.1.22", "10:100", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.22", "10:102", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.22", "10:108", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.22", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.22", "10:108", "ipv6", []string{"2001:db8::/64"}),
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
						ASN:      "65040",
						Addr:     "192.0.1.23",
						Outgoing: frr.AllowedOut{
							PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
							PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
							CommunityPrefixesModifiers: []frr.CommunityPrefixList{
								communityPrefixListFor("65040@192.0.1.23", "20:200", "ip", []string{"192.0.2.0/24"}),
								communityPrefixListFor("65040@192.0.1.23", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),

								communityPrefixListFor("65040@192.0.1.23", "20:200", "ipv6", []string{"2001:db8::/64"}),

								communityPrefixListFor("65040@192.0.1.23", "large:123:456:7890", "ipv6", []string{"2001:db8::/64"}),
							},
							LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
								localPrefPrefixListFor("65040@192.0.1.23", 150, "ip", []string{"192.0.3.0/24"}),
								localPrefPrefixListFor("65040@192.0.1.23", 200, "ipv6", []string{"2001:db8::/64"}),
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
			if diff := cmp.Diff(merged,
				test.expected,
				cmpopts.EquateEmpty(),
				cmp.Comparer(communityComparer),
				cmpopts.SortSlices(communityPrefixListSorter),
				cmpopts.SortSlices(localPrefPrefixListSorter),
			); diff != "" {
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
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24"},
						PrefixesV6: []string{"2001:db8::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.20", "10:100", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "10:102", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "10:108", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "10:108", "ipv6", []string{"2001:db8::/64"}),
						},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
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
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						PrefixesV6: []string{"2001:db8::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.20", "20:200", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "20:200", "ipv6", []string{"2001:db8::/64"}),

							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7892", "ipv6", []string{"2001:db8::/64"}),
						},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
							localPrefPrefixListFor("65040@192.0.1.23", 150, "ip", []string{"192.0.3.0/24"}),
							localPrefPrefixListFor("65040@192.0.1.23", 200, "ipv6", []string{"2001:db8::/64"}),
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
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						PrefixesV6: []string{"2001:db8::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.20", "10:100", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "10:102", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "10:108", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "10:108", "ipv6", []string{"2001:db8::/64"}),
							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7892", "ipv6", []string{"2001:db8::/64"}),
							communityPrefixListFor("65040@192.0.1.20", "20:200", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "20:200", "ipv6", []string{"2001:db8::/64"}),
						},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
							localPrefPrefixListFor("65040@192.0.1.23", 150, "ip", []string{"192.0.3.0/24"}),
							localPrefPrefixListFor("65040@192.0.1.23", 200, "ipv6", []string{"2001:db8::/64"}),
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
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24"},
						PrefixesV6: []string{"2001:db8::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.20", "10:100", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "10:102", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "10:108", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "10:108", "ipv6", []string{"2001:db8::/64"}),
						},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
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
					ASN:      "65040",
					Addr:     "192.0.1.21",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24"},
						PrefixesV6: []string{"2001:db8::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.21", "10:100", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "10:102", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "10:108", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "10:108", "ipv6", []string{"2001:db8::/64"}),
						},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
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
					ASN:      "65040",
					Addr:     "192.0.1.22",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24"},
						PrefixesV6: []string{"2001:db8::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.22", "10:100", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.22", "10:102", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.22", "10:108", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.22", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.22", "10:108", "ipv6", []string{"2001:db8::/64"}),
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
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.20", "20:200", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "20:200", "ipv6", []string{"2001:db8::/64"}),

							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7892", "ipv6", []string{"2001:db8::/64"}),
						},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
							localPrefPrefixListFor("65040@192.0.1.20", 150, "ip", []string{"192.0.3.0/24"}),
							localPrefPrefixListFor("65040@192.0.1.20", 200, "ipv6", []string{"2001:db8::/64"}),
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
					ASN:      "65040",
					Addr:     "192.0.1.21",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.21", "20:200", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "20:200", "ipv6", []string{"2001:db8::/64"}),
							communityPrefixListFor("65040@192.0.1.21", "large:123:456:7890", "ipv6", []string{"2001:db8::/64"}),
						},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
							localPrefPrefixListFor("65040@192.0.1.21", 150, "ip", []string{"192.0.3.0/24"}),
							localPrefPrefixListFor("65040@192.0.1.21", 200, "ipv6", []string{"2001:db8::/64"}),
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
					ASN:      "65040",
					Addr:     "192.0.1.23",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.23", "20:200", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.23", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),

							communityPrefixListFor("65040@192.0.1.23", "20:200", "ipv6", []string{"2001:db8::/64"}),

							communityPrefixListFor("65040@192.0.1.23", "large:123:456:7890", "ipv6", []string{"2001:db8::/64"}),
						},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
							localPrefPrefixListFor("65040@192.0.1.23", 150, "ip", []string{"192.0.3.0/24"}),
							localPrefPrefixListFor("65040@192.0.1.23", 200, "ipv6", []string{"2001:db8::/64"}),
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
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.20", "10:100", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "10:102", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "10:108", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "20:200", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "20:200", "ipv6", []string{"2001:db8::/64"}),
							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.20", "large:123:456:7892", "ipv6", []string{"2001:db8::/64"}),
							communityPrefixListFor("65040@192.0.1.20", "10:108", "ipv6", []string{"2001:db8::/64"}),
						},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
							localPrefPrefixListFor("65040@192.0.1.20", 150, "ip", []string{"192.0.3.0/24"}),
							localPrefPrefixListFor("65040@192.0.1.20", 200, "ipv6", []string{"2001:db8::/64"}),
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
					ASN:      "65040",
					Addr:     "192.0.1.21",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.21", "10:100", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "10:102", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "10:108", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "10:108", "ipv6", []string{"2001:db8::/64"}),
							communityPrefixListFor("65040@192.0.1.21", "20:200", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.21", "20:200", "ipv6", []string{"2001:db8::/64"}),
							communityPrefixListFor("65040@192.0.1.21", "large:123:456:7890", "ipv6", []string{"2001:db8::/64"}),
						},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
							localPrefPrefixListFor("65040@192.0.1.21", 150, "ip", []string{"192.0.3.0/24"}),
							localPrefPrefixListFor("65040@192.0.1.21", 200, "ipv6", []string{"2001:db8::/64"}),
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
					ASN:      "65040",
					Addr:     "192.0.1.22",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24"},
						PrefixesV6: []string{"2001:db8::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.22", "10:100", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.22", "10:102", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.22", "10:108", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.22", "large:123:456:7890", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.22", "10:108", "ipv6", []string{"2001:db8::/64"}),
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
					ASN:      "65040",
					Addr:     "192.0.1.23",
					Outgoing: frr.AllowedOut{
						PrefixesV4: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						PrefixesV6: []string{"2001:db8::/64", "2001:db9::/64"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{
							communityPrefixListFor("65040@192.0.1.23", "20:200", "ip", []string{"192.0.2.0/24"}),
							communityPrefixListFor("65040@192.0.1.23", "large:123:456:7892", "ip", []string{"192.0.2.0/24"}),

							communityPrefixListFor("65040@192.0.1.23", "20:200", "ipv6", []string{"2001:db8::/64"}),

							communityPrefixListFor("65040@192.0.1.23", "large:123:456:7890", "ipv6", []string{"2001:db8::/64"}),
						},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
							localPrefPrefixListFor("65040@192.0.1.23", 150, "ip", []string{"192.0.3.0/24"}),
							localPrefPrefixListFor("65040@192.0.1.23", 200, "ipv6", []string{"2001:db8::/64"}),
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
		}, {
			name: "Incoming: first config has All, the other specific",
			curr: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Incoming: frr.AllowedIn{
						All:        true,
						PrefixesV4: []frr.IncomingFilter{},
						PrefixesV6: []frr.IncomingFilter{},
					},
					Outgoing: frr.AllowedOut{
						PrefixesV4:                 []string{},
						PrefixesV6:                 []string{},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
					},
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      "65040",
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
					Outgoing: frr.AllowedOut{
						PrefixesV4:                 []string{},
						PrefixesV6:                 []string{},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
					},
				},
			},
			expected: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4:                 []string{},
						PrefixesV6:                 []string{},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
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
					ASN:      "65040",
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
					Outgoing: frr.AllowedOut{
						PrefixesV4:                 []string{},
						PrefixesV6:                 []string{},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
					},
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Incoming: frr.AllowedIn{
						All:        true,
						PrefixesV4: []frr.IncomingFilter{},
						PrefixesV6: []frr.IncomingFilter{},
					},
					Outgoing: frr.AllowedOut{
						PrefixesV4:                 []string{},
						PrefixesV6:                 []string{},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
					},
				},
			},
			expected: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4:                 []string{},
						PrefixesV6:                 []string{},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
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
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4:                 []string{"192.0.2.0/24"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
							localPrefPrefixListFor("65040@192.0.1.20", 100, "ip", []string{"192.0.2.0/24"}),
						},
					},
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4:                 []string{"192.0.2.0/24"},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{
							localPrefPrefixListFor("65040@192.0.1.20", 150, "ip", []string{"192.0.2.0/24"}),
						},
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
					ASN:      "65040",
					Addr:     "192.0.1.20",
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily:      ipfamily.IPv4,
					Name:          "65040@192.0.1.20",
					ASN:           "65040",
					Addr:          "192.0.1.20",
					HoldTime:      ptr.To(int64(180)),
					KeepaliveTime: ptr.To(int64(60)),
					ConnectTime:   ptr.To(int64(60)),
				},
			},
			expected: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      "65040",
					Addr:     "192.0.1.20",
					Outgoing: frr.AllowedOut{
						PrefixesV4:                 []string{},
						PrefixesV6:                 []string{},
						CommunityPrefixesModifiers: []frr.CommunityPrefixList{},
						LocalPrefPrefixesModifiers: []frr.LocalPrefPrefixList{},
					},
					Incoming: frr.AllowedIn{
						All:        false,
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
					ASN:           "65040",
					Addr:          "192.0.1.20",
					HoldTime:      ptr.To(int64(180)),
					KeepaliveTime: ptr.To(int64(60)),
					ConnectTime:   ptr.To(int64(60)),
				},
			},
			toMerge: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      "65040",
					Addr:     "192.0.1.20",
				},
			},
			expected: []*frr.NeighborConfig{
				{
					IPFamily: ipfamily.IPv4,
					Name:     "65040@192.0.1.20",
					ASN:      "65040",
					Addr:     "192.0.1.20",
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
			if diff := cmp.Diff(merged, test.expected,
				cmpopts.EquateEmpty(), cmp.Comparer(communityComparer),
				cmpopts.SortSlices(communityPrefixListSorter),
				cmpopts.SortSlices(localPrefPrefixListSorter),
			); diff != "" {
				t.Fatalf("config different from expected: %s", diff)
			}
		})
	}
}

func communityPrefixListFor(neigID, comm string, ipFamily string, prefixes []string) frr.CommunityPrefixList {
	community, err := community.New(comm)
	if err != nil {
		panic(err)
	}
	return frr.CommunityPrefixList{
		PrefixList: frr.PrefixList{
			Name:     communityPrefixListName(neigID, community, ipFamily),
			Prefixes: sets.New(prefixes...),
			IPFamily: ipFamily,
		},
		Community: community,
	}
}

func localPrefPrefixListFor(neigID string, localPref int, ipFamily string, prefixes []string) frr.LocalPrefPrefixList {
	return frr.LocalPrefPrefixList{
		PrefixList: frr.PrefixList{
			Name:     localPrefPrefixListName(neigID, uint32(localPref), ipFamily),
			Prefixes: sets.New(prefixes...),
			IPFamily: ipFamily,
		},
		LocalPref: uint32(localPref),
	}
}

func communityComparer(a, b community.BGPCommunity) bool {
	if a != nil && b != nil {
		return a.String() == b.String()
	}
	return false
}

func communityPrefixListSorter(a, b frr.CommunityPrefixList) bool {
	if a.Name == "" || b.Name == "" {
		panic("empty name")
	}

	if communityPrefixListKey(a.Community, a.IPFamily) < communityPrefixListKey(b.Community, b.IPFamily) {
		return false
	}
	return true
}

func localPrefPrefixListSorter(a, b frr.LocalPrefPrefixList) bool {
	if a.Name == "" || b.Name == "" {
		panic("empty name")
	}

	if localPrefPrefixListKey(a.LocalPref, a.IPFamily) < localPrefPrefixListKey(b.LocalPref, b.IPFamily) {
		return false
	}
	return true
}
