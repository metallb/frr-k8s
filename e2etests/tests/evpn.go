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
	evpnL2VNI      = 1000
	evpnL2VLANID   = 100
	evpnL3VNI      = 3000
	evpnL3VLANID   = 4000
	evpnL3VRF      = "evpnred"
	evpnBridge     = "evpnbr"
	evpnVxlan      = "evpnvx"
	evpnL3VRFTable = 10
	evpnL2Iface    = "evpnl2-100"
)

var evpnL2Config = infra.EVPNConfig{
	L2VNI:    evpnL2VNI,
	L2VLANID: evpnL2VLANID,
	L2IPFmt:  "10.100.0.%d/24",
	Bridge:   evpnBridge,
	Vxlan:    evpnVxlan,
}

var evpnL3Config = infra.EVPNConfig{
	L3VNI:       evpnL3VNI,
	L3VLANID:    evpnL3VLANID,
	L3VRF:       evpnL3VRF,
	L3VRFTable:  evpnL3VRFTable,
	L3PrefixFmt: "10.200.%d.1/24",
	Bridge:      evpnBridge,
	Vxlan:       evpnVxlan,
}

var _ = ginkgo.Describe("EVPN", func() {
	var cs clientset.Interface

	defer ginkgo.GinkgoRecover()
	updater, err := config.NewUpdater()
	Expect(err).NotTo(HaveOccurred())
	reporter := dump.NewK8sReporter(k8s.FRRK8sNamespace)

	var (
		nodes   []corev1.Node
		evpnCfg infra.EVPNConfig
		evpnInfra *infra.EVPN
	)

	ginkgo.BeforeEach(func() {
		ginkgo.By("Clearing any previous configuration")
		for _, c := range infra.FRRContainers {
			err := c.UpdateBGPConfigFile(frrconfig.Empty)
			Expect(err).NotTo(HaveOccurred())
		}
		err := updater.Clean()
		Expect(err).NotTo(HaveOccurred())
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
		err := infra.CleanupEVPN(cs, evpnCfg)
		Expect(err).NotTo(HaveOccurred())
	})

	setupEVPN := func(cfg infra.EVPNConfig) config.PeersConfig {
		ginkgo.GinkgoHelper()
		evpnCfg = cfg

		ginkgo.By("Setting up EVPN networking infrastructure")
		var err error
		evpnInfra, err = infra.SetupEVPN(cs, cfg)
		Expect(err).NotTo(HaveOccurred())

		ginkgo.By("Pairing external FRR with nodes (EVPN)")
		err = frr.PairWithNodesForEVPN(infra.EVPNContainer, cs, frr.EVPNConfig{
			L2VNI: cfg.L2VNI,
			L3VNI: cfg.L3VNI,
			L3VRF: cfg.L3VRF,
		}, ipfamily.IPv4)
		Expect(err).NotTo(HaveOccurred())
		return config.PeersForContainers([]*frrcontainer.FRR{infra.EVPNContainer}, ipfamily.IPv4)
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
	validateL2VNI := func(evpnCfg infra.EVPNConfig, evpnInfra *infra.EVPN) {
		ginkgo.GinkgoHelper()
		ginkgo.By("Validating BGP sessions are established")
		ValidateFRRPeeredWithNodes(nodes, infra.EVPNContainer, ipfamily.IPv4)

		ginkgo.By("Validating EVPN address family is active on frr-k8s pods")
		pods, err := k8s.FRRK8sPods(cs)
		Expect(err).NotTo(HaveOccurred())

		for _, pod := range pods {
			Eventually(func() error {
				return frr.ForPod(pod).HasEVPNNeighbor(infra.EVPNContainer.Ipv4)
			}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
				"EVPN address family not active on pod %s", pod.Name)
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
			return frr.ForContainer(infra.EVPNContainer).HasEVPNType2Routes(expectedType2)
		}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
			"Type-2 routes not found on external FRR")

		ginkgo.By("Validating L2 data path from external FRR to nodes")
		for _, node := range nodes {
			l2IP := evpnInfra.L2IP(node.Name)
			Eventually(func() error {
				return ping(infra.EVPNContainer, l2IP)
			}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
				"L2 ping from external FRR to node %s (%s) failed", node.Name, l2IP)
		}
	}

	// validateL3VNI validates L3 VNI EVPN state: sessions, type-5 routes,
	// and data path connectivity.
	validateL3VNI := func(evpnInfra *infra.EVPN) {
		ginkgo.GinkgoHelper()
		ginkgo.By("Validating BGP sessions are established")
		ValidateFRRPeeredWithNodes(nodes, infra.EVPNContainer, ipfamily.IPv4)

		ginkgo.By("Validating EVPN address family is active on frr-k8s pods")
		pods, err := k8s.FRRK8sPods(cs)
		Expect(err).NotTo(HaveOccurred())

		for _, pod := range pods {
			Eventually(func() error {
				return frr.ForPod(pod).HasEVPNNeighbor(infra.EVPNContainer.Ipv4)
			}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
				"EVPN address family not active on pod %s", pod.Name)
		}

		ginkgo.By("Validating EVPN type-5 routes are received on external FRR")
		expectedRoutes := map[string]string{}
		for _, node := range nodes {
			nodeIP, err := k8s.NodeIPForFamily(node, ipfamily.IPv4)
			Expect(err).NotTo(HaveOccurred())
			expectedRoutes[evpnInfra.L3Prefix(node.Name)] = nodeIP
		}

		Eventually(func() error {
			return frr.ForContainer(infra.EVPNContainer).HasEVPNType5Routes(expectedRoutes)
		}, 30*time.Second, time.Second).ShouldNot(HaveOccurred())

		ginkgo.By("Validating L3 data path from external FRR to nodes via VRF")
		for _, node := range nodes {
			l3IP := evpnInfra.L3PrefixIP(node.Name)
			Eventually(func() error {
				return pingVRF(infra.EVPNContainer, evpnL3VRF, l3IP)
			}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
				"L3 ping from external FRR to node %s (%s) via VRF %s failed", node.Name, l3IP, evpnL3VRF)
		}
	}

	ginkgo.Context("L2 VNI", func() {
		ginkgo.It("should establish EVPN session and exchange L2 VNI routes", func() {
			cfg := evpnL2Config
			peersConfig := setupEVPN(cfg)

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
											VNI: evpnL2VNI,
											VNIProperties: frrk8sv1beta1.VNIProperties{
												RD:  frrk8sv1beta1.RouteDistinguisher(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL2VNI)),
												ImportRTs: []frrk8sv1beta1.ImportRouteTarget{
													frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL2VNI)),
													frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", infra.EVPNContainer.RouterConfig.ASN, evpnL2VNI)),
												},
												ExportRTs: []frrk8sv1beta1.ExportRouteTarget{frrk8sv1beta1.ExportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL2VNI))},
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

			validateL2VNI(cfg, evpnInfra)
		})

		ginkgo.It("should work with L2 VNI config split across multiple FRRConfigurations", func() {
			cfg := evpnL2Config
			peersConfig := setupEVPN(cfg)

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
											VNI: evpnL2VNI,
											VNIProperties: frrk8sv1beta1.VNIProperties{
												RD:        frrk8sv1beta1.RouteDistinguisher(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL2VNI)),
												ImportRTs: []frrk8sv1beta1.ImportRouteTarget{frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL2VNI))},
												ExportRTs: []frrk8sv1beta1.ExportRouteTarget{frrk8sv1beta1.ExportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL2VNI))},
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
											VNI: evpnL2VNI,
											VNIProperties: frrk8sv1beta1.VNIProperties{
												RD:        frrk8sv1beta1.RouteDistinguisher(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL2VNI)),
												ImportRTs: []frrk8sv1beta1.ImportRouteTarget{frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", infra.EVPNContainer.RouterConfig.ASN, evpnL2VNI))},
												ExportRTs: []frrk8sv1beta1.ExportRouteTarget{frrk8sv1beta1.ExportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL2VNI))},
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

			validateL2VNI(cfg, evpnInfra)
		})
	})

	ginkgo.Context("L3 VNI", func() {
		// l3VNIConfigs builds per-node FRRConfigurations with both the default-VRF
		// router (neighbors + advertiseVNIs) and the VRF router (L3VNI).
		l3VNIConfigs := func(neighbors []frrk8sv1beta1.Neighbor, evpnInfra *infra.EVPN) []frrk8sv1beta1.FRRConfiguration {
			var cfgs []frrk8sv1beta1.FRRConfiguration
			for _, node := range nodes {
				prefixes := []string{evpnInfra.L3Prefix(node.Name)}

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
									VRF:      evpnL3VRF,
									Prefixes: prefixes,
									EVPN: &frrk8sv1beta1.EVPNConfig{
										L3VNI: &frrk8sv1beta1.L3VNI{
											VNI: evpnL3VNI,
											VNIProperties: frrk8sv1beta1.VNIProperties{
												RD:  frrk8sv1beta1.RouteDistinguisher(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL3VNI)),
												ImportRTs: []frrk8sv1beta1.ImportRouteTarget{
													frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL3VNI)),
													frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", infra.EVPNContainer.RouterConfig.ASN, evpnL3VNI)),
												},
												ExportRTs: []frrk8sv1beta1.ExportRouteTarget{frrk8sv1beta1.ExportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL3VNI))},
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
			cfg := evpnL3Config
			peersConfig := setupEVPN(cfg)

			ginkgo.By("Building FRRConfigurations with EVPN L3 VNI")
			neighbors := neighborsWithEVPN(peersConfig)

			for _, frrCfg := range l3VNIConfigs(neighbors, evpnInfra) {
				err := updater.Update(peersConfig.Secrets, frrCfg)
				Expect(err).NotTo(HaveOccurred())
			}

			validateL3VNI(evpnInfra)
		})

		ginkgo.It("should work with L3 VNI config split across multiple FRRConfigurations", func() {
			cfg := evpnL3Config
			peersConfig := setupEVPN(cfg)

			ginkgo.By("Building split FRRConfigurations: default-VRF router and VRF router separately")
			neighbors := neighborsWithEVPN(peersConfig)

			// Split each per-node config into two: one for the default-VRF router
			// (neighbors + advertiseVNIs) and one for the VRF router (L3VNI).
			for _, frrCfg := range l3VNIConfigs(neighbors, evpnInfra) {
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

			validateL3VNI(evpnInfra)
		})
	})
})

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
