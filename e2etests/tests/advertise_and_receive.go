// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.universe.tf/e2etest/pkg/frr/container"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	"go.universe.tf/e2etest/pkg/ipfamily"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = ginkgo.Describe("Advertising and Receiving routes", func() {
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

	ginkgo.Context("Advertising and Receiving IPs", func() {
		type params struct {
			vrf           string
			ipFamily      ipfamily.Family
			myAsn         uint32
			toAdvertiseV4 []string // V4 prefixes that the nodes receive from the containers
			toAdvertiseV6 []string // V6 prefixes that the nodes receive from the containers
			prefixes      []string // all prefixes that the nodes advertise to the containers
			modifyPeers   func([]config.Peer, []config.Peer)
			validate      func([]config.Peer, []config.Peer, []*v1.Pod, []v1.Node)
			splitCfg      func(frrk8sv1beta1.FRRConfiguration) ([]frrk8sv1beta1.FRRConfiguration, error)
		}

		ginkgo.DescribeTable("Works with external frrs", func(p params) {
			frrs := config.ContainersForVRF(infra.FRRContainers, p.vrf)
			peersConfig := config.PeersForContainers(frrs, p.ipFamily)
			p.modifyPeers(peersConfig.PeersV4, peersConfig.PeersV6)
			neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)

			config := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
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
				},
			}

			ginkgo.By("pairing with nodes")
			for _, c := range frrs {
				err := container.PairWithNodes(cs, c, p.ipFamily, func(frr *container.FRR) {
					frr.NeighborConfig.ToAdvertiseV4 = p.toAdvertiseV4
					frr.NeighborConfig.ToAdvertiseV6 = p.toAdvertiseV6
				})
				Expect(err).NotTo(HaveOccurred())
			}
			err := updater.Update(peersConfig.Secrets, config)
			Expect(err).NotTo(HaveOccurred())

			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())

			for _, c := range frrs {
				ValidateFRRPeeredWithNodes(nodes, c, p.ipFamily)
			}

			pods, err := k8s.FRRK8sPods(cs)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("validating")
			p.validate(peersConfig.PeersV4, peersConfig.PeersV6, pods, nodes)

			if p.splitCfg == nil {
				return
			}

			ginkgo.By("Cleaning before retesting with the config splitted")
			err = updater.Clean()
			Expect(err).NotTo(HaveOccurred())

			for _, c := range frrs {
				ValidateFRRNotPeeredWithNodes(nodes, c, p.ipFamily)
			}

			cfgs, err := p.splitCfg(config)
			Expect(err).NotTo(HaveOccurred())

			err = updater.Update(peersConfig.Secrets, cfgs...)
			Expect(err).NotTo(HaveOccurred())

			for _, c := range frrs {
				ValidateFRRPeeredWithNodes(nodes, c, p.ipFamily)
			}

			ginkgo.By("validating with splitted config")
			p.validate(peersConfig.PeersV4, peersConfig.PeersV6, pods, nodes)
		},
			ginkgo.Entry("IPV4 - One config for advertising, the other for receiving", params{
				ipFamily:      ipfamily.IPv4,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"},
				prefixes:      []string{"192.168.100.0/24", "192.169.101.0/24"},
				modifyPeers: func(ppV4 []config.Peer, _ []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					ppV4[0].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					for i := range ppV4[1:] {
						ppV4[i+1].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
							{Prefix: "192.168.2.0/24"},
							{Prefix: "192.169.2.0/24"},
						}

						ppV4[i+1].Neigh.ToAdvertise.Allowed.Prefixes = []string{"192.168.100.0/24"}
					}
				},
				validate: func(ppV4 []config.Peer, _ []config.Peer, pods []*v1.Pod, nodes []v1.Node) {
					ValidateNodesHaveRoutes(pods, ppV4[0].FRR, []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"}...)
					ValidatePrefixesForNeighbor(ppV4[0].FRR, nodes, "192.168.100.0/24", "192.169.101.0/24")
					for _, p := range ppV4[1:] {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.100.0/24")
						ValidateNeighborNoPrefixes(p.FRR, nodes, "192.168.101.0/24")
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"192.170.2.0/24"}...)
					}
				},
				splitCfg: splitByNeighReceiveAndAdvertise,
			}),
			ginkgo.Entry("IPV6 - One config for advertising, the other for receiving", params{
				ipFamily:      ipfamily.IPv6,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV6: []string{"fc00:f853:ccd:e899::/64", "fc00:f853:ccd:e900::/64", "fc00:f853:ccd:e901::/64"},
				prefixes:      []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(_ []config.Peer, ppV6 []config.Peer) {
					ppV6[0].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					ppV6[0].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					for i := range ppV6[1:] {
						ppV6[i+1].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
							{Prefix: "fc00:f853:ccd:e899::/64"},
							{Prefix: "fc00:f853:ccd:e900::/64"},
						}

						ppV6[i+1].Neigh.ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64"}
					}
				},
				validate: func(_ []config.Peer, ppV6 []config.Peer, pods []*v1.Pod, nodes []v1.Node) {
					ValidateNodesHaveRoutes(pods, ppV6[0].FRR, []string{"fc00:f853:ccd:e899::/64", "fc00:f853:ccd:e900::/64", "fc00:f853:ccd:e901::/64"}...)
					ValidatePrefixesForNeighbor(ppV6[0].FRR, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
					for _, p := range ppV6[1:] {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "fc00:f853:ccd:e799::/64")
						ValidateNeighborNoPrefixes(p.FRR, nodes, "fc00:f853:ccd:e800::/64")
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e899::/64", "fc00:f853:ccd:e900::/64"}...)
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e901::/64"}...)
					}
				},
				splitCfg: splitByNeighReceiveAndAdvertise,
			}),
			ginkgo.Entry("DUALSTACK - One config for advertising, the other for receiving", params{
				ipFamily:      ipfamily.DualStack,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"},
				toAdvertiseV6: []string{"fc00:f853:ccd:e899::/64", "fc00:f853:ccd:e900::/64", "fc00:f853:ccd:e901::/64"},
				prefixes:      []string{"192.168.100.0/24", "192.169.101.0/24", "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					ppV4[0].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					for i := range ppV4[1:] {
						ppV4[i+1].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
							{Prefix: "192.168.2.0/24"},
							{Prefix: "192.169.2.0/24"},
						}

						ppV4[i+1].Neigh.ToAdvertise.Allowed.Prefixes = []string{"192.168.100.0/24"}
					}

					ppV6[0].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					ppV6[0].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					for i := range ppV6[1:] {
						ppV6[i+1].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
							{Prefix: "fc00:f853:ccd:e899::/64"},
							{Prefix: "fc00:f853:ccd:e900::/64"},
						}

						ppV6[i+1].Neigh.ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64"}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod, nodes []v1.Node) {
					ValidateNodesHaveRoutes(pods, ppV4[0].FRR, []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"}...)
					ValidatePrefixesForNeighbor(ppV4[0].FRR, nodes, "192.168.100.0/24", "192.169.101.0/24")
					for _, p := range ppV4[1:] {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.100.0/24")
						ValidateNeighborNoPrefixes(p.FRR, nodes, "192.168.101.0/24")
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"192.170.2.0/24"}...)
					}

					ValidateNodesHaveRoutes(pods, ppV6[0].FRR, []string{"fc00:f853:ccd:e899::/64", "fc00:f853:ccd:e900::/64", "fc00:f853:ccd:e901::/64"}...)
					ValidatePrefixesForNeighbor(ppV6[0].FRR, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
					for _, p := range ppV6[1:] {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "fc00:f853:ccd:e799::/64")
						ValidateNeighborNoPrefixes(p.FRR, nodes, "fc00:f853:ccd:e800::/64")
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e899::/64", "fc00:f853:ccd:e900::/64"}...)
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e901::/64"}...)
					}
				},
				splitCfg: splitByNeighReceiveAndAdvertise,
			}),
		)
	})
})
