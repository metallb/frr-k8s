// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/frr"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	. "github.com/onsi/gomega"
	"go.universe.tf/e2etest/pkg/executor"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

const (
	evpnFRRContainerName = "ibgp-single-hop"
	evpnL2VNI            = 1000
	evpnL2VLANID         = 100
	evpnL3VNI            = 3000
	evpnL3VLANID         = 4000
	evpnL3VRF            = "evpnred"
	evpnBridge           = "evpnbr"
	evpnVxlan            = "evpnvx"
	evpnL3VRFTable       = 10
	evpnL2Iface          = "evpnl2-100"
)

var evpnL2ConfigTemplate = infra.EVPNConfig{
	L2VNI:     evpnL2VNI,
	L2VLANID:  evpnL2VLANID,
	L2IPFmt:   "10.100.0.%d/24",
	L2IPv6Fmt: "fc00:100::%d/64",
	Bridge:    evpnBridge,
	Vxlan:     evpnVxlan,
}

var evpnL3ConfigTemplate = infra.EVPNConfig{
	L3VNI:           evpnL3VNI,
	L3VLANID:        evpnL3VLANID,
	L3VRF:           evpnL3VRF,
	L3VRFTable:      evpnL3VRFTable,
	L3PrefixFmt:     "10.200.%d.1/24",
	L3IPv6PrefixFmt: "fc00:200:%d::1/64",
	Bridge:          evpnBridge,
	Vxlan:           evpnVxlan,
}

var _ = ginkgo.DescribeTableSubtree("EVPN",
	func(peerFamily ipfamily.Family, advertiseFamily ipfamily.Family) {
		var (
			cs               clientset.Interface
			nodes            []corev1.Node
			evpnCfg          infra.EVPNConfig
			evpnInfra        *infra.EVPN
			evpnFRRContainer *frrcontainer.FRR
		)

		defer ginkgo.GinkgoRecover()
		updater, err := config.NewUpdater()
		Expect(err).NotTo(HaveOccurred())
		reporter := dump.NewK8sReporter(k8s.FRRK8sNamespace)

		ginkgo.BeforeEach(func() {
			ginkgo.By("Clearing any previous configuration")
			for _, c := range infra.FRRContainers {
				err := c.UpdateConfigFile(frrconfig.Empty)
				Expect(err).NotTo(HaveOccurred())
			}
			err := updater.Clean()
			Expect(err).NotTo(HaveOccurred())

			// Workaround for https://github.com/FRRouting/frr/issues/22060:
			// frr-reload.py crashes when a config transition both removes route-maps
			// and adds a new address-family. Wait for the clean config to be applied
			// before the EVPN config, avoiding the two being merged by the debouncer.
			time.Sleep(5 * time.Second)

			evpnFRRContainer = infra.FindContainer(evpnFRRContainerName)
			Expect(evpnFRRContainer).NotTo(BeNil(), "%s container not found in FRRContainers", evpnFRRContainerName)

			cs = k8sclient.New()
			nodes, err = k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			if ginkgo.CurrentSpecReport().Failed() {
				testName := ginkgo.CurrentSpecReport().LeafNodeText
				dump.K8sInfo(testName, reporter)
				dump.BGPInfo(testName, infra.FRRContainers, cs)
			}
			err := infra.CleanupEVPN(cs, evpnFRRContainer, evpnCfg)
			Expect(err).NotTo(HaveOccurred())
		})

		setupEVPN := func(tmpl infra.EVPNConfig) config.PeersConfig {
			ginkgo.GinkgoHelper()

			evpnCfg = tmpl
			switch advertiseFamily {
			case ipfamily.IPv4:
				evpnCfg.L2IPv6Fmt = ""
				evpnCfg.L3IPv6PrefixFmt = ""
			case ipfamily.IPv6:
				evpnCfg.L2IPFmt = ""
				evpnCfg.L3PrefixFmt = ""
			}

			ginkgo.By("Setting up EVPN networking infrastructure")
			var err error
			evpnInfra, err = infra.SetupEVPN(cs, evpnFRRContainer, evpnCfg)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Pairing external FRR with nodes (EVPN)")
			evpnCfg := frr.EVPNConfig{
				L2VNI:           evpnCfg.L2VNI,
				L3VNI:           evpnCfg.L3VNI,
				L3VRF:           evpnCfg.L3VRF,
				AdvertiseFamily: advertiseFamily,
			}
			err = frr.PairWithNodesForEVPN(evpnFRRContainer, cs, evpnCfg, peerFamily)
			Expect(err).NotTo(HaveOccurred())
			return config.PeersForContainers([]*frrcontainer.FRR{evpnFRRContainer}, peerFamily)
		}

		neighborsWithEVPN := func(peersConfig config.PeersConfig) []frrk8sv1beta1.Neighbor {
			neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)
			for i := range neighbors {
				neighbors[i].AddressFamilies = []frrk8sv1beta1.AddressFamily{"unicast", "evpn"}
			}
			return neighbors
		}

		// validateL2VNI validates L2 VNI EVPN state: sessions, VNI visibility,
		// type-2 routes, and data path connectivity.
		validateL2VNI := func() {
			ginkgo.GinkgoHelper()
			ginkgo.By("Validating BGP sessions are established")
			ValidateFRRPeeredWithNodes(nodes, evpnFRRContainer, peerFamily)

			ginkgo.By("Validating EVPN address family is active on frr-k8s pods")
			pods, err := k8s.FRRK8sPods(cs)
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range pods {
				for _, peerIP := range evpnFRRContainer.AddressesForFamily(peerFamily) {
					Eventually(func() error {
						return frr.ForPod(pod).HasEVPNNeighbor(peerIP)
					}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
						"EVPN address family not active on pod %s for peer %s", pod.Name, peerIP)
				}
			}

			ginkgo.By("Validating L2 VNI is visible in EVPN state")
			for _, pod := range pods {
				Eventually(func() error {
					return frr.ForPod(pod).HasEVPNVNI(evpnCfg.L2VNI)
				}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
					"L2 VNI %d not visible on pod %s", evpnCfg.L2VNI, pod.Name)
			}

			ginkgo.By("Collecting expected type-2 routes")
			expectedType2, err := expectedL2VNIRoutes(pods)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Validating EVPN type-2 routes are exchanged")
			Eventually(func() error {
				return frr.ForContainer(evpnFRRContainer).HasEVPNType2Routes(expectedType2)
			}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
				"Type-2 routes not found on external FRR")

			targetIPs := map[string]string{}
			for _, node := range nodes {
				for _, ip := range evpnInfra.L2IPForFamily(node.Name, advertiseFamily) {
					targetIPs[ip] = node.Name
				}
			}

			ginkgo.By("Validating L2 data path from external FRR to nodes")
			for targetIP, node := range targetIPs {
				Eventually(func() error {
					return ping(evpnFRRContainer, targetIP)
				}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
					"ping from external FRR to node %s (%s) failed", node, targetIP, evpnCfg.L3VRF)
			}
		}

		// validateL3VNI validates L3 VNI EVPN state: sessions, type-5 routes,
		// and data path connectivity.
		validateL3VNI := func() {
			ginkgo.GinkgoHelper()
			ginkgo.By("Validating BGP sessions are established")
			ValidateFRRPeeredWithNodes(nodes, evpnFRRContainer, peerFamily)

			ginkgo.By("Validating EVPN address family is active on frr-k8s pods")
			pods, err := k8s.FRRK8sPods(cs)
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range pods {
				for _, peerIP := range evpnFRRContainer.AddressesForFamily(peerFamily) {
					Eventually(func() error {
						return frr.ForPod(pod).HasEVPNNeighbor(peerIP)
					}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
						"EVPN address family not active on pod %s for peer %s", pod.Name, peerIP)
				}
			}

			expectedRoutes := map[string]string{}
			targetIPs := map[string]string{}
			for _, node := range nodes {
				// only IPv4 supported as next hop VTEP for now
				nodeIP, err := k8s.NodeIPForFamily(node, ipfamily.IPv4)
				Expect(err).NotTo(HaveOccurred())
				for _, prefix := range evpnInfra.L3PrefixForFamily(node.Name, advertiseFamily) {
					expectedRoutes[prefix] = nodeIP
				}
				for _, ip := range evpnInfra.L3IPForFamily(node.Name, advertiseFamily) {
					targetIPs[ip] = node.Name
				}
			}

			ginkgo.By("Validating EVPN type-5 routes are received on external FRR")
			Eventually(func() error {
				return frr.ForContainer(evpnFRRContainer).HasEVPNType5Routes(expectedRoutes)
			}, 30*time.Second, time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Validating L3 data path from external FRR to nodes via VRF")
			for targetIP, node := range targetIPs {
				Eventually(func() error {
					return pingVRF(evpnFRRContainer, evpnCfg.L3VRF, targetIP)
				}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
					"ping from external FRR to node %s (%s) via VRF %s failed", node, targetIP, evpnCfg.L3VRF)
			}
		}

		ginkgo.Context("L2 VNI", func() {
			ginkgo.It("should establish EVPN session and exchange L2 VNI routes", func() {
				peersConfig := setupEVPN(evpnL2ConfigTemplate)

				ginkgo.By("Building FRRConfiguration with EVPN L2 VNI")
				neighbors := neighborsWithEVPN(peersConfig)

				frrCfg := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-evpn-l2",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRConfigurationSpec{
						BGP: frrk8sv1beta1.BGPConfig{
							Routers: []frrk8sv1beta1.Router{
								{
									ASN:       infra.FRRK8sASN,
									Neighbors: neighbors,
									EVPN: &frrk8sv1beta1.EVPNConfig{
										AdvertiseVNIs: ptr.To(frrk8sv1beta1.VNIAdvertisementAll),
										L2VNIs: []frrk8sv1beta1.L2VNI{
											{
												VNI: evpnCfg.L2VNI,
												VNIProperties: frrk8sv1beta1.VNIProperties{
													RD: frrk8sv1beta1.RouteDistinguisher(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnCfg.L2VNI)),
													ImportRTs: []frrk8sv1beta1.ImportRouteTarget{
														frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnCfg.L2VNI)),
														frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", evpnFRRContainer.RouterConfig.ASN, evpnCfg.L2VNI)),
													},
													ExportRTs: []frrk8sv1beta1.ExportRouteTarget{frrk8sv1beta1.ExportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnCfg.L2VNI))},
												},
											},
										},
									},
								},
							},
						},
					},
				}

				ginkgo.By("Applying FRRConfiguration")
				err = updater.Update(peersConfig.Secrets, frrCfg)
				Expect(err).NotTo(HaveOccurred())

				validateL2VNI()
			})

			ginkgo.It("should work with L2 VNI config split across multiple FRRConfigurations", func() {
				peersConfig := setupEVPN(evpnL2ConfigTemplate)

				ginkgo.By("Building split FRRConfigurations: one with local ASN RT, one with external ASN RT")
				neighbors := neighborsWithEVPN(peersConfig)

				// First config: neighbors, advertiseVNIs, and L2VNI with local ASN import RT.
				cfgLocal := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-evpn-l2-local",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRConfigurationSpec{
						BGP: frrk8sv1beta1.BGPConfig{
							Routers: []frrk8sv1beta1.Router{
								{
									ASN:       infra.FRRK8sASN,
									Neighbors: neighbors,
									EVPN: &frrk8sv1beta1.EVPNConfig{
										AdvertiseVNIs: ptr.To(frrk8sv1beta1.VNIAdvertisementAll),
										L2VNIs: []frrk8sv1beta1.L2VNI{
											{
												VNI: evpnCfg.L2VNI,
												VNIProperties: frrk8sv1beta1.VNIProperties{
													RD:        frrk8sv1beta1.RouteDistinguisher(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnCfg.L2VNI)),
													ImportRTs: []frrk8sv1beta1.ImportRouteTarget{frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnCfg.L2VNI))},
													ExportRTs: []frrk8sv1beta1.ExportRouteTarget{frrk8sv1beta1.ExportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnCfg.L2VNI))},
												},
											},
										},
									},
								},
							},
						},
					},
				}

				// Second config: same L2VNI with external ASN import RT (merged with first).
				cfgExternal := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-evpn-l2-external",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRConfigurationSpec{
						BGP: frrk8sv1beta1.BGPConfig{
							Routers: []frrk8sv1beta1.Router{
								{
									ASN: infra.FRRK8sASN,
									EVPN: &frrk8sv1beta1.EVPNConfig{
										AdvertiseVNIs: ptr.To(frrk8sv1beta1.VNIAdvertisementAll),
										L2VNIs: []frrk8sv1beta1.L2VNI{
											{
												VNI: evpnCfg.L2VNI,
												VNIProperties: frrk8sv1beta1.VNIProperties{
													RD:        frrk8sv1beta1.RouteDistinguisher(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnCfg.L2VNI)),
													ImportRTs: []frrk8sv1beta1.ImportRouteTarget{frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", evpnFRRContainer.RouterConfig.ASN, evpnCfg.L2VNI))},
													ExportRTs: []frrk8sv1beta1.ExportRouteTarget{frrk8sv1beta1.ExportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnCfg.L2VNI))},
												},
											},
										},
									},
								},
							},
						},
					},
				}

				ginkgo.By("Applying split FRRConfigurations")
				err = updater.Update(peersConfig.Secrets, cfgLocal, cfgExternal)
				Expect(err).NotTo(HaveOccurred())

				validateL2VNI()
			})
		})

		ginkgo.Context("L3 VNI", func() {
			// l3VNIConfigs builds per-node FRRConfigurations with both the default-VRF
			// router (neighbors + advertiseVNIs) and the VRF router (L3VNI).
			l3VNIConfigs := func(neighbors []frrk8sv1beta1.Neighbor) []frrk8sv1beta1.FRRConfiguration {
				var cfgs []frrk8sv1beta1.FRRConfiguration
				for _, node := range nodes {
					cfgs = append(cfgs, frrk8sv1beta1.FRRConfiguration{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("test-evpn-l3-%s", node.Name),
							Namespace: k8s.FRRK8sNamespace,
						},
						Spec: frrk8sv1beta1.FRRConfigurationSpec{
							NodeSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/hostname": node.Name,
								},
							},
							BGP: frrk8sv1beta1.BGPConfig{
								Routers: []frrk8sv1beta1.Router{
									{
										ASN:       infra.FRRK8sASN,
										Neighbors: neighbors,
										EVPN: &frrk8sv1beta1.EVPNConfig{
											AdvertiseVNIs: ptr.To(frrk8sv1beta1.VNIAdvertisementAll),
										},
									},
									{
										ASN:      infra.FRRK8sASN,
										VRF:      evpnCfg.L3VRF,
										Prefixes: evpnInfra.L3PrefixForFamily(node.Name, advertiseFamily),
										EVPN: &frrk8sv1beta1.EVPNConfig{
											L3VNI: &frrk8sv1beta1.L3VNI{
												VNI: evpnCfg.L3VNI,
												VNIProperties: frrk8sv1beta1.VNIProperties{
													RD: frrk8sv1beta1.RouteDistinguisher(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnCfg.L3VNI)),
													ImportRTs: []frrk8sv1beta1.ImportRouteTarget{
														frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnCfg.L3VNI)),
														frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", evpnFRRContainer.RouterConfig.ASN, evpnCfg.L3VNI)),
													},
													ExportRTs: []frrk8sv1beta1.ExportRouteTarget{frrk8sv1beta1.ExportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnCfg.L3VNI))},
												},
												AdvertisePrefixes: []frrk8sv1beta1.AdvertisePrefixType{"unicast"},
											},
										},
									},
								},
							},
						},
					})
				}
				return cfgs
			}

			ginkgo.It("should advertise type-5 prefixes via L3 VNI", func() {
				peersConfig := setupEVPN(evpnL3ConfigTemplate)

				ginkgo.By("Building FRRConfigurations with EVPN L3 VNI")
				neighbors := neighborsWithEVPN(peersConfig)

				for _, frrCfg := range l3VNIConfigs(neighbors) {
					err := updater.Update(peersConfig.Secrets, frrCfg)
					Expect(err).NotTo(HaveOccurred())
				}

				validateL3VNI()
			})

			ginkgo.It("should work with L3 VNI config split across multiple FRRConfigurations", func() {
				peersConfig := setupEVPN(evpnL3ConfigTemplate)

				ginkgo.By("Building split FRRConfigurations: default-VRF router and VRF router separately")
				neighbors := neighborsWithEVPN(peersConfig)

				// Split each per-node config into two: one for the default-VRF router
				// (neighbors + advertiseVNIs) and one for the VRF router (L3VNI).
				for _, frrCfg := range l3VNIConfigs(neighbors) {
					nodeSelector := frrCfg.Spec.NodeSelector
					routers := frrCfg.Spec.BGP.Routers

					cfgDefault := frrk8sv1beta1.FRRConfiguration{
						ObjectMeta: metav1.ObjectMeta{
							Name:      frrCfg.Name + "-default",
							Namespace: k8s.FRRK8sNamespace,
						},
						Spec: frrk8sv1beta1.FRRConfigurationSpec{
							NodeSelector: nodeSelector,
							BGP: frrk8sv1beta1.BGPConfig{
								Routers: []frrk8sv1beta1.Router{routers[0]},
							},
						},
					}

					cfgVRF := frrk8sv1beta1.FRRConfiguration{
						ObjectMeta: metav1.ObjectMeta{
							Name:      frrCfg.Name + "-vrf",
							Namespace: k8s.FRRK8sNamespace,
						},
						Spec: frrk8sv1beta1.FRRConfigurationSpec{
							NodeSelector: nodeSelector,
							BGP: frrk8sv1beta1.BGPConfig{
								Routers: []frrk8sv1beta1.Router{routers[1]},
							},
						},
					}

					err := updater.Update(peersConfig.Secrets, cfgDefault, cfgVRF)
					Expect(err).NotTo(HaveOccurred())
				}

				validateL3VNI()
			})
		})
	},
	ginkgo.Entry("on IPV4 configurations with IPv4 peering and IPv4 advertising", ipfamily.IPv4, ipfamily.IPv4),
	ginkgo.Entry("on DUALSTACK configurations with IPv4 peering and dualstack advertising", ipfamily.IPv4, ipfamily.DualStack),
	ginkgo.Entry("on DUALSTACK configurations with IPv6 peering and dualstack advertising", ipfamily.IPv6, ipfamily.DualStack),
)

func expectedL2VNIRoutes(frrk8sPods []*corev1.Pod) (map[string]string, error) {
	routes := make(map[string]string, len(frrk8sPods))
	for _, pod := range frrk8sPods {
		podExec := executor.ForPod(pod.Namespace, pod.Name, k8s.FRRContainerName)
		out, err := podExec.Exec("cat", fmt.Sprintf("/sys/class/net/%s/address", evpnL2Iface))
		if err != nil {
			return nil, fmt.Errorf("failed to get MAC for pod %s: %w", pod.Name, err)
		}
		mac := strings.TrimSpace(out)
		if mac == "" {
			return nil, fmt.Errorf("empty MAC for pod %s", pod.Name)
		}
		routes[mac] = pod.Status.HostIP
	}
	return routes, nil
}

func ping(exec executor.Executor, targetIP string) error {
	out, err := exec.Exec("ping", "-c", "1", "-W", "2", targetIP)
	if err != nil {
		return fmt.Errorf("ping %s failed: %w\noutput: %s", targetIP, err, out)
	}
	return nil
}

func pingVRF(exec executor.Executor, vrf, targetIP string) error {
	out, err := exec.Exec("ip", "vrf", "exec", vrf, "ping", "-c", "1", "-W", "2", targetIP)
	if err != nil {
		return fmt.Errorf("ping %s via VRF %s failed: %w\noutput: %s", targetIP, vrf, err, out)
	}
	return nil
}
