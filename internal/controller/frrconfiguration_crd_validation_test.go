/*
Copyright 2023.

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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/utils/ptr"
)

var _ = Describe("CRD validation", func() {
	AfterEach(func() {
		toDel := &v1beta1.FRRConfiguration{}
		err := k8sClient.DeleteAllOf(context.Background(), toDel, client.InNamespace("default"))
		if apierrors.IsNotFound(err) {
			return
		}
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() int {
			frrConfigList := &v1beta1.FRRConfigurationList{}
			err := k8sClient.List(context.Background(), frrConfigList)
			Expect(err).ToNot(HaveOccurred())
			return len(frrConfigList.Items)
		}).Should(Equal(0))
	})

	Context("EVPN fields", func() {
		DescribeTable("should reject invalid configurations",
			func(frrConfig *v1beta1.FRRConfiguration) {
				err := k8sClient.Create(context.Background(), frrConfig)
				Expect(err).To(HaveOccurred())
				Expect(apierrors.IsInvalid(err) || apierrors.IsForbidden(err)).To(BeTrue(), "expected validation error, got: %v", err)
			},
			Entry("VNI out of range (0)", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 0}},
							},
						}},
					},
				},
			}),
			Entry("invalid RD format (missing colon)", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{RD: "invalid"}}},
							},
						}},
					},
				},
			}),
			Entry("invalid RD format (non-numeric local admin)", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{RD: "65000:abc"}}},
							},
						}},
					},
				},
			}),
			Entry("invalid RD format (missing colon)", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{RD: "nocolon"}}},
							},
						}},
					},
				},
			}),
			Entry("invalid RD non-numeric global admin", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{RD: "abc:100"}}},
							},
						}},
					},
				},
			}),
			Entry("invalid RD 4-byte ASN with local admin overflow", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{RD: "70000:70000"}}},
							},
						}},
					},
				},
			}),
			Entry("invalid RD IPv4 with local admin overflow", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{RD: "192.0.2.1:70000"}}},
							},
						}},
					},
				},
			}),
			Entry("invalid ExportRT with wildcard", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ExportRTs: []v1beta1.ExportRouteTarget{"*:100"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("invalid ExportRT format (missing colon)", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ExportRTs: []v1beta1.ExportRouteTarget{"nocolon"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("invalid ImportRT format (missing colon)", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ImportRTs: []v1beta1.ImportRouteTarget{"nocolon"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("invalid ImportRT non-numeric local admin", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ImportRTs: []v1beta1.ImportRouteTarget{"65000:abc"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("invalid ExportRT non-numeric local admin", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ExportRTs: []v1beta1.ExportRouteTarget{"65000:abc"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("invalid ImportRT non-numeric global admin", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ImportRTs: []v1beta1.ImportRouteTarget{"abc:100"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("invalid ExportRT non-numeric global admin", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ExportRTs: []v1beta1.ExportRouteTarget{"abc:100"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("invalid ImportRT wildcard with non-numeric local admin", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ImportRTs: []v1beta1.ImportRouteTarget{"*:abc"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("invalid RT 4-byte ASN with local admin overflow", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ImportRTs: []v1beta1.ImportRouteTarget{"70000:70000"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("invalid RT IPv4 with local admin overflow", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ExportRTs: []v1beta1.ExportRouteTarget{"192.0.2.1:70000"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("invalid AdvertiseVNIs enum value", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								AdvertiseVNIs: ptr.To(v1beta1.VNIAdvertisement("SomeOther")),
							},
						}},
					},
				},
			}),
			Entry("invalid AddressFamily enum value", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							Neighbors: []v1beta1.Neighbor{{
								ASN:             65001,
								Address:         "192.0.2.1",
								AddressFamilies: []v1beta1.AddressFamily{"l2vpn"},
							}},
						}},
					},
				},
			}),
			Entry("invalid AdvertisePrefixes enum value", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							VRF: "red",
							EVPN: &v1beta1.EVPNConfig{
								L3VNI: &v1beta1.L3VNI{
									VNI:               500,
									AdvertisePrefixes: []v1beta1.AdvertisePrefixType{"evpn"},
								},
							},
						}},
					},
				},
			}),
			Entry("L3VNI missing required AdvertisePrefixes", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							VRF: "red",
							EVPN: &v1beta1.EVPNConfig{
								L3VNI: &v1beta1.L3VNI{
									VNI: 500,
								},
							},
						}},
					},
				},
			}),
		)

		DescribeTable("should accept valid configurations",
			func(frrConfig *v1beta1.FRRConfiguration) {
				err := k8sClient.Create(context.Background(), frrConfig)
				Expect(err).ToNot(HaveOccurred())
			},
			Entry("ImportRT with wildcard", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-valid-wildcard", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ImportRTs: []v1beta1.ImportRouteTarget{"*:100"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("RD with 4-byte ASN", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-valid-rd-4byte", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									RD: "4294967295:100",
								}}},
							},
						}},
					},
				},
			}),
			Entry("RD with 2-byte ASN and large local admin", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-valid-rd-2byte", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									RD: "100:4294967295",
								}}},
							},
						}},
					},
				},
			}),
			Entry("RD with IPv4 global admin", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-valid-iprd", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									RD: "192.0.2.1:100",
								}}},
							},
						}},
					},
				},
			}),
			Entry("L2VNI with full config", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-valid-full", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								AdvertiseVNIs: ptr.To(v1beta1.VNIAdvertisementAll),
								L2VNIs: []v1beta1.L2VNI{{VNI: 1000, VNIProperties: v1beta1.VNIProperties{
									RD:        "65000:1000",
									ImportRTs: []v1beta1.ImportRouteTarget{"65000:1000"},
									ExportRTs: []v1beta1.ExportRouteTarget{"65000:1000"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("ImportRT with 4-byte ASN", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-valid-4byte-asn", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ImportRTs: []v1beta1.ImportRouteTarget{"4294967295:100"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("ExportRT with 2-byte ASN and large local admin", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-valid-2byte-large", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ExportRTs: []v1beta1.ExportRouteTarget{"100:4294967295"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("ImportRT with IPv4 global admin", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-valid-iprt", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ImportRTs: []v1beta1.ImportRouteTarget{"192.0.2.1:100"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("ExportRT with IPv4 global admin", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-valid-iprt-export", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							EVPN: &v1beta1.EVPNConfig{
								L2VNIs: []v1beta1.L2VNI{{VNI: 100, VNIProperties: v1beta1.VNIProperties{
									ExportRTs: []v1beta1.ExportRouteTarget{"10.0.0.1:65535"},
								}}},
							},
						}},
					},
				},
			}),
			Entry("neighbor with evpn address family", &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Name: "test-evpn-valid-af", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{{
							ASN: 65000,
							Neighbors: []v1beta1.Neighbor{{
								ASN:             65001,
								Address:         "192.0.2.1",
								AddressFamilies: []v1beta1.AddressFamily{"unicast", "evpn"},
							}},
						}},
					},
				},
			}),
		)
	})
})
