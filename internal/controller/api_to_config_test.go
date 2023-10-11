// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	v1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8s/internal/frr"
	"github.com/metallb/frrk8s/internal/ipfamily"
	"k8s.io/utils/pointer"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConversion(t *testing.T) {
	tests := []struct {
		name     string
		fromK8s  []v1beta1.FRRConfiguration
		secrets  map[string]v1.Secret
		expected *frr.Config
		err      error
	}{

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
											Address: "192.0.2.2",
											Port:    179,
											KeepaliveTime: metav1.Duration{
												Duration: 20 * time.Second,
											},
											HoldTime: metav1.Duration{
												Duration: 40 * time.Second,
											},
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
								ASN:           65002,
								Addr:          "192.0.2.2",
								Port:          179,
								KeepaliveTime: 20,
								HoldTime:      40,
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
						VRF:          "",
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
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
											Port:    179,
										},
										{
											ASN:     65012,
											Address: "192.0.2.7",
											Port:    179,
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.2.0/24"},
								},
								{
									ASN: 65013,
									ID:  "2001:db8::3",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65014,
											Address: "2001:db8::4",
											Port:    179,
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
								ASN:      65011,
								Addr:     "192.0.2.6",
								Port:     179,
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{},
									PrefixesV6: []frr.OutgoingFilter{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      65012,
								Addr:     "192.0.2.7",
								Port:     179,
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
						VRF:          "",
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{},
					},
					{
						MyASN:    65013,
						RouterID: "2001:db8::3",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv6,
								Name:     "65014@2001:db8::4",
								ASN:      65014,
								Addr:     "2001:db8::4",
								Port:     179,
								VRFName:  "vrf2",
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
						VRF:          "vrf2",
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
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
											Port:    179,
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
								ASN:      65021,
								Addr:     "192.0.2.11",
								Port:     179,
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
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
			},
			err: nil,
		},
		{
			name: "Empty Configuration",
			fromK8s: []v1beta1.FRRConfiguration{
				{},
			},
			secrets: map[string]v1.Secret{},
			expected: &frr.Config{
				Routers:     []*frr.RouterConfig{},
				BFDProfiles: []frr.BFDProfile{},
			},
			err: nil,
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
											Port:    179,
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
								ASN:      65031,
								Addr:     "192.0.2.16",
								Port:     179,
								VRFName:  "vrf1",
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
						VRF:          "vrf1",
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
								ASN:      65041,
								Addr:     "192.0.2.21",
								Port:     179,
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.2.0/24",
										},
									},
									PrefixesV6: []frr.OutgoingFilter{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Prefixes: []string{"192.0.2.0/24", "192.0.4.0/24"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
										{
											ASN:     65041,
											Address: "192.0.2.22",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
								ASN:      65041,
								Addr:     "192.0.2.21",
								Port:     179,
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
									PrefixesV6: []frr.OutgoingFilter{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.22",
								ASN:      65041,
								Addr:     "192.0.2.22",
								Port:     179,
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
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
								ASN:      65041,
								Addr:     "192.0.2.21",
								Port:     179,
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
									PrefixesV6: []frr.OutgoingFilter{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.22",
								ASN:      65041,
								Addr:     "192.0.2.22",
								Port:     179,
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
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
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
											Port:    179,
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
											Port:    179,
											ToReceive: v1beta1.Receive{
												Allowed: v1beta1.AllowedPrefixes{
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
						MyASN:        65040,
						RouterID:     "192.0.2.20",
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.21",
								ASN:      65041,
								Addr:     "192.0.2.21",
								Port:     179,
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
					},
				},
				BFDProfiles: []frr.BFDProfile{},
			},
			err: nil,
		}, {
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
											Port:    179,
											ToReceive: v1beta1.Receive{
												Allowed: v1beta1.AllowedPrefixes{
													Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24", "2001:db8::/64"},
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
								ASN:      65041,
								Addr:     "192.0.2.21",
								Port:     179,
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{},
									PrefixesV6: []frr.OutgoingFilter{},
								},
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
				BFDProfiles: []frr.BFDProfile{},
			},
			err: nil,
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
											Port:    179,
											ToReceive: v1beta1.Receive{
												Allowed: v1beta1.AllowedPrefixes{
													Mode:     v1beta1.AllowRestricted,
													Prefixes: []string{"192.0.100.0/24", "192.0.101.0/24"},
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
								ASN:      65012,
								Addr:     "192.0.2.7",
								Port:     179,
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
									PrefixesV6: []frr.OutgoingFilter{},
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
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
						},
						VRF:          "",
						IPV4Prefixes: []string{"192.0.2.10/32", "192.0.2.11/32"},
						IPV6Prefixes: []string{},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Prefixes: []string{"192.0.2.5/32"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
										{
											ASN:     65014,
											Address: "2001:db8::4",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Prefixes: []string{"192.0.3.1/32", "192.0.3.2/32"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
										{
											ASN:     65012,
											Address: "192.0.2.7",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
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
								ASN:      65011,
								Addr:     "192.0.2.6",
								Port:     179,
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
									PrefixesV6: []frr.OutgoingFilter{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      65012,
								Addr:     "192.0.2.7",
								Port:     179,
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
									PrefixesV6: []frr.OutgoingFilter{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
						},
						VRF:          "",
						IPV4Prefixes: []string{"192.0.2.10/32", "192.0.2.11/32", "192.0.3.1/32", "192.0.3.2/32", "192.0.3.20/32", "192.0.3.21/32"},
						IPV6Prefixes: []string{},
					},
					{
						MyASN:    65013,
						RouterID: "2001:db8::3",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65017@192.0.2.7",
								ASN:      65017,
								Addr:     "192.0.2.7",
								Port:     179,
								VRFName:  "vrf2",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{
										{
											IPFamily: ipfamily.IPv4,
											Prefix:   "192.0.2.5/32",
										},
									},
									PrefixesV6: []frr.OutgoingFilter{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
							{
								IPFamily: ipfamily.IPv6,
								Name:     "65014@2001:db8::4",
								ASN:      65014,
								Addr:     "2001:db8::4",
								Port:     179,
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
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
						},
						VRF:          "vrf2",
						IPV4Prefixes: []string{"192.0.2.5/32"},
						IPV6Prefixes: []string{"2001:db8::/64", "2001:db9::/96"},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
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
											Port:    179,
											PasswordSecret: v1.SecretReference{
												Name:      "secret1",
												Namespace: "frr-k8s-system",
											},
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
											Port:    179,
										},
										{
											ASN:     65014,
											Address: "2001:db8::4",
											Port:    179,
											PasswordSecret: v1.SecretReference{
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
								ASN:      65012,
								Addr:     "192.0.2.7",
								Port:     179,
								Password: "password1",
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
						VRF:          "",
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
					},
					{
						MyASN:    65013,
						RouterID: "2001:db8::3",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65017@192.0.2.7",
								ASN:      65017,
								Addr:     "192.0.2.7",
								Port:     179,
								VRFName:  "vrf2",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{},
									PrefixesV6: []frr.OutgoingFilter{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
							{
								IPFamily: ipfamily.IPv6,
								Name:     "65014@2001:db8::4",
								ASN:      65014,
								Addr:     "2001:db8::4",
								Port:     179,
								VRFName:  "vrf2",
								Password: "password2",
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
						VRF:          "vrf2",
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
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
											Port:    179,
											PasswordSecret: v1.SecretReference{
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
						MyASN:        65001,
						RouterID:     "192.0.2.1",
						Neighbors:    []*frr.NeighborConfig{},
						VRF:          "",
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
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
						MyASN:        65001,
						RouterID:     "192.0.2.1",
						Neighbors:    []*frr.NeighborConfig{},
						VRF:          "",
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
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
											Port:       179,
											BFDProfile: "bfd1",
										},
									},
								},
							},
							BFDProfiles: []v1beta1.BFDProfile{
								{
									Name:             "bfd1",
									ReceiveInterval:  42,
									TransmitInterval: 43,
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
								ASN:        65041,
								Addr:       "192.0.2.21",
								Port:       179,
								BFDProfile: "bfd1",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{},
									PrefixesV6: []frr.OutgoingFilter{},
								},
								Incoming: frr.AllowedIn{
									All:        false,
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
							},
						},
					},
				},
				BFDProfiles: []frr.BFDProfile{
					{
						Name:             "bfd1",
						ReceiveInterval:  pointer.Uint32(42),
						TransmitInterval: pointer.Uint32(43),
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
											Port:       179,
											BFDProfile: "bfd2",
										},
									},
								},
							},
							BFDProfiles: []v1beta1.BFDProfile{
								{
									Name:             "bfd1",
									ReceiveInterval:  42,
									TransmitInterval: 43,
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
											Port:       179,
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
									ReceiveInterval:  42,
									TransmitInterval: 43,
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
									ReceiveInterval:  42,
									TransmitInterval: 43,
								},
								{
									Name:             "bfd1",
									ReceiveInterval:  42,
									TransmitInterval: 43,
								},
								{
									Name:             "bfd3",
									ReceiveInterval:  42,
									TransmitInterval: 44,
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
									ReceiveInterval:  42,
									TransmitInterval: 43,
								},
								{
									Name:             "bfd2",
									ReceiveInterval:  42,
									TransmitInterval: 44,
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
									ReceiveInterval:  42,
									TransmitInterval: 43,
								},
								{
									Name:             "bfd3",
									ReceiveInterval:  42,
									TransmitInterval: 45,
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
						ReceiveInterval:  pointer.Uint32(42),
						TransmitInterval: pointer.Uint32(43),
					},
					{
						Name:             "bfd2",
						ReceiveInterval:  pointer.Uint32(42),
						TransmitInterval: pointer.Uint32(44),
					},
					{
						Name:             "bfd3",
						ReceiveInterval:  pointer.Uint32(42),
						TransmitInterval: pointer.Uint32(45),
					},
				},
			},
			err: nil,
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
											Port:    179,
											KeepaliveTime: metav1.Duration{
												Duration: 50 * time.Second,
											},
											HoldTime: metav1.Duration{
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resources := ClusterResources{
				FRRConfigs:      test.fromK8s,
				PasswordSecrets: test.secrets,
			}
			frr, err := apiToFRR(resources)
			if test.err != nil && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if test.err == nil && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if diff := cmp.Diff(frr, test.expected); diff != "" {
				t.Fatalf("config different from expected: %s", diff)
			}
		})
	}
}
