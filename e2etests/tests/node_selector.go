// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"github.com/onsi/ginkgo/v2"

	"go.universe.tf/e2etest/pkg/frr/container"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	. "github.com/onsi/gomega"

	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	"go.universe.tf/e2etest/pkg/ipfamily"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = ginkgo.Describe("Node Selector", func() {
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

	ginkgo.Context("Advertise", func() {
		type params struct {
			vrf            string
			ipFamily       ipfamily.Family
			myAsn          uint32
			prefixes       []string // prefixes that selected nodes advertise to the containers
			globalPrefixes []string // prefixes that all nodes advertise to the containers
			modifyPeers    func([]config.Peer, []config.Peer)
		}

		ginkgo.DescribeTable("global and single config", func(p params) {
			frrs := config.ContainersForVRF(infra.FRRContainers, p.vrf)
			peersConfig := config.PeersForContainers(frrs, p.ipFamily)
			p.modifyPeers(peersConfig.PeersV4, peersConfig.PeersV6)
			neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)

			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("pairing with nodes")
			for _, c := range frrs {
				err := container.PairWithNodes(cs, c, p.ipFamily)
				Expect(err).NotTo(HaveOccurred())
			}

			ginkgo.By("creating the config selecting a single node")
			configWithSelector := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testadv-selector",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       p.myAsn,
								VRF:       p.vrf,
								Neighbors: neighbors,
								Prefixes:  p.prefixes,
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

			err = updater.Update(peersConfig.Secrets, configWithSelector)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("validating the containers paired only with the node matching the config")
			for _, c := range frrs {
				ValidateFRRPeeredWithNodes(nodes[:1], c, p.ipFamily)
				ValidateFRRNotPeeredWithNodes(nodes[1:], c, p.ipFamily)
			}

			ginkgo.By("validating only the node matching the config advertises the prefixes")
			peers := peersConfig.PeersV4
			if p.ipFamily == ipfamily.IPv6 {
				peers = peersConfig.PeersV6
			}
			for _, peer := range peers {
				ValidatePrefixesForNeighbor(peer.FRR, nodes[:1], p.prefixes...)
				ValidateNeighborNoPrefixes(peer.FRR, nodes[1:], p.prefixes...)
			}

			ginkgo.By("creating the global config")
			globalConfig := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testadv-selector-global",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       p.myAsn,
								VRF:       p.vrf,
								Neighbors: neighbors,
								Prefixes:  p.globalPrefixes,
							},
						},
					},
				},
			}
			err = updater.Update(peersConfig.Secrets, globalConfig)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("validating the containers are now paired with all nodes")
			for _, c := range frrs {
				ValidateFRRPeeredWithNodes(nodes, c, p.ipFamily)
			}

			ginkgo.By("validating that the node matching the first config advertises all prefixes and the other nodes advertise the global prefixes only")
			for _, peer := range peers {
				ValidatePrefixesForNeighbor(peer.FRR, nodes[:1], p.prefixes...)
				ValidateNeighborNoPrefixes(peer.FRR, nodes[1:], p.prefixes...)
				ValidatePrefixesForNeighbor(peer.FRR, nodes, p.globalPrefixes...)
			}
		},
			ginkgo.Entry("IPV4", params{
				ipFamily:       ipfamily.IPv4,
				vrf:            "",
				myAsn:          infra.FRRK8sASN,
				prefixes:       []string{"192.168.2.0/24", "192.169.2.0/24"},
				globalPrefixes: []string{"192.170.2.0/24", "192.171.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, _ []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
			}),
			ginkgo.Entry("IPV6", params{
				ipFamily:       ipfamily.IPv6,
				vrf:            "",
				myAsn:          infra.FRRK8sASN,
				prefixes:       []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				globalPrefixes: []string{"fc00:f853:ccd:e801::/64", "fc00:f853:ccd:e802::/64"},
				modifyPeers: func(_ []config.Peer, ppV6 []config.Peer) {
					for i := range ppV6 {
						ppV6[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
			}),
		)
	})

	ginkgo.Context("Receive", func() {
		type params struct {
			vrf               string
			ipFamily          ipfamily.Family
			myAsn             uint32
			v4Prefixes        []string // V4 prefixes that selected nodes receive from the containers
			v6Prefixes        []string // V6 prefixes that selected nodes receive from the containers
			globalv4Prefixes  []string // V4 prefixes that all nodes receive from the containers
			globalv6Prefixes  []string // V6 prefixes that all nodes receive from the containers
			modifyPeers       func([]config.Peer, []config.Peer)
			globalModifyPeers func([]config.Peer, []config.Peer)
		}

		ginkgo.DescribeTable("global and single config", func(p params) {
			frrs := config.ContainersForVRF(infra.FRRContainers, p.vrf)
			peersConfig := config.PeersForContainers(frrs, p.ipFamily)
			peersV4ForFirst, peersV6ForFirst := peersConfig.PeersV4, peersConfig.PeersV6
			p.modifyPeers(peersV4ForFirst, peersV6ForFirst)
			neighbors := config.NeighborsFromPeers(peersV4ForFirst, peersV6ForFirst)

			allNodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())

			pods, err := k8s.FRRK8sPods(cs)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("pairing with nodes")
			allV4Pfxs := p.v4Prefixes
			allV4Pfxs = append(allV4Pfxs, p.globalv4Prefixes...)
			allV6Pfxs := p.v6Prefixes
			allV6Pfxs = append(allV6Pfxs, p.globalv6Prefixes...)
			for _, c := range frrs {
				err := container.PairWithNodes(cs, c, p.ipFamily, func(frr *container.FRR) {
					frr.NeighborConfig.ToAdvertiseV4 = allV4Pfxs
					frr.NeighborConfig.ToAdvertiseV6 = allV6Pfxs
				})
				Expect(err).NotTo(HaveOccurred())
			}

			ginkgo.By("creating the config selecting a single node")
			firstNode := corev1.Node{} // corresponding to the pods slice - the one we create the config for
			otherNodes := []corev1.Node{}
			for i, n := range allNodes {
				if n.Name != pods[0].Spec.NodeName {
					continue
				}
				firstNode = n
				otherNodes = allNodes[:i]
				otherNodes = append(otherNodes, allNodes[i+1:]...)
			}
			Expect(firstNode).NotTo(Equal(corev1.Node{}))

			cfg := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testreceive-selector",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       p.myAsn,
								VRF:       p.vrf,
								Neighbors: neighbors,
							},
						},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/hostname": pods[0].Spec.NodeName,
						},
					},
				},
			}

			err = updater.Update(peersConfig.Secrets, cfg)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("validating the containers paired only with the node matching the config")
			for _, c := range frrs {
				ValidateFRRPeeredWithNodes([]corev1.Node{firstNode}, c, p.ipFamily)
				ValidateFRRNotPeeredWithNodes(otherNodes, c, p.ipFamily)
			}

			ginkgo.By("validating only the node matching the config receives the prefixes")
			peers := peersConfig.PeersV4
			if p.ipFamily == ipfamily.IPv6 {
				peers = peersConfig.PeersV6
			}

			pfxsToValidate := p.v4Prefixes
			globalPfxsToValidate := p.globalv4Prefixes
			if p.ipFamily == ipfamily.IPv6 {
				pfxsToValidate = p.v6Prefixes
				globalPfxsToValidate = p.globalv6Prefixes
			}

			for _, peer := range peers {
				ValidateNodesHaveRoutes(pods[:1], peer.FRR, pfxsToValidate...)
				ValidateNodesDoNotHaveRoutes(pods[1:], peer.FRR, pfxsToValidate...)
			}

			ginkgo.By("creating the global config")
			p.globalModifyPeers(peersConfig.PeersV4, peersConfig.PeersV6)
			neighbors = config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)
			globalConfig := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testreceive-selector-global",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       p.myAsn,
								VRF:       p.vrf,
								Neighbors: neighbors,
								Prefixes:  globalPfxsToValidate,
							},
						},
					},
				},
			}
			err = updater.Update(peersConfig.Secrets, globalConfig)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("validating the containers are now paired with all nodes")
			for _, c := range frrs {
				ValidateFRRPeeredWithNodes(allNodes, c, p.ipFamily)
			}

			ginkgo.By("validating that the node matching the first config receives all prefixes and the other nodes receive the global prefixes only")
			for _, peer := range peers {
				ValidateNodesHaveRoutes(pods[:1], peer.FRR, pfxsToValidate...)
				ValidateNodesDoNotHaveRoutes(pods[1:], peer.FRR, pfxsToValidate...)
				ValidateNodesHaveRoutes(pods, peer.FRR, globalPfxsToValidate...)
			}
		},
			ginkgo.Entry("IPV4", params{
				ipFamily:         ipfamily.IPv4,
				vrf:              "",
				myAsn:            infra.FRRK8sASN,
				v4Prefixes:       []string{"192.168.2.0/24", "192.169.2.0/24"},
				globalv4Prefixes: []string{"192.170.2.0/24", "192.171.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, _ []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
							{Prefix: "192.168.2.0/24"},
							{Prefix: "192.169.2.0/24"},
						}
					}
				},
				globalModifyPeers: func(ppV4 []config.Peer, _ []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
							{Prefix: "192.170.2.0/24"},
							{Prefix: "192.171.2.0/24"},
						}
					}
				},
			}),
			ginkgo.Entry("IPV6", params{
				ipFamily:         ipfamily.IPv6,
				vrf:              "",
				myAsn:            infra.FRRK8sASN,
				v6Prefixes:       []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				globalv6Prefixes: []string{"fc00:f853:ccd:e801::/64", "fc00:f853:ccd:e802::/64"},
				modifyPeers: func(_ []config.Peer, ppV6 []config.Peer) {
					for i := range ppV6 {
						ppV6[i].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
							{Prefix: "fc00:f853:ccd:e799::/64"},
							{Prefix: "fc00:f853:ccd:e800::/64"},
						}
					}
				},
				globalModifyPeers: func(_ []config.Peer, ppV6 []config.Peer) {
					for i := range ppV6 {
						ppV6[i].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
							{Prefix: "fc00:f853:ccd:e801::/64"},
							{Prefix: "fc00:f853:ccd:e802::/64"},
						}
					}
				},
			}),
		)
	})
})
