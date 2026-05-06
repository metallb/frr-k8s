// SPDX-License-Identifier:Apache-2.0

package tests

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	. "github.com/onsi/gomega"
	"go.universe.tf/e2etest/pkg/executor"
	"go.universe.tf/e2etest/pkg/frr"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

//go:embed testdata/evpn-setup.sh
var evpnSetupScript string

//go:embed testdata/evpn-node-setup.sh
var evpnNodeSetupScript string

const (
	evpnL2VNI      = 1000
	evpnL2VLANID   = 100
	evpnL3VNI      = 3000
	evpnL3VLANID   = 4000
	evpnL3VRF      = "evpnred"
	evpnBridge     = "evpnbr"
	evpnVxlan      = "evpnvx"
	evpnL3VRFTable = 10
	evpnL2Iface = "evpnl2-100"
)

var _ = ginkgo.Describe("EVPN IPV4", func() {
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

	ginkgo.Context("L2 VNI", func() {
		var (
			externalFRR *frrcontainer.FRR
			nodes       []corev1.Node
		)

		ginkgo.BeforeEach(func() {
			// EVPN tests need a single-hop container because it shares the kind
			// network with nodes, so VXLAN underlay works without extra routing.
			// ibgp-single-hop would also work; we pick eBGP as the more common
			// EVPN deployment model.
			frrs := config.ContainersForVRF(infra.FRRContainers, "")
			for _, f := range frrs {
				if f.Name == "ebgp-single-hop" {
					externalFRR = f
					break
				}
			}
			Expect(externalFRR).NotTo(BeNil(), "ebgp-single-hop container not found")

			var err error
			nodes, err = k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			if externalFRR != nil {
				err := runEVPNSetupScript(nodes, externalFRR, true, false, true)
				if err != nil {
					ginkgo.GinkgoWriter.Printf("EVPN cleanup error: %v\n", err)
				}
			}
		})

		// validateL2VNI validates the L2 VNI EVPN state: sessions, VNI visibility,
		// type-2 routes, and data path connectivity.
		validateL2VNI := func(externalFRR *frrcontainer.FRR, nodes []corev1.Node) {
			ginkgo.By("Validating BGP sessions are established")
			ValidateFRRPeeredWithNodes(nodes, externalFRR, ipfamily.IPv4)

			ginkgo.By("Validating EVPN address family is active on frr-k8s pods")
			pods, err := k8s.FRRK8sPods(cs)
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range pods {
				podExec := executor.ForPod(pod.Namespace, pod.Name, "frr")
				Eventually(func() error {
					return neighborHasAddressFamily(podExec, externalFRR.Ipv4)
				}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
					"EVPN address family not active on pod %s", pod.Name)
			}

			ginkgo.By("Validating L2 VNI is visible in EVPN state")
			for _, pod := range pods {
				podExec := executor.ForPod(pod.Namespace, pod.Name, "frr")
				Eventually(func() error {
					return vniExists(podExec, evpnL2VNI)
				}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
					"L2 VNI %d not visible on pod %s", evpnL2VNI, pod.Name)
			}

			ginkgo.By("Collecting node MAC addresses for type-2 route validation")
			nodeMacs, err := nodeL2VNIMacs(pods)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Validating EVPN type-2 routes are exchanged")
			Eventually(func() error {
				return checkType2Routes(externalFRR, nodeMacs)
			}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
				"Type-2 routes for node MACs not found on external FRR")

			ginkgo.By("Validating L2 data path from external FRR to nodes")
			for nodeIdx := range nodes {
				nodeIP := fmt.Sprintf("10.100.0.%d", nodeIdx+1)
				Eventually(func() error {
					return ping(externalFRR, nodeIP)
				}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
					"L2 ping from external FRR to node %s (%s) failed", nodes[nodeIdx].Name, nodeIP)
			}
		}

		ginkgo.It("should establish EVPN session and exchange L2 VNI routes", func() {
			ginkgo.By("Pairing external FRR with nodes")
			err := frrcontainer.PairWithNodes(cs, externalFRR, ipfamily.IPv4)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Setting up EVPN networking on nodes and external FRR")
			err = runEVPNSetupScript(nodes, externalFRR, true, false, false)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Building FRRConfiguration with EVPN L2 VNI")
			peersConfig := config.PeersForContainers([]*frrcontainer.FRR{externalFRR}, ipfamily.IPv4)
			neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)
			// Set address families to include EVPN
			for i := range neighbors {
				neighbors[i].AddressFamilies = []frrk8sv1beta1.AddressFamily{"unicast", "evpn"}
			}

			cfg := frrk8sv1beta1.FRRConfiguration{
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
													frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", externalFRR.RouterConfig.ASN, evpnL2VNI)),
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
			err = updater.Update(peersConfig.Secrets, cfg)
			Expect(err).NotTo(HaveOccurred())

			validateL2VNI(externalFRR, nodes)
		})

		ginkgo.It("should work with L2 VNI config split across multiple FRRConfigurations", func() {
			ginkgo.By("Pairing external FRR with nodes")
			err := frrcontainer.PairWithNodes(cs, externalFRR, ipfamily.IPv4)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Setting up EVPN networking on nodes and external FRR")
			err = runEVPNSetupScript(nodes, externalFRR, true, false, false)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Building split FRRConfigurations: one with local ASN RT, one with external ASN RT")
			peersConfig := config.PeersForContainers([]*frrcontainer.FRR{externalFRR}, ipfamily.IPv4)
			neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)
			for i := range neighbors {
				neighbors[i].AddressFamilies = []frrk8sv1beta1.AddressFamily{"unicast", "evpn"}
			}

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
												ImportRTs: []frrk8sv1beta1.ImportRouteTarget{frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", externalFRR.RouterConfig.ASN, evpnL2VNI))},
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

			validateL2VNI(externalFRR, nodes)
		})
	})

	ginkgo.Context("L3 VNI", func() {
		var (
			externalFRR *frrcontainer.FRR
			nodes       []corev1.Node
		)

		ginkgo.BeforeEach(func() {
			frrs := config.ContainersForVRF(infra.FRRContainers, "")
			for _, f := range frrs {
				if f.Name == "ebgp-single-hop" {
					externalFRR = f
					break
				}
			}
			Expect(externalFRR).NotTo(BeNil(), "ebgp-single-hop container not found")

			var err error
			nodes, err = k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			if externalFRR != nil {
				err := runEVPNSetupScript(nodes, externalFRR, false, true, true)
				if err != nil {
					ginkgo.GinkgoWriter.Printf("EVPN cleanup error: %v\n", err)
				}
			}
		})

		// validateL3VNI validates the L3 VNI EVPN state: sessions, type-5 routes,
		// and data path connectivity.
		validateL3VNI := func(externalFRR *frrcontainer.FRR, nodes []corev1.Node) {
			ginkgo.By("Validating BGP sessions are established")
			ValidateFRRPeeredWithNodes(nodes, externalFRR, ipfamily.IPv4)

			ginkgo.By("Validating EVPN address family is active on frr-k8s pods")
			pods, err := k8s.FRRK8sPods(cs)
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range pods {
				podExec := executor.ForPod(pod.Namespace, pod.Name, "frr")
				Eventually(func() error {
					return neighborHasAddressFamily(podExec, externalFRR.Ipv4)
				}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
					"EVPN address family not active on pod %s", pod.Name)
			}

			ginkgo.By("Validating EVPN type-5 routes are received on external FRR")
			expectedRoutes := map[string]string{}
			for nodeIdx, node := range nodes {
				routeKey := fmt.Sprintf("[5]:[0]:[24]:[10.200.%d.0]", nodeIdx+1)
				for _, addr := range node.Status.Addresses {
					if addr.Type == corev1.NodeInternalIP && !strings.Contains(addr.Address, ":") {
						expectedRoutes[routeKey] = addr.Address
						break
					}
				}
			}

			Eventually(func() error {
				return checkType5Routes(externalFRR, expectedRoutes)
			}, 30*time.Second, time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Validating L3 data path from external FRR to nodes via VRF")
			for nodeIdx := range nodes {
				nodeIP := fmt.Sprintf("10.200.%d.1", nodeIdx+1)
				Eventually(func() error {
					return pingVRF(externalFRR, evpnL3VRF, nodeIP)
				}, 30*time.Second, time.Second).ShouldNot(HaveOccurred(),
					"L3 ping from external FRR to node %s (%s) via VRF %s failed", nodes[nodeIdx].Name, nodeIP, evpnL3VRF)
			}
		}

		// l3VNIConfigs builds per-node FRRConfigurations with both the default-VRF
		// router (neighbors + advertiseVNIs) and the VRF router (L3VNI) in a single config.
		l3VNIConfigs := func(neighbors []frrk8sv1beta1.Neighbor, externalFRR *frrcontainer.FRR, nodes []corev1.Node) []frrk8sv1beta1.FRRConfiguration {
			var cfgs []frrk8sv1beta1.FRRConfiguration
			for nodeIdx, node := range nodes {
				prefix := fmt.Sprintf("10.200.%d.0/24", nodeIdx+1)
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
									Prefixes: []string{prefix},
									EVPN: &frrk8sv1beta1.EVPNConfig{
										L3VNI: &frrk8sv1beta1.L3VNI{
											VNI: evpnL3VNI,
											VNIProperties: frrk8sv1beta1.VNIProperties{
												RD:  frrk8sv1beta1.RouteDistinguisher(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL3VNI)),
												ImportRTs: []frrk8sv1beta1.ImportRouteTarget{
													frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", infra.FRRK8sASN, evpnL3VNI)),
													frrk8sv1beta1.ImportRouteTarget(fmt.Sprintf("%d:%d", externalFRR.RouterConfig.ASN, evpnL3VNI)),
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
			ginkgo.By("Pairing external FRR with nodes")
			err := frrcontainer.PairWithNodes(cs, externalFRR, ipfamily.IPv4)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Setting up EVPN networking with L3 VNI")
			err = runEVPNSetupScript(nodes, externalFRR, false, true, false)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Building FRRConfigurations with EVPN L3 VNI")
			peersConfig := config.PeersForContainers([]*frrcontainer.FRR{externalFRR}, ipfamily.IPv4)
			neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)
			for i := range neighbors {
				neighbors[i].AddressFamilies = []frrk8sv1beta1.AddressFamily{"unicast", "evpn"}
			}

			for _, cfg := range l3VNIConfigs(neighbors, externalFRR, nodes) {
				err := updater.Update(peersConfig.Secrets, cfg)
				Expect(err).NotTo(HaveOccurred())
			}

			validateL3VNI(externalFRR, nodes)
		})

		ginkgo.It("should work with L3 VNI config split across multiple FRRConfigurations", func() {
			ginkgo.By("Pairing external FRR with nodes")
			err := frrcontainer.PairWithNodes(cs, externalFRR, ipfamily.IPv4)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Setting up EVPN networking with L3 VNI")
			err = runEVPNSetupScript(nodes, externalFRR, false, true, false)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Building split FRRConfigurations: default-VRF router and VRF router separately")
			peersConfig := config.PeersForContainers([]*frrcontainer.FRR{externalFRR}, ipfamily.IPv4)
			neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)
			for i := range neighbors {
				neighbors[i].AddressFamilies = []frrk8sv1beta1.AddressFamily{"unicast", "evpn"}
			}

			// Split each per-node config into two: one for the default-VRF router
			// (neighbors + advertiseVNIs) and one for the VRF router (L3VNI).
			for _, cfg := range l3VNIConfigs(neighbors, externalFRR, nodes) {
				nodeSelector := cfg.Spec.NodeSelector
				routers := cfg.Spec.BGP.Routers

				cfgDefault := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cfg.Name + "-default",
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
						Name:      cfg.Name + "-vrf",
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

			validateL3VNI(externalFRR, nodes)
		})
	})
})

// runEVPNSetupScript runs evpn-setup.sh with the given parameters.
// Both evpn-setup.sh and evpn-node-setup.sh are written to a temp directory
// so evpn-setup.sh can locate and pipe evpn-node-setup.sh into containers.
func runEVPNSetupScript(nodes []corev1.Node, externalFRR *frrcontainer.FRR, l2 bool, l3 bool, cleanup bool) error {
	tmpDir, err := os.MkdirTemp("", "evpn-setup-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "evpn-setup.sh"), []byte(evpnSetupScript), 0755); err != nil {
		return fmt.Errorf("writing evpn-setup.sh: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "evpn-node-setup.sh"), []byte(evpnNodeSetupScript), 0755); err != nil {
		return fmt.Errorf("writing evpn-node-setup.sh: %w", err)
	}

	nodeNames := make([]string, 0, len(nodes))
	for _, n := range nodes {
		nodeNames = append(nodeNames, n.Name)
	}

	env := []string{
		fmt.Sprintf("EVPN_NODES=%s", strings.Join(nodeNames, " ")),
		fmt.Sprintf("EVPN_EXTERNAL=%s", externalFRR.Name),
		fmt.Sprintf("EVPN_EXTERNAL_ASN=%d", externalFRR.RouterConfig.ASN),
		fmt.Sprintf("EVPN_FRR_K8S_ASN=%d", infra.FRRK8sASN),
		fmt.Sprintf("CONTAINER_RUNTIME=%s", executor.ContainerRuntime),
		fmt.Sprintf("EVPN_BRIDGE=%s", evpnBridge),
		fmt.Sprintf("EVPN_VXLAN=%s", evpnVxlan),
		fmt.Sprintf("FRRK8S_NAMESPACE=%s", k8s.FRRK8sNamespace),
		fmt.Sprintf("FRRK8S_LABEL=%s", k8s.FRRK8sDaemonsetLS),
		fmt.Sprintf("FRRK8S_CONTAINER=%s", k8s.FRRContainerName),
	}

	if l2 {
		env = append(env,
			fmt.Sprintf("EVPN_L2_VNI=%d", evpnL2VNI),
			fmt.Sprintf("EVPN_L2_VLAN_ID=%d", evpnL2VLANID),
		)
	}

	if l3 {
		env = append(env,
			fmt.Sprintf("EVPN_L3_VNI=%d", evpnL3VNI),
			fmt.Sprintf("EVPN_L3_VLAN_ID=%d", evpnL3VLANID),
			fmt.Sprintf("EVPN_L3_VRF=%s", evpnL3VRF),
			fmt.Sprintf("EVPN_L3_VRF_TABLE=%d", evpnL3VRFTable),
		)
	}

	if cleanup {
		env = append(env, "EVPN_CLEANUP=true")
	}

	cmd := exec.Command("bash", filepath.Join(tmpDir, "evpn-setup.sh"))
	cmd.Env = append(cmd.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("evpn-setup.sh failed: %w\noutput: %s", err, string(out))
	}
	return nil
}

func neighborHasAddressFamily(exec executor.Executor, neighborIP string) error {
	neighbors, err := frr.NeighborsInfo(exec)
	if err != nil {
		return err
	}
	for _, n := range neighbors {
		if n.ID == neighborIP || n.BGPNeighborAddr == neighborIP {
			for _, af := range n.AddressFamilies {
				if strings.Contains(strings.ToLower(af), "l2vpnevpn") {
					if !n.Connected {
						return fmt.Errorf("neighbor %s has l2vpnEvpn but is not connected", neighborIP)
					}
					return nil
				}
			}
			return fmt.Errorf("neighbor %s does not have l2vpnEvpn address family, has: %v", neighborIP, n.AddressFamilies)
		}
	}
	return fmt.Errorf("neighbor %s not found", neighborIP)
}

func vniExists(exec executor.Executor, vni uint32) error {
	out, err := exec.Exec("vtysh", "-c", fmt.Sprintf("show evpn vni %d json", vni))
	if err != nil {
		return fmt.Errorf("failed to query VNI %d: %w", vni, err)
	}
	var result struct {
		VNI uint32 `json:"vni"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return fmt.Errorf("failed to parse VNI %d output: %w", vni, err)
	}
	if result.VNI != vni {
		return fmt.Errorf("VNI %d not found in EVPN state, got %d", vni, result.VNI)
	}
	return nil
}

func nodeL2VNIMacs(frrk8sPods []*corev1.Pod) ([]string, error) {
	macs := make([]string, 0, len(frrk8sPods))
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
		macs = append(macs, mac)
	}
	return macs, nil
}

func checkType2Routes(exec executor.Executor, expectedMACs []string) error {
	out, err := exec.Exec("vtysh", "-c", "show bgp l2vpn evpn route type macip json")
	if err != nil {
		return fmt.Errorf("failed to query type-2 routes: %w", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		return fmt.Errorf("failed to parse type-2 routes: %w", err)
	}

	var routeKeys []string
	for key, val := range top {
		if key == "numPrefix" || key == "numPaths" {
			continue
		}
		var rd map[string]json.RawMessage
		if err := json.Unmarshal(val, &rd); err != nil {
			continue
		}
		for routeKey := range rd {
			routeKeys = append(routeKeys, routeKey)
		}
	}

	var missing []string
	for _, mac := range expectedMACs {
		found := false
		for _, rk := range routeKeys {
			if strings.Contains(strings.ToLower(rk), strings.ToLower(mac)) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, mac)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("type-2 routes missing for MACs: %v", missing)
	}
	return nil
}

func checkType5Routes(exec executor.Executor, expectedRoutes map[string]string) error {
	out, err := exec.Exec("vtysh", "-c", "show bgp l2vpn evpn route type prefix json")
	if err != nil {
		return fmt.Errorf("failed to query type-5 routes: %w", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		return fmt.Errorf("failed to parse type-5 routes: %w", err)
	}

	type nexthop struct {
		IP string `json:"ip"`
	}
	type path struct {
		NextHops []nexthop `json:"nexthops"`
	}
	type routeEntry struct {
		Paths [][]path `json:"paths"`
	}

	routeNextHops := map[string][]string{}
	for key, val := range top {
		if key == "numPrefix" || key == "numPaths" {
			continue
		}
		var rd map[string]json.RawMessage
		if err := json.Unmarshal(val, &rd); err != nil {
			continue
		}
		for routeKey, routeVal := range rd {
			if routeKey == "rd" {
				continue
			}
			var entry routeEntry
			if err := json.Unmarshal(routeVal, &entry); err != nil {
				continue
			}
			for _, pathGroup := range entry.Paths {
				for _, p := range pathGroup {
					for _, nh := range p.NextHops {
						routeNextHops[routeKey] = append(routeNextHops[routeKey], nh.IP)
					}
				}
			}
		}
	}

	for routeKey, expectedNH := range expectedRoutes {
		nhs, ok := routeNextHops[routeKey]
		if !ok {
			return fmt.Errorf("type-5 route %s not found, have: %v", routeKey, routeNextHops)
		}
		found := false
		for _, nh := range nhs {
			if nh == expectedNH {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("type-5 route %s expected next-hop %s, got %v", routeKey, expectedNH, nhs)
		}
	}
	return nil
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
