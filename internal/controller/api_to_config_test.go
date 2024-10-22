// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	v1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/metallb/frr-k8s/internal/ipfamily"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestConversion(t *testing.T) {
	_, ipv4CIDR, _ := net.ParseCIDR("192.168.1.0/24")
	_, ipv6CIDR, _ := net.ParseCIDR("fc00:f853:ccd:e800::/64")

	tests := []struct {
		name        string
		fromK8s     []v1beta1.FRRConfiguration
		secrets     map[string]v1.Secret
		alwaysBlock []net.IPNet
		expected    *frr.Config
		err         error
	}{
		{
			name: "Single Router and Neighbor with SrcAddr",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65001,
									ID:  "192.0.2.1",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:           65002,
											Port:          ptr.To[uint16](179),
											SourceAddress: "192.1.1.1",
											Address:       "192.0.2.2",
											KeepaliveTime: &metav1.Duration{
												Duration: 20 * time.Second,
											},
											HoldTime: &metav1.Duration{
												Duration: 40 * time.Second,
											},
											ConnectTime: &metav1.Duration{
												Duration: 2 * time.Second,
											},
											DisableMP:             true,
											EnableGracefulRestart: true,
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.2.0/24"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65001,
						RouterID: "192.0.2.1",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily:        ipfamily.IPv4,
								Name:            "65002@192.0.2.2",
								ASN:             "65002",
								Port:            ptr.To[uint16](179),
								SrcAddr:         "192.1.1.1",
								Addr:            "192.0.2.2",
								KeepaliveTime:   ptr.To[int64](20),
								HoldTime:        ptr.To[int64](40),
								ConnectTime:     ptr.To(int64(2)),
								DisableMP:       true,
								GracefulRestart: true,
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24"},
					},
				},
			},
			err: nil,
		},
		{
			name: "Single Router and Neighbor",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65001,
									ID:  "192.0.2.1",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65002,
											Port:    ptr.To[uint16](179),
											Address: "192.0.2.2",
											KeepaliveTime: &metav1.Duration{
												Duration: 20 * time.Second,
											},
											HoldTime: &metav1.Duration{
												Duration: 40 * time.Second,
											},
											ConnectTime: &metav1.Duration{
												Duration: 2 * time.Second,
											},
											DisableMP: true,
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.2.0/24"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65001,
						RouterID: "192.0.2.1",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily:      ipfamily.IPv4,
								Name:          "65002@192.0.2.2",
								ASN:           "65002",
								Port:          ptr.To[uint16](179),
								Addr:          "192.0.2.2",
								KeepaliveTime: ptr.To[int64](20),
								HoldTime:      ptr.To[int64](40),
								ConnectTime:   ptr.To(int64(2)),
								DisableMP:     true,
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24"},
					},
				},
			},
			err: nil,
		},
		{
			name: "Multiple Routers and Neighbors",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65011,
											Address: "192.0.2.6",
										},
										{
											ASN:     65012,
											Address: "192.0.2.7",
										},
									},
									Prefixes: []string{"192.0.2.0/24"},
								},
								{
									ASN: 65013,
									ID:  "2001:db8::3",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65014,
											Address: "2001:db8::4",
										},
									},
									VRF:      "vrf2",
									Prefixes: []string{"2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65010,
						RouterID: "192.0.2.5",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65011@192.0.2.6",
								ASN:      "65011",
								Addr:     "192.0.2.6",
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      "65012",
								Addr:     "192.0.2.7",
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24"},
					},
					{
						MyASN:    65013,
						RouterID: "2001:db8::3",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv6,
								Name:     "65014@2001:db8::4",
								ASN:      "65014",
								Addr:     "2001:db8::4",
								VRFName:  "vrf2",
							},
						},
						VRF:          "vrf2",
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
			},
			err: nil,
		},
		{
			name: "IPv4 Neighbor with IPv4 and IPv6 Prefixes",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65020,
									ID:  "192.0.2.10",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65021,
											Address: "192.0.2.11",
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.2.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65020,
						RouterID: "192.0.2.10",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65021@192.0.2.11",
								ASN:      "65021",
								Addr:     "192.0.2.11",
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
			},
			err: nil,
		},
		{
			name: "Empty Configuration",
			fromK8s: []v1beta1.FRRConfiguration{
				{},
			},
			secrets:  map[string]v1.Secret{},
			expected: &frr.Config{},
			err:      nil,
		},
		{
			name: "Non default VRF",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65030,
									ID:  "192.0.2.15",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65031,
											Address: "192.0.2.16",
										},
									},
									VRF:      "vrf1",
									Prefixes: []string{"192.0.2.0/24"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65030,
						RouterID: "192.0.2.15",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65031@192.0.2.16",
								ASN:      "65031",
								Addr:     "192.0.2.16",
								VRFName:  "vrf1",
							},
						},
						VRF:          "vrf1",
						IPV4Prefixes: []string{"192.0.2.0/24"},
					},
				},
			},
			err: nil,
		},
		{
			name: "Neighbor with ToAdvertise",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.2.0/24"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65040,
						RouterID: "192.0.2.20",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.21",
								ASN:      "65041",
								Addr:     "192.0.2.21",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.2.0/24",
										},
									},
								},
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24"},
					},
				},
			},
			err: nil,
		},
		{
			name: "Two Neighbor with ToAdvertise, one advertise all",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.2.0/24", "192.0.4.0/24"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
										{
											ASN:     65041,
											Address: "192.0.2.22",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65040,
						RouterID: "192.0.2.20",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.21",
								ASN:      "65041",
								Addr:     "192.0.2.21",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.2.0/24",
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.4.0/24",
										},
									},
								},
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.22",
								ASN:      "65041",
								Addr:     "192.0.2.22",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.2.0/24",
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.3.0/24",
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.4.0/24",
										},
									},
									PrefixesV6: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv6,
											Prefix:   "2001:db8::/64",
										},
									},
								},
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
			},
			err: nil,
		},
		{
			name: "Two Neighbor with ToAdvertise, one advertise all, both with communities and localPref",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.2.0/24", "192.0.4.0/24", "192.0.6.0/24"},
													Mode:     v1beta1.AllowRestricted,
												},
												PrefixesWithCommunity: []v1beta1.CommunityPrefixes{
													{
														Prefixes:  []string{"192.0.2.0/24", "192.0.4.0/24"},
														Community: "10:100",
													},
													{
														Prefixes:  []string{"192.0.2.0/24"},
														Community: "10:102",
													},
													{
														Prefixes:  []string{"192.0.2.0/24", "192.0.4.0/24"},
														Community: "large:123:456:7890",
													},
													{
														Prefixes:  []string{"192.0.4.0/24"},
														Community: "large:123:456:7892",
													},
													{
														Prefixes:  []string{"192.0.4.0/24"},
														Community: "10:104",
													},
												},
												PrefixesWithLocalPref: []v1beta1.LocalPrefPrefixes{
													{
														Prefixes:  []string{"192.0.2.0/24", "192.0.6.0/24"},
														LocalPref: 100,
													},
													{
														Prefixes:  []string{"192.0.4.0/24"},
														LocalPref: 104,
													},
												},
											},
										},
										{
											ASN:     65041,
											Address: "192.0.2.22",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Mode: v1beta1.AllowAll,
												},
												PrefixesWithCommunity: []v1beta1.CommunityPrefixes{
													{
														Prefixes:  []string{"192.0.2.0/24", "192.0.4.0/24"},
														Community: "10:100",
													},
													{
														Prefixes:  []string{"192.0.2.0/24"},
														Community: "10:102",
													},
													{
														Prefixes:  []string{"192.0.2.0/24", "2001:db8::/64"},
														Community: "10:108",
													},
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24", "192.0.6.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65040,
						RouterID: "192.0.2.20",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.21",
								ASN:      "65041",
								Addr:     "192.0.2.21",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily:         ipfamily.IPv4,
											Prefix:           "192.0.2.0/24",
											Communities:      []string{"10:100", "10:102"},
											LargeCommunities: []string{"123:456:7890"},
											LocalPref:        100,
										},
										{
											IPFamily:         ipfamily.IPv4,
											Prefix:           "192.0.4.0/24",
											Communities:      []string{"10:100", "10:104"},
											LargeCommunities: []string{"123:456:7890", "123:456:7892"},
											LocalPref:        104,
										},
										{
											IPFamily:  ipfamily.IPv4,
											Prefix:    "192.0.6.0/24",
											LocalPref: 100,
										},
									},
								},
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.22",
								ASN:      "65041",
								Addr:     "192.0.2.22",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily:    ipfamily.IPv4,
											Prefix:      "192.0.2.0/24",
											Communities: []string{"10:100", "10:102", "10:108"},
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.3.0/24",
										},
										{
											IPFamily:    ipfamily.IPv4,
											Prefix:      "192.0.4.0/24",
											Communities: []string{"10:100"},
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.6.0/24",
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
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24", "192.0.6.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
			},
			err: nil,
		},
		{
			name: "One neighbor, invalid address",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "19@.0.2.@1",
										},
									},
								},
							},
						},
					},
				},
			},
			secrets:  map[string]v1.Secret{},
			expected: nil,
			err:      fmt.Errorf("failed to find ipfamily"),
		},
		{
			name: "One neighbor, trying to advertise a prefix not in router",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24"},
								},
							},
						},
					},
				},
			},
			secrets:  map[string]v1.Secret{},
			expected: nil,
			err:      fmt.Errorf("prefix %s is not an allowed prefix", "192.0.3.0/24"),
		},
		{
			name: "One neighbor, trying to set community on an unallowed prefix",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.2.0/24", "192.0.4.0/24"},
													Mode:     v1beta1.AllowRestricted,
												},
												PrefixesWithCommunity: []v1beta1.CommunityPrefixes{
													{
														Prefixes:  []string{"192.0.2.0/24", "192.0.4.0/24"},
														Community: "10:100",
													},
													{
														Prefixes:  []string{"192.0.10.10/32"}, // not allowed
														Community: "10:100",
													},
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			secrets:  map[string]v1.Secret{},
			expected: nil,
			err:      fmt.Errorf("prefix %s with community %s not in allowed list for neighbor %s", "192.0.10.10/32", "10:100", "192.0.2.21"),
		},
		{
			name: "One neighbor, trying to set localPref on an unallowed prefix",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.2.0/24", "192.0.4.0/24"},
													Mode:     v1beta1.AllowRestricted,
												},
												PrefixesWithLocalPref: []v1beta1.LocalPrefPrefixes{
													{
														Prefixes:  []string{"192.0.2.0/24", "192.0.4.0/24"},
														LocalPref: 100,
													},
													{
														Prefixes:  []string{"192.0.10.10/32"}, // not allowed
														LocalPref: 101,
													},
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			secrets:  map[string]v1.Secret{},
			expected: nil,
			err:      fmt.Errorf("localPref associated to non existing prefix %s", "192.0.10.10/32"),
		},
		{
			name: "One neighbor, trying to set multiple localPrefs for a prefix",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.2.0/24", "192.0.4.0/24"},
													Mode:     v1beta1.AllowRestricted,
												},
												PrefixesWithLocalPref: []v1beta1.LocalPrefPrefixes{
													{
														Prefixes:  []string{"192.0.2.0/24", "192.0.4.0/24"},
														LocalPref: 100,
													},
													{
														Prefixes:  []string{"192.0.4.0/24"},
														LocalPref: 104,
													},
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			secrets:  map[string]v1.Secret{},
			expected: nil,
			err:      fmt.Errorf("multiple local prefs specified for prefix %s", "192.0.4.0/24"),
		},
		{
			name: "Neighbor with ToReceiveAll",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToReceive: v1beta1.Receive{
												Allowed: v1beta1.AllowedInPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65040,
						RouterID: "192.0.2.20",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.21",
								ASN:      "65041",
								Addr:     "192.0.2.21",
								Incoming: frr.AllowedIn{
									All: true,
								},
							},
						},
					},
				},
			},
			err: nil,
		},
		{
			name: "Neighbor with ToReceive some ips only",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToReceive: v1beta1.Receive{
												Allowed: v1beta1.AllowedInPrefixes{
													Prefixes: []v1beta1.PrefixSelector{
														{Prefix: "192.0.2.0/24"},
														{Prefix: "192.0.3.0/24"},
														{Prefix: "192.0.4.0/24"},
														{Prefix: "2001:db8::/64"},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:        65040,
						RouterID:     "192.0.2.20",
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.21",
								ASN:      "65041",
								Addr:     "192.0.2.21",
								Incoming: frr.AllowedIn{
									All: false,
									PrefixesV4: []frr.IncomingFilter{
										{IPFamily: "ipv4", Prefix: "192.0.2.0/24"},
										{IPFamily: "ipv4", Prefix: "192.0.3.0/24"},
										{IPFamily: "ipv4", Prefix: "192.0.4.0/24"},
									},
									PrefixesV6: []frr.IncomingFilter{
										{IPFamily: "ipv6", Prefix: "2001:db8::/64"},
									},
								},
							},
						},
					},
				},
			},
			err: nil,
		},
		{
			name: "Neighbor with ToReceive some ips only and modifiers",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToReceive: v1beta1.Receive{
												Allowed: v1beta1.AllowedInPrefixes{
													Prefixes: []v1beta1.PrefixSelector{
														{Prefix: "192.0.2.0/24", LE: 32, GE: 26},
														{Prefix: "192.0.3.0/24", LE: 32, GE: 26},
														{Prefix: "192.0.4.0/24"},
														{Prefix: "2001:db8::/64"},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:        65040,
						RouterID:     "192.0.2.20",
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.21",
								ASN:      "65041",
								Addr:     "192.0.2.21",
								Incoming: frr.AllowedIn{
									All: false,
									PrefixesV4: []frr.IncomingFilter{
										{IPFamily: "ipv4", Prefix: "192.0.2.0/24", LE: 32, GE: 26},
										{IPFamily: "ipv4", Prefix: "192.0.3.0/24", LE: 32, GE: 26},
										{IPFamily: "ipv4", Prefix: "192.0.4.0/24"},
									},
									PrefixesV6: []frr.IncomingFilter{
										{IPFamily: "ipv6", Prefix: "2001:db8::/64"},
									},
								},
							},
						},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
			},
			err: nil,
		},
		{
			name: "Neighbor with ToReceive some ips only and setting le and ge",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToReceive: v1beta1.Receive{
												Allowed: v1beta1.AllowedInPrefixes{
													Prefixes: []v1beta1.PrefixSelector{
														{Prefix: "192.0.2.0/24", LE: 10, GE: 12},
														{Prefix: "192.0.3.0/24", LE: 32, GE: 12},
														{Prefix: "192.0.4.0/24"},
														{Prefix: "2001:db8::/64"},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			secrets:  map[string]v1.Secret{},
			expected: nil,
			err:      errors.New("failed to process neighbor 65041@192.0.2.21 for router 65040-: invalid prefix 192.0.2.0/24 selector: GE 12 > LE 10"),
		},
		{
			name: "Multiple FRRConfigurations - Single Router and neighbor, one config for advertise the other for receiving",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65012,
											Address: "192.0.2.7",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.2.10/32", "192.0.2.11/32"},
													Mode:     v1beta1.AllowRestricted,
												},
												PrefixesWithCommunity: []v1beta1.CommunityPrefixes{
													{
														Community: "10:100",
														Prefixes:  []string{"192.0.2.10/32"},
													},
													{
														Community: "10:101",
														Prefixes:  []string{"192.0.2.10/32", "192.0.2.11/32"},
													},
												},
												PrefixesWithLocalPref: []v1beta1.LocalPrefPrefixes{
													{
														LocalPref: 200,
														Prefixes:  []string{"192.0.2.10/32"},
													},
												},
											},
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.2.10/32", "192.0.2.11/32"},
								},
							},
						},
					},
				},
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65012,
											Address: "192.0.2.7",
											ToReceive: v1beta1.Receive{
												Allowed: v1beta1.AllowedInPrefixes{
													Mode: v1beta1.AllowRestricted,
													Prefixes: []v1beta1.PrefixSelector{
														{Prefix: "192.0.100.0/24"},
														{Prefix: "192.0.101.0/24"},
													},
												},
											},
										},
									},
									VRF: "",
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65010,
						RouterID: "192.0.2.5",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      "65012",
								Addr:     "192.0.2.7",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily:    ipfamily.IPv4,
											Prefix:      "192.0.2.10/32",
											Communities: []string{"10:100", "10:101"},
											LocalPref:   200,
										},
										{
											IPFamily:    ipfamily.IPv4,
											Prefix:      "192.0.2.11/32",
											Communities: []string{"10:101"},
										},
									},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.100.0/24",
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.101.0/24",
										},
									},
								},
							},
						},
						VRF:          "",
						IPV4Prefixes: []string{"192.0.2.10/32", "192.0.2.11/32"},
					},
				},
			},
			err: nil,
		},
		{
			name: "Multiple FRRConfigurations - Single Router and neighbor, merging different lengths",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65012,
											Address: "192.0.2.7",
											ToReceive: v1beta1.Receive{
												Allowed: v1beta1.AllowedInPrefixes{
													Mode: v1beta1.AllowRestricted,
													Prefixes: []v1beta1.PrefixSelector{
														{Prefix: "192.0.100.0/24", LE: 28},
														{Prefix: "192.0.101.0/24", LE: 32, GE: 26},
														{Prefix: "192.0.102.0/24", LE: 32, GE: 26},
													},
												},
											},
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.2.10/32", "192.0.2.11/32"},
								},
							},
						},
					},
				},
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65012,
											Address: "192.0.2.7",
											ToReceive: v1beta1.Receive{
												Allowed: v1beta1.AllowedInPrefixes{
													Mode: v1beta1.AllowRestricted,
													Prefixes: []v1beta1.PrefixSelector{
														{Prefix: "192.0.100.0/24", LE: 32},
														{Prefix: "192.0.101.0/24", GE: 24},
														{Prefix: "192.0.102.0/24", LE: 32, GE: 26},
													},
												},
											},
										},
									},
									VRF: "",
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65010,
						RouterID: "192.0.2.5",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      "65012",
								Addr:     "192.0.2.7",
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.100.0/24",
											LE:       28,
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.100.0/24",
											LE:       32,
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.101.0/24",
											GE:       24,
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.101.0/24",
											LE:       32,
											GE:       26,
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.102.0/24",
											LE:       32,
											GE:       26,
										},
									},
								},
							},
						},
						VRF:          "",
						IPV4Prefixes: []string{"192.0.2.10/32", "192.0.2.11/32"},
					},
				},
			},
			err: nil,
		},
		{
			name: "Multiple FRRConfigurations - Multiple Routers and Neighbors",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65012,
											Address: "192.0.2.7",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.2.10/32", "192.0.2.11/32"},
													Mode:     v1beta1.AllowRestricted,
												},
												PrefixesWithCommunity: []v1beta1.CommunityPrefixes{
													{
														Community: "10:100",
														Prefixes:  []string{"192.0.2.10/32"},
													},
													{
														Community: "10:101",
														Prefixes:  []string{"192.0.2.10/32", "192.0.2.11/32"},
													},
												},
												PrefixesWithLocalPref: []v1beta1.LocalPrefPrefixes{
													{
														LocalPref: 200,
														Prefixes:  []string{"192.0.2.10/32"},
													},
												},
											},
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.2.10/32", "192.0.2.11/32"},
								},
								{
									ASN: 65013,
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65017,
											Address: "192.0.2.7",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.2.5/32"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
										{
											ASN:     65014,
											Address: "2001:db8::4",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
									VRF:      "vrf2",
									Prefixes: []string{"192.0.2.5/32", "2001:db8::/64"},
								},
							},
						},
					},
				},
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65011,
											Address: "192.0.2.6",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.3.1/32", "192.0.3.2/32"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
										{
											ASN:     65012,
											Address: "192.0.2.7",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.3.20/32", "192.0.3.21/32"},
													Mode:     v1beta1.AllowRestricted,
												},
												PrefixesWithCommunity: []v1beta1.CommunityPrefixes{
													{
														Community: "10:100",
														Prefixes:  []string{"192.0.3.20/32"},
													},
													{
														Community: "10:101",
														Prefixes:  []string{"192.0.3.21/32"},
													},
												},
												PrefixesWithLocalPref: []v1beta1.LocalPrefPrefixes{
													{
														LocalPref: 200,
														Prefixes:  []string{"192.0.3.21/32"},
													},
												},
											},
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.3.1/32", "192.0.3.2/32", "192.0.3.20/32", "192.0.3.21/32"},
								},
								{
									ASN: 65013,
									ID:  "2001:db8::3",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65014,
											Address: "2001:db8::4",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"2001:db9::/96"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
									},
									VRF:      "vrf2",
									Prefixes: []string{"2001:db9::/96"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65010,
						RouterID: "192.0.2.5",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65011@192.0.2.6",
								ASN:      "65011",
								Addr:     "192.0.2.6",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.3.1/32",
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.3.2/32",
										},
									},
								},
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      "65012",
								Addr:     "192.0.2.7",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily:    ipfamily.IPv4,
											Prefix:      "192.0.2.10/32",
											Communities: []string{"10:100", "10:101"},
											LocalPref:   200,
										},
										{
											IPFamily:    ipfamily.IPv4,
											Prefix:      "192.0.2.11/32",
											Communities: []string{"10:101"},
										},
										{
											IPFamily:    ipfamily.IPv4,
											Prefix:      "192.0.3.20/32",
											Communities: []string{"10:100"},
										},
										{
											IPFamily:    ipfamily.IPv4,
											Prefix:      "192.0.3.21/32",
											Communities: []string{"10:101"},
											LocalPref:   200,
										},
									},
								},
							},
						},
						VRF:          "",
						IPV4Prefixes: []string{"192.0.2.10/32", "192.0.2.11/32", "192.0.3.1/32", "192.0.3.2/32", "192.0.3.20/32", "192.0.3.21/32"},
					},
					{
						MyASN:    65013,
						RouterID: "2001:db8::3",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65017@192.0.2.7",
								ASN:      "65017",
								Addr:     "192.0.2.7",
								VRFName:  "vrf2",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.2.5/32",
										},
									},
								},
							},
							{
								IPFamily: ipfamily.IPv6,
								Name:     "65014@2001:db8::4",
								ASN:      "65014",
								Addr:     "2001:db8::4",
								VRFName:  "vrf2",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: "ipv4",
											Prefix:   "192.0.2.5/32",
										},
									},
									PrefixesV6: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv6,
											Prefix:   "2001:db8::/64",
										},
										{
											IPFamily: ipfamily.IPv6,
											Prefix:   "2001:db9::/96",
										},
									},
								},
							},
						},
						VRF:          "vrf2",
						IPV4Prefixes: []string{"192.0.2.5/32"},
						IPV6Prefixes: []string{"2001:db8::/64", "2001:db9::/96"},
					},
				},
			},
			err: nil,
		},
		{
			name: "Multiple Routers and Neighbors with passwords",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65012,
											Address: "192.0.2.7",
											PasswordSecret: v1beta1.SecretReference{
												Name:      "secret1",
												Namespace: "frr-k8s-system",
											},
										},
										{
											ASN:      65012,
											Address:  "192.0.2.8",
											Password: "cleartext-password",
										},
									},
									VRF: "",
								},
								{
									ASN: 65013,
									ID:  "2001:db8::3",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65017,
											Address: "192.0.2.7",
										},
										{
											ASN:     65014,
											Address: "2001:db8::4",
											PasswordSecret: v1beta1.SecretReference{
												Name:      "secret2",
												Namespace: "frr-k8s-system",
											},
										},
									},
									VRF: "vrf2",
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{
				"secret1": {
					Type: v1.SecretTypeBasicAuth,
					Data: map[string][]byte{
						"password": []byte("password1"),
					},
				},
				"secret2": {
					Type: v1.SecretTypeBasicAuth,
					Data: map[string][]byte{
						"password": []byte("password2"),
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65010,
						RouterID: "192.0.2.5",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      "65012",
								Addr:     "192.0.2.7",
								Password: "password1",
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.8",
								ASN:      "65012",
								Addr:     "192.0.2.8",
								Password: "cleartext-password",
							},
						},
						VRF: "",
					},
					{
						MyASN:    65013,
						RouterID: "2001:db8::3",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65017@192.0.2.7",
								ASN:      "65017",
								Addr:     "192.0.2.7",
								VRFName:  "vrf2",
							},
							{
								IPFamily: ipfamily.IPv6,
								Name:     "65014@2001:db8::4",
								ASN:      "65014",
								Addr:     "2001:db8::4",
								VRFName:  "vrf2",
								Password: "password2",
							},
						},
						VRF: "vrf2",
					},
				},
			},
			err: nil,
		},
		{
			name: "Non existing secret ref",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65012,
											Address: "192.0.2.7",
											PasswordSecret: v1beta1.SecretReference{
												Name:      "secret1",
												Namespace: "frr-k8s-system",
											},
										},
									},
									VRF: "",
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{
				"secret2": {
					Type: v1.SecretTypeBasicAuth,
					Data: map[string][]byte{
						"password": []byte("password2"),
					},
				},
			},
			expected: nil,
			err:      errors.New("failed to process neighbor 65012@192.0.2.7 for router 65010-: secret ref not found for neighbor 65012@192.0.2.7"),
		},
		{
			name: "Specifying both cleartext password and secret ref",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:      65012,
											Address:  "192.0.2.7",
											Password: "cleartext-password",
											PasswordSecret: v1beta1.SecretReference{
												Name:      "secret1",
												Namespace: "frr-k8s-system",
											},
										},
									},
									VRF: "",
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{
				"secret1": {
					Type: v1.SecretTypeBasicAuth,
					Data: map[string][]byte{
						"password": []byte("password"),
					},
				},
			},
			expected: nil,
			err:      errors.New("failed to process neighbor 65012@192.0.2.7 for router 65010-: neighbor 65012@192.0.2.7 specifies both cleartext password and secret ref"),
		},
		{
			name: "Single Router and injection",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65001,
									ID:  "192.0.2.1",
								},
							},
						},
						Raw: v1beta1.RawConfig{
							Config: "foo",
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65001,
						RouterID: "192.0.2.1",
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				ExtraConfig: "foo\n",
			},
			err: nil,
		},
		{
			name: "Single Router and double injection",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65001,
									ID:  "192.0.2.1",
								},
							},
						},
						Raw: v1beta1.RawConfig{
							Config:   "foo",
							Priority: 5,
						},
					},
				}, {
					Spec: v1beta1.FRRConfigurationSpec{
						Raw: v1beta1.RawConfig{
							Config:   "bar\nbaz",
							Priority: 10,
						},
					},
				}, {
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65001,
									ID:  "192.0.2.1",
								},
							},
						},
						Raw: v1beta1.RawConfig{
							Config: "bar",
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65001,
						RouterID: "192.0.2.1",
					},
				},
				ExtraConfig: "bar\nfoo\nbar\nbaz\n",
			},
			err: nil,
		},
		{
			name: "Neighbor with BFDProfile",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:        65041,
											Address:    "192.0.2.21",
											BFDProfile: "bfd1",
										},
									},
								},
							},
							BFDProfiles: []v1beta1.BFDProfile{
								{
									Name:             "bfd1",
									ReceiveInterval:  ptr.To[uint32](42),
									TransmitInterval: ptr.To[uint32](43),
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:        65040,
						RouterID:     "192.0.2.20",
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily:   ipfamily.IPv4,
								Name:       "65041@192.0.2.21",
								ASN:        "65041",
								Addr:       "192.0.2.21",
								BFDProfile: "bfd1",
							},
						},
					},
				},
				BFDProfiles: []frr.BFDProfile{
					{
						Name:             "bfd1",
						ReceiveInterval:  ptr.To[uint32](42),
						TransmitInterval: ptr.To[uint32](43),
					},
				},
			},
			err: nil,
		},
		{
			name: "Neighbor with BFDProfile does not exist",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:        65041,
											Address:    "192.0.2.21",
											BFDProfile: "bfd2",
										},
									},
								},
							},
							BFDProfiles: []v1beta1.BFDProfile{
								{
									Name:             "bfd1",
									ReceiveInterval:  ptr.To[uint32](42),
									TransmitInterval: ptr.To[uint32](43),
								},
							},
						},
					},
				},
			},
			secrets:  map[string]v1.Secret{},
			expected: nil,
			err:      errors.New("got failed to process neighbor 65041@192.0.2.21 for router 65040-: neighbor 65041@192.0.2.21 referencing non existing BFDProfile bfd2"),
		},
		{
			name: "Neighbor with BFDProfile in different config",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:        65041,
											Address:    "192.0.2.21",
											BFDProfile: "bfd2",
										},
									},
								},
							},
						},
					},
				},
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							BFDProfiles: []v1beta1.BFDProfile{
								{
									Name:             "bfd1",
									ReceiveInterval:  ptr.To[uint32](42),
									TransmitInterval: ptr.To[uint32](43),
								},
							},
						},
					},
				},
			},
			secrets:  map[string]v1.Secret{},
			expected: nil,
			err:      errors.New("got failed to process neighbor 65041@192.0.2.21 for router 65040-: neighbor 65041@192.0.2.21 referencing non existing BFDProfile bfd2"),
		},
		{
			name: "Two BFDProfiles, but identical",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							BFDProfiles: []v1beta1.BFDProfile{
								{
									Name:             "bfd1",
									ReceiveInterval:  ptr.To[uint32](42),
									TransmitInterval: ptr.To[uint32](43),
								},
								{
									Name:             "bfd1",
									ReceiveInterval:  ptr.To[uint32](42),
									TransmitInterval: ptr.To[uint32](43),
								},
								{
									Name:             "bfd3",
									ReceiveInterval:  ptr.To[uint32](42),
									TransmitInterval: ptr.To[uint32](44),
								},
							},
						},
					},
				},
			},
			secrets:  map[string]v1.Secret{},
			expected: nil,
			err:      errors.New("duplicate bfd profile name bfd1"),
		},
		{
			name: "Two BFDProfiles, from different configs",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							BFDProfiles: []v1beta1.BFDProfile{
								{
									Name:             "bfd1",
									ReceiveInterval:  ptr.To[uint32](42),
									TransmitInterval: ptr.To[uint32](43),
								},
								{
									Name:             "bfd2",
									ReceiveInterval:  ptr.To[uint32](42),
									TransmitInterval: ptr.To[uint32](44),
								},
							},
						},
					},
				},
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							BFDProfiles: []v1beta1.BFDProfile{
								{
									Name:             "bfd1",
									ReceiveInterval:  ptr.To[uint32](42),
									TransmitInterval: ptr.To[uint32](43),
								},
								{
									Name:             "bfd3",
									ReceiveInterval:  ptr.To[uint32](42),
									TransmitInterval: ptr.To[uint32](45),
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{},
				BFDProfiles: []frr.BFDProfile{
					{
						Name:             "bfd1",
						ReceiveInterval:  ptr.To[uint32](42),
						TransmitInterval: ptr.To[uint32](43),
					},
					{
						Name:             "bfd2",
						ReceiveInterval:  ptr.To[uint32](42),
						TransmitInterval: ptr.To[uint32](44),
					},
					{
						Name:             "bfd3",
						ReceiveInterval:  ptr.To[uint32](42),
						TransmitInterval: ptr.To[uint32](45),
					},
				},
			},
			err: nil,
		},
		{
			name: "HoldTime without KeepaliveTime",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65001,
									ID:  "192.0.2.1",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:      65002,
											Address:  "192.0.2.2",
											HoldTime: &metav1.Duration{Duration: 120 * time.Second},
										},
									},
								},
							},
						},
					},
				},
			},
			secrets:  map[string]v1.Secret{},
			expected: nil,
			err:      errors.New(`failed to process neighbor 65002@192.0.2.2 for router 65001-: one of KeepaliveTime/HoldTime specified, both must be set or none`),
		},
		{
			name: "KeepaliveTime without HoldTime",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65001,
									ID:  "192.0.2.1",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:           65002,
											Address:       "192.0.2.2",
											KeepaliveTime: &metav1.Duration{Duration: 120 * time.Second},
										},
									},
								},
							},
						},
					},
				},
			},
			secrets:  map[string]v1.Secret{},
			expected: nil,
			err:      errors.New(`failed to process neighbor 65002@192.0.2.2 for router 65001-: one of KeepaliveTime/HoldTime specified, both must be set or none`),
		},
		{
			name: "HoldTime bigger than keepalive time",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65001,
									ID:  "192.0.2.1",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65002,
											Address: "192.0.2.2",
											KeepaliveTime: &metav1.Duration{
												Duration: 50 * time.Second,
											},
											HoldTime: &metav1.Duration{
												Duration: 40 * time.Second,
											},
										},
									},
									VRF: "",
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			err:     errors.New(`failed to process neighbor 65002@192.0.2.2 for router 65001-: invalid keepaliveTime {"50s"}`),
		},
		{
			name: "With alwaysblock",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65011,
											Address: "192.0.2.6",
										},
									},
									VRF: "",
								},
								{
									ASN: 65013,
									ID:  "2001:db8::3",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65014,
											Address: "2001:db8::4",
										},
									},
									VRF:      "vrf2",
									Prefixes: []string{},
								},
							},
						},
					},
				},
			},
			secrets:     map[string]v1.Secret{},
			alwaysBlock: []net.IPNet{*ipv4CIDR, *ipv6CIDR},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65010,
						RouterID: "192.0.2.5",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65011@192.0.2.6",
								ASN:      "65011",
								Addr:     "192.0.2.6",
								AlwaysBlock: []frr.IncomingFilter{
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.168.1.0/24",
										LE:       uint32(32),
									}, {
										IPFamily: ipfamily.IPv6,
										Prefix:   "fc00:f853:ccd:e800::/64",
										LE:       uint32(128),
									},
								},
							},
						},
					},
					{
						MyASN:    65013,
						RouterID: "2001:db8::3",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv6,
								Name:     "65014@2001:db8::4",
								ASN:      "65014",
								Addr:     "2001:db8::4",
								VRFName:  "vrf2",
								AlwaysBlock: []frr.IncomingFilter{
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.168.1.0/24",
										LE:       uint32(32),
									}, {
										IPFamily: ipfamily.IPv6,
										Prefix:   "fc00:f853:ccd:e800::/64",
										LE:       uint32(128),
									},
								},
							},
						},
						VRF: "vrf2",
					},
				},
			},
			err: nil,
		},
		{
			name: "Multiple Routers import VRFs",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									VRF: "",
									Imports: []v1beta1.Import{
										{VRF: "red"},
									},
								},
								{
									ASN: 65013,
									ID:  "2001:db8::3",
									VRF: "red",
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:      65010,
						RouterID:   "192.0.2.5",
						VRF:        "",
						ImportVRFs: []string{"red"},
					},
					{
						MyASN:    65013,
						RouterID: "2001:db8::3",
						VRF:      "red",
					},
				},
			},
			err: nil,
		},
		{
			name: "Multiple Routers import VRF, advertise ips from the imported vrf",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									VRF: "",
									Imports: []v1beta1.Import{
										{VRF: "red"},
									},
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Prefixes: []string{"192.0.2.0/24", "192.0.5.0/24"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
										{
											ASN:     65041,
											Address: "192.0.2.22",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "2001:db8::/64"},
								},
								{
									ASN:      65013,
									ID:       "192.0.2.20",
									VRF:      "red",
									Prefixes: []string{"192.0.5.0/24", "2001:db9::/64"},
								},
							},
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65040,
						RouterID: "192.0.2.20",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.21",
								ASN:      "65041",
								Addr:     "192.0.2.21",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.2.0/24",
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.5.0/24",
										},
									},
								},
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.22",
								ASN:      "65041",
								Addr:     "192.0.2.22",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.2.0/24",
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.5.0/24",
										},
									},
									PrefixesV6: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv6,
											Prefix:   "2001:db8::/64",
										}, {
											IPFamily: ipfamily.IPv6,
											Prefix:   "2001:db9::/64",
										},
									},
								},
							},
						},
						ImportVRFs:   []string{"red"},
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
					{
						MyASN:        65013,
						RouterID:     "192.0.2.20",
						VRF:          "red",
						IPV4Prefixes: []string{"192.0.5.0/24"},
						IPV6Prefixes: []string{"2001:db9::/64"},
					},
				},
			},
		},
		{
			name: "Multiple Routers import non existing VRFs",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									VRF: "",
									Imports: []v1beta1.Import{
										{VRF: "blue"},
									},
								},
								{
									ASN: 65013,
									ID:  "2001:db8::3",
									VRF: "red",
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			err:     errors.New("router 65010- imports vrf blue which is not defined"),
		},
		{
			name: "Multiple Routers import VRF, red imports default and advertises",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									VRF: "red",
									Imports: []v1beta1.Import{
										{VRF: "default"},
									},
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.22",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "2001:db8::/64"},
								},
								{
									ASN:      65013,
									ID:       "192.0.2.20",
									Prefixes: []string{"192.0.5.0/24", "2001:db9::/64"},
								},
							},
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:        65013,
						RouterID:     "192.0.2.20",
						IPV4Prefixes: []string{"192.0.5.0/24"},
						IPV6Prefixes: []string{"2001:db9::/64"},
					},
					{
						MyASN:    65040,
						VRF:      "red",
						RouterID: "192.0.2.20",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.22",
								ASN:      "65041",
								Addr:     "192.0.2.22",
								VRFName:  "red",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.2.0/24",
										},
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.5.0/24",
										},
									},
									PrefixesV6: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv6,
											Prefix:   "2001:db8::/64",
										}, {
											IPFamily: ipfamily.IPv6,
											Prefix:   "2001:db9::/64",
										},
									},
								},
							},
						},
						ImportVRFs:   []string{"default"},
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
			},
		},
		{
			name: "Neighbor without ASN or DynamicASN",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									VRF: "",
									Neighbors: []v1beta1.Neighbor{
										{
											Address: "192.0.2.22",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			err:     errors.New("65010-: neighbor 0@192.0.2.22 has no ASN or DynamicASN specified"),
		},
		{
			name: "Neighbor with both ASN and DynamicASN",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									VRF: "",
									Neighbors: []v1beta1.Neighbor{
										{
											Address:    "192.0.2.22",
											ASN:        65010,
											DynamicASN: "internal",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			err:     errors.New("neighbor internal@192.0.2.22 has both ASN and DynamicASN specified"),
		},
		{
			name: "Neighbor with DynamicASN",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									VRF: "",
									Neighbors: []v1beta1.Neighbor{
										{
											Address:    "192.0.2.22",
											DynamicASN: "internal",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65010,
						VRF:      "",
						RouterID: "192.0.2.5",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "internal@192.0.2.22",
								ASN:      "internal",
								Addr:     "192.0.2.22",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.2.0/24",
										},
									},
									PrefixesV6: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv6,
											Prefix:   "2001:db8::/64",
										},
									},
								},
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			err:     nil,
		},
		{
			name: "Neighbor with Interface and without Address",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									VRF: "",
									Neighbors: []v1beta1.Neighbor{
										{
											Interface:  "eth0",
											DynamicASN: "internal",
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65010,
						VRF:      "",
						RouterID: "192.0.2.5",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily:   ipfamily.DualStack,
								Name:       "internal@eth0",
								ASN:        "internal",
								Addr:       "eth0",
								Unnumbered: true,
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.2.0/24",
										},
									},
									PrefixesV6: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv6,
											Prefix:   "2001:db8::/64",
										},
									},
								},
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			err:     nil,
		},
		{
			name: "Neighbor with no Address and no Interface",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									VRF: "",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN: 65010,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedOutPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			secrets: map[string]v1.Secret{},
			err:     errors.New("a not nil error"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resources := ClusterResources{
				FRRConfigs:      test.fromK8s,
				PasswordSecrets: test.secrets,
			}
			frr, err := apiToFRR(resources, test.alwaysBlock)
			// DISCUSS do we need to enforce checking the err values/string?
			// if test.err != err {
			// 	t.Fatalf("\nexpected error %s,\n got %v", test.err, err)
			// }
			if test.err != nil && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if test.err == nil && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if diff := cmp.Diff(frr, test.expected, cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("config different from expected: %s", diff)
			}
		})
	}
}

func TestFilterForSelector(t *testing.T) {
	tests := []struct {
		name     string
		selector v1beta1.PrefixSelector
		expected frr.IncomingFilter
		mustFail bool
	}{
		{
			name: "prefix only",
			selector: v1beta1.PrefixSelector{
				Prefix: "192.168.1.0/24",
			},
			expected: frr.IncomingFilter{
				IPFamily: ipfamily.IPv4,
				Prefix:   "192.168.1.0/24",
			},
		},
		{
			name: "prefix with valid modifiers",
			selector: v1beta1.PrefixSelector{
				Prefix: "192.168.1.0/24",
				LE:     32,
				GE:     25,
			},
			expected: frr.IncomingFilter{
				IPFamily: ipfamily.IPv4,
				Prefix:   "192.168.1.0/24",
				LE:       32,
				GE:       25,
			},
		},
		{
			name: "prefix le smaller than mask",
			selector: v1beta1.PrefixSelector{
				Prefix: "192.168.1.0/24",
				LE:     22,
			},
			expected: frr.IncomingFilter{},
			mustFail: true,
		},
		{
			name: "prefix le valid, ge smaller than mask",
			selector: v1beta1.PrefixSelector{
				Prefix: "192.168.1.0/24",
				LE:     32,
				GE:     22,
			},
			expected: frr.IncomingFilter{},
			mustFail: true,
		},
		{
			name: "prefix le nil, ge smaller than mask",
			selector: v1beta1.PrefixSelector{
				Prefix: "192.168.1.0/24",
				GE:     22,
			},
			expected: frr.IncomingFilter{},
			mustFail: true,
		},
		{
			name: "prefix ge greater than le",
			selector: v1beta1.PrefixSelector{
				Prefix: "192.168.1.0/24",
				LE:     26,
				GE:     28,
			},
			expected: frr.IncomingFilter{},
			mustFail: true,
		},
		{
			name: "prefix with valid modifiers, ipv6",
			selector: v1beta1.PrefixSelector{
				Prefix: "fc00:f853:ccd:e799::/64",
				LE:     128,
				GE:     70,
			},
			expected: frr.IncomingFilter{
				IPFamily: ipfamily.IPv6,
				Prefix:   "fc00:f853:ccd:e799::/64",
				LE:       128,
				GE:       70,
			},
		},
		{
			name: "prefix with invalid modifiers, ipv6",
			selector: v1beta1.PrefixSelector{
				Prefix: "fc00:f853:ccd:e799::/64",
				LE:     63,
			},
			mustFail: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filter, err := filterForSelector(test.selector)
			if test.mustFail && err == nil {
				t.Fatalf("expecting error, got nil")
			}
			if !test.mustFail && err != nil {
				t.Fatalf("not expecting error, got %s", err)
			}

			if diff := cmp.Diff(filter, test.expected); diff != "" {
				t.Fatalf("filter different from expected: %s", diff)
			}
		})
	}
}
