// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = ginkgo.Describe("Webhooks", func() {
	var cs clientset.Interface

	defer ginkgo.GinkgoRecover()
	updater, err := config.NewUpdater()
	Expect(err).NotTo(HaveOccurred())
	reporter := dump.NewK8sReporter(k8s.FRRK8sNamespace)

	ginkgo.AfterEach(func() {
		if ginkgo.CurrentSpecReport().Failed() {
			testName := ginkgo.CurrentSpecReport().LeafNodeText
			dump.K8sInfo(testName, reporter)
			dump.BGPInfo(testName, infra.FRRContainers, cs)
		}
	})

	ginkgo.BeforeEach(func() {
		ginkgo.By("Clearing any previous configuration")

		for _, c := range infra.FRRContainers {
			err := c.UpdateBGPConfigFile(frrconfig.Empty)
			Expect(err).NotTo(HaveOccurred())
		}
		err := updater.Clean()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
	})

	ginkgo.Context("FRRConfiguration", func() {
		ginkgo.DescribeTable("Should reject create", func(modify func(*frrk8sv1beta1.FRRConfiguration), errStr string) {
			cfg := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-test",
					Namespace: k8s.FRRK8sNamespace,
				},
			}
			modify(&cfg)

			err := updater.Update([]corev1.Secret{}, cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(errStr))
		},
			ginkgo.Entry("invalid nodeSelector",
				func(cfg *frrk8sv1beta1.FRRConfiguration) {
					cfg.Spec.NodeSelector = metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "@",
						},
					}
				},
				"invalid NodeSelector",
			),
			ginkgo.Entry("invalid router prefix",
				func(cfg *frrk8sv1beta1.FRRConfiguration) {
					cfg.Spec.BGP.Routers = []frrk8sv1beta1.Router{
						{
							Prefixes: []string{"192.a.b.10"},
						},
					}
				},
				"unknown ipfamily",
			),
			ginkgo.Entry("invalid neighbor address",
				func(cfg *frrk8sv1beta1.FRRConfiguration) {
					cfg.Spec.BGP.Routers = []frrk8sv1beta1.Router{
						{
							ASN: 100,
							Neighbors: []frrk8sv1beta1.Neighbor{
								{
									ASN:     100,
									Address: "192.a.b.10",
								},
							},
						},
					}
				},
				"failed to find ipfamily for",
			),
			ginkgo.Entry("localpref on non-existing prefix",
				func(cfg *frrk8sv1beta1.FRRConfiguration) {
					cfg.Spec.BGP.Routers = []frrk8sv1beta1.Router{
						{
							Neighbors: []frrk8sv1beta1.Neighbor{
								{
									ASN:     100,
									Address: "1.2.3.4",
									ToAdvertise: frrk8sv1beta1.Advertise{
										PrefixesWithLocalPref: []frrk8sv1beta1.LocalPrefPrefixes{
											{
												LocalPref: 200,
												Prefixes:  []string{"10.10.10.10"},
											},
										},
									},
								},
							},
						},
					}
				},
				"localPref associated to non existing prefix",
			),
			ginkgo.Entry("both asn and dynamicASN not specified",
				func(cfg *frrk8sv1beta1.FRRConfiguration) {
					cfg.Spec.BGP.Routers = []frrk8sv1beta1.Router{
						{
							Neighbors: []frrk8sv1beta1.Neighbor{
								{
									Address: "1.2.3.4",
								},
							},
						},
					}
				},
				"has no ASN or DynamicASN specified",
			),
			ginkgo.Entry("both asn and dynamicASN specified",
				func(cfg *frrk8sv1beta1.FRRConfiguration) {
					cfg.Spec.BGP.Routers = []frrk8sv1beta1.Router{
						{
							Neighbors: []frrk8sv1beta1.Neighbor{
								{
									ASN:        100,
									DynamicASN: frrk8sv1beta1.ExternalASNMode,
									Address:    "1.2.3.4",
								},
							},
						},
					}
				},
				"has both ASN and DynamicASN specified",
			),
			ginkgo.Entry("no address and no interface is specified",
				func(cfg *frrk8sv1beta1.FRRConfiguration) {
					cfg.Spec.BGP.Routers = []frrk8sv1beta1.Router{
						{
							Neighbors: []frrk8sv1beta1.Neighbor{
								{
									ASN: 100,
								},
							},
						},
					}
				},
				"has no Address and Interface specified",
			),
			ginkgo.Entry("both address and interface specified",
				func(cfg *frrk8sv1beta1.FRRConfiguration) {
					cfg.Spec.BGP.Routers = []frrk8sv1beta1.Router{
						{
							Neighbors: []frrk8sv1beta1.Neighbor{
								{
									ASN:       100,
									Address:   "1.2.3.4",
									Interface: "eth0",
								},
							},
						},
					}
				},
				"has both Address and Interface specified",
			),
		)

		ginkgo.It("Should reject create/update when there is a conflict with an existing config", func() {
			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Creating the first config on the first node")
			cfg1 := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-cfg1",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN: 100,
								VRF: "",
							},
						},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/hostname": nodes[0].GetLabels()["kubernetes.io/hostname"],
						},
					},
				},
			}
			err = updater.Update([]corev1.Secret{}, cfg1)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Attempting to create a second config on the first node with a different ASN")
			cfg2 := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-cfg2",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN: 200,
								VRF: "",
							},
						},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/hostname": nodes[0].GetLabels()["kubernetes.io/hostname"],
						},
					},
				},
			}
			err = updater.Update([]corev1.Secret{}, cfg2)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("different asns"))

			ginkgo.By("Creating the second config on the second node")
			cfg2.Spec.NodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/hostname": nodes[1].GetLabels()["kubernetes.io/hostname"],
				},
			}
			err = updater.Update([]corev1.Secret{}, cfg2)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Attempting to update the second config to select the first node")
			cfg2.Spec.NodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/hostname": nodes[0].GetLabels()["kubernetes.io/hostname"],
				},
			}
			err = updater.Update([]corev1.Secret{}, cfg2)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("different asns"))

			ginkgo.By("Attempting to create a third config in a different namespace on the first node with a different ASN")
			cfg3 := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-cfg3",
					Namespace: "default",
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN: 200,
								VRF: "",
							},
						},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/hostname": nodes[0].GetLabels()["kubernetes.io/hostname"],
						},
					},
				},
			}
			err = updater.Update([]corev1.Secret{}, cfg3)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("different asns"))
		})

		ginkgo.It("Should not reject a resource with a missing secret ref", func() {
			cfg := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN: 100,
								Neighbors: []frrk8sv1beta1.Neighbor{
									{
										ASN:     100,
										Address: "1.2.3.4",
										PasswordSecret: frrk8sv1beta1.SecretReference{
											Name:      "nonexisting",
											Namespace: k8s.FRRK8sNamespace,
										},
									},
								},
							},
						},
					},
				},
			}

			err := updater.Update([]corev1.Secret{}, cfg)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
