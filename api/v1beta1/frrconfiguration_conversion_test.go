/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/metallb/frr-k8s/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestConvertTo(t *testing.T) {
	tests := []struct {
		name     string
		src      *FRRConfiguration
		expected *v1beta2.FRRConfiguration
	}{
		{
			name: "empty configuration",
			src: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
			},
			expected: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
			},
		},
		{
			name: "with metadata labels and annotations",
			src: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Labels: map[string]string{
						"app": "frr",
					},
					Annotations: map[string]string{
						"key": "value",
					},
				},
			},
			expected: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Labels: map[string]string{
						"app": "frr",
					},
					Annotations: map[string]string{
						"key": "value",
					},
				},
			},
		},
		{
			name: "with BFD profiles",
			src: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					BGP: BGPConfig{
						BFDProfiles: []BFDProfile{
							{
								Name:             "profile1",
								ReceiveInterval:  ptr.To(uint32(300)),
								TransmitInterval: ptr.To(uint32(300)),
								DetectMultiplier: ptr.To(uint32(3)),
								EchoInterval:     ptr.To(uint32(50)),
								EchoMode:         ptr.To(true),
								PassiveMode:      ptr.To(false),
								MinimumTTL:       ptr.To(uint32(254)),
							},
						},
					},
				},
			},
			expected: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					BGP: v1beta2.BGPConfig{
						BFDProfiles: []v1beta2.BFDProfile{
							{
								Name:             "profile1",
								ReceiveInterval:  ptr.To(uint32(300)),
								TransmitInterval: ptr.To(uint32(300)),
								DetectMultiplier: ptr.To(uint32(3)),
								EchoInterval:     ptr.To(uint32(50)),
								EchoMode:         ptr.To(true),
								PassiveMode:      ptr.To(false),
								MinimumTTL:       ptr.To(uint32(254)),
							},
						},
					},
				},
			},
		},
		{
			name: "with routers and basic neighbors",
			src: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					BGP: BGPConfig{
						Routers: []Router{
							{
								ASN: 65000,
								ID:  "10.0.0.1",
								VRF: "vrf1",
								Neighbors: []Neighbor{
									{
										ASN:     65001,
										Address: "192.168.1.1",
										Port:    ptr.To(uint16(179)),
									},
								},
								Prefixes: []string{"10.0.0.0/24"},
							},
						},
					},
				},
			},
			expected: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					BGP: v1beta2.BGPConfig{
						Routers: []v1beta2.Router{
							{
								ASN: 65000,
								ID:  "10.0.0.1",
								VRF: "vrf1",
								Neighbors: []v1beta2.Neighbor{
									{
										ASN:     65001,
										Address: "192.168.1.1",
										Port:    ptr.To(uint16(179)),
									},
								},
								Prefixes: []string{"10.0.0.0/24"},
							},
						},
					},
				},
			},
		},
		{
			name: "with complete neighbor configuration",
			src: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					BGP: BGPConfig{
						Routers: []Router{
							{
								ASN: 65000,
								Neighbors: []Neighbor{
									{
										ASN:           65001,
										Address:       "192.168.1.1",
										DynamicASN:    InternalASNMode,
										SourceAddress: "192.168.1.2",
										Interface:     "eth0",
										Port:          ptr.To(uint16(179)),
										Password:      "secret",
										PasswordSecret: SecretReference{
											Name:      "bgp-secret",
											Namespace: "default",
										},
										HoldTime:               &metav1.Duration{Duration: 180000000000},
										KeepaliveTime:          &metav1.Duration{Duration: 60000000000},
										ConnectTime:            &metav1.Duration{Duration: 10000000000},
										EBGPMultiHop:           true,
										BFDProfile:             "profile1",
										EnableGracefulRestart:  true,
										DisableMP:              true,
										DualStackAddressFamily: true,
										ToAdvertise: Advertise{
											Allowed: AllowedOutPrefixes{
												Prefixes: []string{"10.0.0.0/24"},
												Mode:     AllowAll,
											},
											PrefixesWithLocalPref: []LocalPrefPrefixes{
												{
													Prefixes:  []string{"10.0.1.0/24"},
													LocalPref: 100,
												},
											},
											PrefixesWithCommunity: []CommunityPrefixes{
												{
													Prefixes:  []string{"10.0.2.0/24"},
													Community: "65000:100",
												},
											},
										},
										ToReceive: Receive{
											Allowed: AllowedInPrefixes{
												Prefixes: []PrefixSelector{
													{
														Prefix: "192.168.0.0/16",
														LE:     24,
														GE:     20,
													},
												},
												Mode: AllowRestricted,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					BGP: v1beta2.BGPConfig{
						Routers: []v1beta2.Router{
							{
								ASN: 65000,
								Neighbors: []v1beta2.Neighbor{
									{
										ASN:           65001,
										Address:       "192.168.1.1",
										DynamicASN:    v1beta2.InternalASNMode,
										SourceAddress: "192.168.1.2",
										Interface:     "eth0",
										Port:          ptr.To(uint16(179)),
										Password:      "secret",
										PasswordSecret: v1beta2.SecretReference{
											Name:      "bgp-secret",
											Namespace: "default",
										},
										HoldTime:               &metav1.Duration{Duration: 180000000000},
										KeepaliveTime:          &metav1.Duration{Duration: 60000000000},
										ConnectTime:            &metav1.Duration{Duration: 10000000000},
										EBGPMultiHop:           true,
										BFDProfile:             "profile1",
										EnableGracefulRestart:  true,
										DisableMP:              true,
										DualStackAddressFamily: true,
										ToAdvertise: v1beta2.Advertise{
											Allowed: v1beta2.AllowedOutPrefixes{
												Prefixes: []string{"10.0.0.0/24"},
												Mode:     v1beta2.AllowAll,
											},
											PrefixesWithLocalPref: []v1beta2.LocalPrefPrefixes{
												{
													Prefixes:  []string{"10.0.1.0/24"},
													LocalPref: 100,
												},
											},
											PrefixesWithCommunity: []v1beta2.CommunityPrefixes{
												{
													Prefixes:  []string{"10.0.2.0/24"},
													Community: "65000:100",
												},
											},
										},
										ToReceive: v1beta2.Receive{
											Allowed: v1beta2.AllowedInPrefixes{
												Prefixes: []v1beta2.PrefixSelector{
													{
														Prefix: "192.168.0.0/16",
														LE:     24,
														GE:     20,
													},
												},
												Mode: v1beta2.AllowRestricted,
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
		{
			name: "with VRF imports",
			src: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					BGP: BGPConfig{
						Routers: []Router{
							{
								ASN: 65000,
								VRF: "red",
								Imports: []Import{
									{VRF: "blue"},
									{VRF: "green"},
								},
							},
						},
					},
				},
			},
			expected: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					BGP: v1beta2.BGPConfig{
						Routers: []v1beta2.Router{
							{
								ASN: 65000,
								VRF: "red",
								Imports: []v1beta2.Import{
									{VRF: "blue"},
									{VRF: "green"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with node selector",
			src: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role": "router",
						},
					},
				},
			},
			expected: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role": "router",
						},
					},
				},
			},
		},
		{
			name: "with raw config",
			src: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					Raw: RawConfig{
						Priority: 100,
						Config:   "raw frr config",
					},
				},
			},
			expected: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					Raw: v1beta2.RawConfig{
						Priority: 100,
						Config:   "raw frr config",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := &v1beta2.FRRConfiguration{}
			err := tt.src.ConvertTo(dst)
			if err != nil {
				t.Fatalf("ConvertTo() failed: %v", err)
			}

			if diff := cmp.Diff(tt.expected, dst); diff != "" {
				t.Errorf("ConvertTo() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertFrom(t *testing.T) {
	tests := []struct {
		name     string
		src      *v1beta2.FRRConfiguration
		expected *FRRConfiguration
	}{
		{
			name: "empty configuration",
			src: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
			},
			expected: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
			},
		},
		{
			name: "with metadata labels and annotations",
			src: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Labels: map[string]string{
						"app": "frr",
					},
					Annotations: map[string]string{
						"key": "value",
					},
				},
			},
			expected: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Labels: map[string]string{
						"app": "frr",
					},
					Annotations: map[string]string{
						"key": "value",
					},
				},
			},
		},
		{
			name: "with BFD profiles",
			src: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					BGP: v1beta2.BGPConfig{
						BFDProfiles: []v1beta2.BFDProfile{
							{
								Name:             "profile1",
								ReceiveInterval:  ptr.To(uint32(300)),
								TransmitInterval: ptr.To(uint32(300)),
								DetectMultiplier: ptr.To(uint32(3)),
								EchoInterval:     ptr.To(uint32(50)),
								EchoMode:         ptr.To(true),
								PassiveMode:      ptr.To(false),
								MinimumTTL:       ptr.To(uint32(254)),
							},
						},
					},
				},
			},
			expected: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					BGP: BGPConfig{
						BFDProfiles: []BFDProfile{
							{
								Name:             "profile1",
								ReceiveInterval:  ptr.To(uint32(300)),
								TransmitInterval: ptr.To(uint32(300)),
								DetectMultiplier: ptr.To(uint32(3)),
								EchoInterval:     ptr.To(uint32(50)),
								EchoMode:         ptr.To(true),
								PassiveMode:      ptr.To(false),
								MinimumTTL:       ptr.To(uint32(254)),
							},
						},
					},
				},
			},
		},
		{
			name: "with routers and basic neighbors",
			src: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					BGP: v1beta2.BGPConfig{
						Routers: []v1beta2.Router{
							{
								ASN: 65000,
								ID:  "10.0.0.1",
								VRF: "vrf1",
								Neighbors: []v1beta2.Neighbor{
									{
										ASN:     65001,
										Address: "192.168.1.1",
										Port:    ptr.To(uint16(179)),
									},
								},
								Prefixes: []string{"10.0.0.0/24"},
							},
						},
					},
				},
			},
			expected: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					BGP: BGPConfig{
						Routers: []Router{
							{
								ASN: 65000,
								ID:  "10.0.0.1",
								VRF: "vrf1",
								Neighbors: []Neighbor{
									{
										ASN:     65001,
										Address: "192.168.1.1",
										Port:    ptr.To(uint16(179)),
									},
								},
								Prefixes: []string{"10.0.0.0/24"},
							},
						},
					},
				},
			},
		},
		{
			name: "with complete neighbor configuration",
			src: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					BGP: v1beta2.BGPConfig{
						Routers: []v1beta2.Router{
							{
								ASN: 65000,
								Neighbors: []v1beta2.Neighbor{
									{
										ASN:           65001,
										Address:       "192.168.1.1",
										DynamicASN:    v1beta2.ExternalASNMode,
										SourceAddress: "192.168.1.2",
										Interface:     "eth0",
										Port:          ptr.To(uint16(179)),
										Password:      "secret",
										PasswordSecret: v1beta2.SecretReference{
											Name:      "bgp-secret",
											Namespace: "default",
										},
										HoldTime:               &metav1.Duration{Duration: 180000000000},
										KeepaliveTime:          &metav1.Duration{Duration: 60000000000},
										ConnectTime:            &metav1.Duration{Duration: 10000000000},
										EBGPMultiHop:           true,
										BFDProfile:             "profile1",
										EnableGracefulRestart:  true,
										DisableMP:              true,
										DualStackAddressFamily: true,
										ToAdvertise: v1beta2.Advertise{
											Allowed: v1beta2.AllowedOutPrefixes{
												Prefixes: []string{"10.0.0.0/24"},
												Mode:     v1beta2.AllowAll,
											},
											PrefixesWithLocalPref: []v1beta2.LocalPrefPrefixes{
												{
													Prefixes:  []string{"10.0.1.0/24"},
													LocalPref: 100,
												},
											},
											PrefixesWithCommunity: []v1beta2.CommunityPrefixes{
												{
													Prefixes:  []string{"10.0.2.0/24"},
													Community: "65000:100",
												},
											},
										},
										ToReceive: v1beta2.Receive{
											Allowed: v1beta2.AllowedInPrefixes{
												Prefixes: []v1beta2.PrefixSelector{
													{
														Prefix: "192.168.0.0/16",
														LE:     24,
														GE:     20,
													},
												},
												Mode: v1beta2.AllowRestricted,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					BGP: BGPConfig{
						Routers: []Router{
							{
								ASN: 65000,
								Neighbors: []Neighbor{
									{
										ASN:           65001,
										Address:       "192.168.1.1",
										DynamicASN:    ExternalASNMode,
										SourceAddress: "192.168.1.2",
										Interface:     "eth0",
										Port:          ptr.To(uint16(179)),
										Password:      "secret",
										PasswordSecret: SecretReference{
											Name:      "bgp-secret",
											Namespace: "default",
										},
										HoldTime:               &metav1.Duration{Duration: 180000000000},
										KeepaliveTime:          &metav1.Duration{Duration: 60000000000},
										ConnectTime:            &metav1.Duration{Duration: 10000000000},
										EBGPMultiHop:           true,
										BFDProfile:             "profile1",
										EnableGracefulRestart:  true,
										DisableMP:              true,
										DualStackAddressFamily: true,
										ToAdvertise: Advertise{
											Allowed: AllowedOutPrefixes{
												Prefixes: []string{"10.0.0.0/24"},
												Mode:     AllowAll,
											},
											PrefixesWithLocalPref: []LocalPrefPrefixes{
												{
													Prefixes:  []string{"10.0.1.0/24"},
													LocalPref: 100,
												},
											},
											PrefixesWithCommunity: []CommunityPrefixes{
												{
													Prefixes:  []string{"10.0.2.0/24"},
													Community: "65000:100",
												},
											},
										},
										ToReceive: Receive{
											Allowed: AllowedInPrefixes{
												Prefixes: []PrefixSelector{
													{
														Prefix: "192.168.0.0/16",
														LE:     24,
														GE:     20,
													},
												},
												Mode: AllowRestricted,
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
		{
			name: "with VRF imports",
			src: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					BGP: v1beta2.BGPConfig{
						Routers: []v1beta2.Router{
							{
								ASN: 65000,
								VRF: "red",
								Imports: []v1beta2.Import{
									{VRF: "blue"},
									{VRF: "green"},
								},
							},
						},
					},
				},
			},
			expected: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					BGP: BGPConfig{
						Routers: []Router{
							{
								ASN: 65000,
								VRF: "red",
								Imports: []Import{
									{VRF: "blue"},
									{VRF: "green"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with node selector",
			src: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role": "router",
						},
					},
				},
			},
			expected: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role": "router",
						},
					},
				},
			},
		},
		{
			name: "with raw config",
			src: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					Raw: v1beta2.RawConfig{
						Priority: 100,
						Config:   "raw frr config",
					},
				},
			},
			expected: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: FRRConfigurationSpec{
					Raw: RawConfig{
						Priority: 100,
						Config:   "raw frr config",
					},
				},
			},
		},
		{
			name: "v1beta2 with LogLevel field (should be ignored in v1beta1)",
			src: &v1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1beta2.FRRConfigurationSpec{
					LogLevel: "debug",
				},
			},
			expected: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := &FRRConfiguration{}
			err := dst.ConvertFrom(tt.src)
			if err != nil {
				t.Fatalf("ConvertFrom() failed: %v", err)
			}

			if diff := cmp.Diff(tt.expected, dst); diff != "" {
				t.Errorf("ConvertFrom() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRoundTripConversion(t *testing.T) {
	tests := []struct {
		name     string
		original *FRRConfiguration
	}{
		{
			name: "empty configuration",
			original: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
			},
		},
		{
			name: "full configuration",
			original: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Labels: map[string]string{
						"app": "frr",
					},
				},
				Spec: FRRConfigurationSpec{
					BGP: BGPConfig{
						BFDProfiles: []BFDProfile{
							{
								Name:             "profile1",
								ReceiveInterval:  ptr.To(uint32(300)),
								TransmitInterval: ptr.To(uint32(300)),
								DetectMultiplier: ptr.To(uint32(3)),
							},
						},
						Routers: []Router{
							{
								ASN: 65000,
								ID:  "10.0.0.1",
								Neighbors: []Neighbor{
									{
										ASN:     65001,
										Address: "192.168.1.1",
										ToAdvertise: Advertise{
											Allowed: AllowedOutPrefixes{
												Prefixes: []string{"10.0.0.0/24"},
												Mode:     AllowAll,
											},
										},
										ToReceive: Receive{
											Allowed: AllowedInPrefixes{
												Prefixes: []PrefixSelector{
													{
														Prefix: "192.168.0.0/16",
														LE:     24,
													},
												},
											},
										},
									},
								},
								Imports: []Import{
									{VRF: "blue"},
								},
							},
						},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role": "router",
						},
					},
					Raw: RawConfig{
						Priority: 100,
						Config:   "raw config",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// v1beta1 -> v1beta2
			v1beta2Config := &v1beta2.FRRConfiguration{}
			err := tt.original.ConvertTo(v1beta2Config)
			if err != nil {
				t.Fatalf("ConvertTo() failed: %v", err)
			}

			// v1beta2 -> v1beta1
			roundTripped := &FRRConfiguration{}
			err = roundTripped.ConvertFrom(v1beta2Config)
			if err != nil {
				t.Fatalf("ConvertFrom() failed: %v", err)
			}

			// Compare
			if diff := cmp.Diff(tt.original, roundTripped); diff != "" {
				t.Errorf("Round trip conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
