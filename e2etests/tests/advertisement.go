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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clientset "k8s.io/client-go/kubernetes"
)

var _ = ginkgo.Describe("Advertisement", func() {
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

	ginkgo.Context("Advertising IPs", func() {
		type params struct {
			vrf         string
			ipFamily    ipfamily.Family
			myAsn       uint32
			prefixes    []string
			modifyPeers func([]config.Peer, []config.Peer)
			validate    func([]config.Peer, []config.Peer, []v1.Node)
			splitCfg    func(frrk8sv1beta1.FRRConfiguration) ([]frrk8sv1beta1.FRRConfiguration, error)
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
				err := container.PairWithNodes(cs, c, p.ipFamily)
				Expect(err).NotTo(HaveOccurred())
			}

			err := updater.Update(peersConfig.Secrets, config)
			Expect(err).NotTo(HaveOccurred())

			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())

			for _, c := range frrs {
				ValidateFRRPeeredWithNodes(nodes, c, p.ipFamily)
			}

			ginkgo.By("validating")
			p.validate(peersConfig.PeersV4, peersConfig.PeersV6, nodes)

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
			p.validate(peersConfig.PeersV4, peersConfig.PeersV6, nodes)
		},
			ginkgo.Entry("IPV4 - Advertise with mode allowall", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.2.0/24", "192.169.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV4 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.2.0/24", "192.169.2.0/24")
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV4 - Advertise a subset of ips", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.2.0/24", "192.169.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToAdvertise.Allowed.Prefixes = []string{"192.168.2.0/24"}
					for i := 1; i < len(ppV4); i++ {
						ppV4[i].Neigh.ToAdvertise.Allowed.Prefixes = []string{"192.168.2.0/24", "192.169.2.0/24"}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					ValidatePrefixesForNeighbor(ppV4[0].FRR, nodes, "192.168.2.0/24")
					ValidateNeighborNoPrefixes(ppV4[0].FRR, nodes, "192.169.2.0/24")
					for _, p := range ppV4[1:] {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.2.0/24", "192.169.2.0/24")
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV6 - Advertise with mode allowall", params{
				ipFamily: ipfamily.IPv6,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV6 {
						ppV6[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV6 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV6 - Advertise a subset of ips", params{
				ipFamily: ipfamily.IPv6,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV6[0].Neigh.ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64"}
					for i := 1; i < len(ppV6); i++ {
						ppV6[i].Neigh.ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					ValidatePrefixesForNeighbor(ppV6[0].FRR, nodes, "fc00:f853:ccd:e799::/64")
					ValidateNeighborNoPrefixes(ppV6[0].FRR, nodes, "fc00:f853:ccd:e800::/64")
					for _, p := range ppV6[1:] {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV4 - VRF - Advertise with mode allowall", params{
				ipFamily: ipfamily.IPv4,
				vrf:      infra.VRFName,
				myAsn:    infra.FRRK8sASNVRF,
				prefixes: []string{"192.168.2.0/24", "192.169.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV4 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.2.0/24", "192.169.2.0/24")
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV4 - VRF - Advertise a subset of ips", params{
				ipFamily: ipfamily.IPv4,
				vrf:      infra.VRFName,
				myAsn:    infra.FRRK8sASNVRF,
				prefixes: []string{"192.168.2.0/24", "192.169.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToAdvertise.Allowed.Prefixes = []string{"192.168.2.0/24"}
					for i := 1; i < len(ppV4); i++ {
						ppV4[i].Neigh.ToAdvertise.Allowed.Prefixes = []string{"192.168.2.0/24", "192.169.2.0/24"}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					ValidatePrefixesForNeighbor(ppV4[0].FRR, nodes, "192.168.2.0/24")
					ValidateNeighborNoPrefixes(ppV4[0].FRR, nodes, "192.169.2.0/24")
					for _, p := range ppV4[1:] {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.2.0/24", "192.169.2.0/24")
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV6 - Advertise a subset of ips", params{
				ipFamily: ipfamily.IPv6,
				vrf:      infra.VRFName,
				myAsn:    infra.FRRK8sASNVRF,
				prefixes: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV6[0].Neigh.ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64"}
					for i := 1; i < len(ppV6); i++ {
						ppV6[i].Neigh.ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					ValidatePrefixesForNeighbor(ppV6[0].FRR, nodes, "fc00:f853:ccd:e799::/64")
					ValidateNeighborNoPrefixes(ppV6[0].FRR, nodes, "fc00:f853:ccd:e800::/64")
					for _, p := range ppV6[1:] {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("DUALSTACK - Advertise with mode allowall", params{
				ipFamily: ipfamily.DualStack,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.2.0/24", "192.169.2.0/24", "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
					for i := range ppV6 {
						ppV6[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV4 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.2.0/24", "192.169.2.0/24")
					}
					for _, p := range ppV6 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("DUALSTACK - Advertise a subset of ips", params{
				ipFamily: ipfamily.DualStack,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.2.0/24", "192.169.2.0/24", "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToAdvertise.Allowed.Prefixes = []string{"192.168.2.0/24"}
					for i := 1; i < len(ppV4); i++ {
						ppV4[i].Neigh.ToAdvertise.Allowed.Prefixes = []string{"192.169.2.0/24"}
					}
					ppV6[0].Neigh.ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64"}
					for i := 1; i < len(ppV6); i++ {
						ppV6[i].Neigh.ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e800::/64"}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					ValidatePrefixesForNeighbor(ppV4[0].FRR, nodes, "192.168.2.0/24")
					ValidateNeighborNoPrefixes(ppV4[0].FRR, nodes, "192.169.2.0/24")
					for _, p := range ppV4[1:] {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.169.2.0/24")
						ValidateNeighborNoPrefixes(p.FRR, nodes, "192.168.2.0/24")
					}
					ValidatePrefixesForNeighbor(ppV6[0].FRR, nodes, "fc00:f853:ccd:e799::/64")
					ValidateNeighborNoPrefixes(ppV6[0].FRR, nodes, "fc00:f853:ccd:e800::/64")
					for _, p := range ppV6[1:] {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "fc00:f853:ccd:e800::/64")
						ValidateNeighborNoPrefixes(p.FRR, nodes, "fc00:f853:ccd:e799::/64")
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV4 - Advertise with communities, AllowedMode all", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.0.0/24", "192.168.1.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV4[i].Neigh.ToAdvertise.PrefixesWithCommunity = []frrk8sv1beta1.CommunityPrefixes{
							{
								Community: "10:100",
								Prefixes:  []string{"192.168.0.0/24"},
							},
							{
								Community: "10:101",
								Prefixes:  []string{"192.168.1.0/24"},
							},
						}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV4 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.0.0/24", "192.168.1.0/24")
						ValidateNeighborCommunityPrefixes(p.FRR, "10:100", []string{"192.168.0.0"}, ipfamily.IPv4)
						ValidateNeighborCommunityPrefixes(p.FRR, "10:101", []string{"192.168.1.0"}, ipfamily.IPv4)
					}
				},
				splitCfg: splitByCommunities,
			}),
			ginkgo.Entry("IPV4 - Advertise with communities, AllowedMode restricted", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.0.0/24", "192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowRestricted
						ppV4[i].Neigh.ToAdvertise.Allowed.Prefixes = []string{"192.168.0.0/24", "192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24"}
						ppV4[i].Neigh.ToAdvertise.PrefixesWithCommunity = []frrk8sv1beta1.CommunityPrefixes{
							{
								Community: "10:100",
								Prefixes:  []string{"192.168.0.0/24"},
							},
							{
								Community: "10:101",
								Prefixes:  []string{"192.168.0.0/24", "192.168.1.0/24"},
							},
						}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV4 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.0.0/24", "192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24")
						ValidateNeighborCommunityPrefixes(p.FRR, "10:100", []string{"192.168.0.0"}, ipfamily.IPv4)
						ValidateNeighborCommunityPrefixes(p.FRR, "10:101", []string{"192.168.0.0", "192.168.1.0"}, ipfamily.IPv4)
					}
				},
				splitCfg: splitByCommunities,
			}),
			ginkgo.Entry("IPV4 - Advertise with localPref, AllowedMode all", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.0.0/24", "192.168.1.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV4[i].Neigh.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{
							{
								Prefixes:  []string{"192.168.0.0/24"},
								LocalPref: 100,
							},
							{
								Prefixes:  []string{"192.168.1.0/24"},
								LocalPref: 150,
							},
						}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV4 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.0.0/24", "192.168.1.0/24")
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.0.0", 100, ipfamily.IPv4)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.1.0", 150, ipfamily.IPv4)
					}
				},
				splitCfg: splitByLocalPref,
			}),
			ginkgo.Entry("IPV4 - Advertise with localPref, AllowedMode restricted", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.0.0/24", "192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowRestricted
						ppV4[i].Neigh.ToAdvertise.Allowed.Prefixes = []string{"192.168.0.0/24", "192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24"}
						ppV4[i].Neigh.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{
							{
								Prefixes:  []string{"192.168.0.0/24", "192.168.1.0/24"},
								LocalPref: 100,
							},
							{
								Prefixes:  []string{"192.168.2.0/24"},
								LocalPref: 150,
							},
						}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV4 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.0.0/24", "192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24")
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.0.0", 100, ipfamily.IPv4)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.1.0", 100, ipfamily.IPv4)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.2.0", 150, ipfamily.IPv4)
					}
				},
				splitCfg: splitByLocalPref,
			}),
			ginkgo.Entry("IPV4 - Advertise with communities and localPref, AllowedMode all", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.0.0/24", "192.168.1.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV4[i].Neigh.ToAdvertise.PrefixesWithCommunity = []frrk8sv1beta1.CommunityPrefixes{
							{
								Community: "10:100",
								Prefixes:  []string{"192.168.0.0/24"},
							},
							{
								Community: "10:101",
								Prefixes:  []string{"192.168.1.0/24"},
							},
						}
						ppV4[i].Neigh.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{
							{
								Prefixes:  []string{"192.168.0.0/24"},
								LocalPref: 100,
							},
							{
								Prefixes:  []string{"192.168.1.0/24"},
								LocalPref: 150,
							},
						}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV4 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.0.0/24", "192.168.1.0/24")
						ValidateNeighborCommunityPrefixes(p.FRR, "10:100", []string{"192.168.0.0"}, ipfamily.IPv4)
						ValidateNeighborCommunityPrefixes(p.FRR, "10:101", []string{"192.168.1.0"}, ipfamily.IPv4)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.0.0", 100, ipfamily.IPv4)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.1.0", 150, ipfamily.IPv4)
					}
				},
				splitCfg: splitByLocalPrefAndCommunities,
			}),
			ginkgo.Entry("IPV4 - Advertise with multiple communities and localPref, AllowedMode restricted", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.0.0/24", "192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowRestricted
						ppV4[i].Neigh.ToAdvertise.Allowed.Prefixes = []string{"192.168.0.0/24", "192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24"}
						ppV4[i].Neigh.ToAdvertise.PrefixesWithCommunity = []frrk8sv1beta1.CommunityPrefixes{
							{
								Community: "10:100",
								Prefixes:  []string{"192.168.0.0/24"},
							},
							{
								Community: "10:101",
								Prefixes:  []string{"192.168.0.0/24", "192.168.1.0/24"},
							},
						}
						ppV4[i].Neigh.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{
							{
								Prefixes:  []string{"192.168.0.0/24", "192.168.1.0/24"},
								LocalPref: 100,
							},
							{
								Prefixes:  []string{"192.168.2.0/24"},
								LocalPref: 150,
							},
						}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV4 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.0.0/24", "192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24")
						ValidateNeighborCommunityPrefixes(p.FRR, "10:100", []string{"192.168.0.0"}, ipfamily.IPv4)
						ValidateNeighborCommunityPrefixes(p.FRR, "10:101", []string{"192.168.0.0", "192.168.1.0"}, ipfamily.IPv4)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.0.0", 100, ipfamily.IPv4)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.1.0", 100, ipfamily.IPv4)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.2.0", 150, ipfamily.IPv4)
					}
				},
				splitCfg: splitByLocalPrefAndCommunities,
			}),
			ginkgo.Entry("IPV4 - large community and localPref", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.0.0/24", "192.168.1.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV4[i].Neigh.ToAdvertise.PrefixesWithCommunity = []frrk8sv1beta1.CommunityPrefixes{
							{
								Community: "large:123:456:7890",
								Prefixes:  []string{"192.168.0.0/24"},
							},
						}
						ppV4[i].Neigh.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{
							{
								Prefixes:  []string{"192.168.0.0/24"},
								LocalPref: 100,
							},
						}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV4 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.0.0/24", "192.168.1.0/24")
						ValidateNeighborCommunityPrefixes(p.FRR, "large:123:456:7890", []string{"192.168.0.0"}, ipfamily.IPv4)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.0.0", 100, ipfamily.IPv4)
					}
				},
				splitCfg: splitByLocalPrefAndCommunities,
			}),
			ginkgo.Entry("IPV6 - Advertise with communities and localPref, AllowedMode all", params{
				ipFamily: ipfamily.IPv6,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV6 {
						ppV6[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV6[i].Neigh.ToAdvertise.PrefixesWithCommunity = []frrk8sv1beta1.CommunityPrefixes{
							{
								Community: "10:100",
								Prefixes:  []string{"fc00:f853:ccd:e799::/64"},
							},
							{
								Community: "10:101",
								Prefixes:  []string{"fc00:f853:ccd:e800::/64"},
							},
						}
						ppV6[i].Neigh.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{
							{
								Prefixes:  []string{"fc00:f853:ccd:e799::/64"},
								LocalPref: 100,
							},
							{
								Prefixes:  []string{"fc00:f853:ccd:e800::/64"},
								LocalPref: 150,
							},
						}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV6 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
						ValidateNeighborCommunityPrefixes(p.FRR, "10:100", []string{"fc00:f853:ccd:e799::"}, ipfamily.IPv6)
						ValidateNeighborCommunityPrefixes(p.FRR, "10:101", []string{"fc00:f853:ccd:e800::"}, ipfamily.IPv6)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "fc00:f853:ccd:e799::", 100, ipfamily.IPv6)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "fc00:f853:ccd:e800::", 150, ipfamily.IPv6)
					}
				},
				splitCfg: splitByLocalPrefAndCommunities,
			}),
			ginkgo.Entry("IPV6 - Advertise with multiple communities and localPref, AllowedMode restricted", params{
				ipFamily: ipfamily.IPv6,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64", "fc00:f853:ccd:e801::/64", "fc00:f853:ccd:e802::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV6 {
						ppV6[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowRestricted
						ppV6[i].Neigh.ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64", "fc00:f853:ccd:e801::/64"}
						ppV6[i].Neigh.ToAdvertise.PrefixesWithCommunity = []frrk8sv1beta1.CommunityPrefixes{
							{
								Community: "10:100",
								Prefixes:  []string{"fc00:f853:ccd:e799::/64"},
							},
							{
								Community: "10:101",
								Prefixes:  []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
							},
						}
						ppV6[i].Neigh.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{
							{
								Prefixes:  []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
								LocalPref: 100,
							},
							{
								Prefixes:  []string{"fc00:f853:ccd:e801::/64"},
								LocalPref: 150,
							},
						}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV6 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64", "fc00:f853:ccd:e801::/64")
						ValidateNeighborCommunityPrefixes(p.FRR, "10:100", []string{"fc00:f853:ccd:e799::"}, ipfamily.IPv6)
						ValidateNeighborCommunityPrefixes(p.FRR, "10:101", []string{"fc00:f853:ccd:e799::", "fc00:f853:ccd:e800::"}, ipfamily.IPv6)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "fc00:f853:ccd:e799::", 100, ipfamily.IPv6)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "fc00:f853:ccd:e800::", 100, ipfamily.IPv6)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "fc00:f853:ccd:e801::", 150, ipfamily.IPv6)
					}
				},
				splitCfg: splitByLocalPrefAndCommunities,
			}),
			ginkgo.Entry("IPV6 - large community and localPref", params{
				ipFamily: ipfamily.IPv6,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV6 {
						ppV6[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV6[i].Neigh.ToAdvertise.PrefixesWithCommunity = []frrk8sv1beta1.CommunityPrefixes{
							{
								Community: "large:123:456:7890",
								Prefixes:  []string{"fc00:f853:ccd:e799::/64"},
							},
						}
						ppV6[i].Neigh.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{
							{
								Prefixes:  []string{"fc00:f853:ccd:e799::/64"},
								LocalPref: 100,
							},
						}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV6 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
						ValidateNeighborCommunityPrefixes(p.FRR, "large:123:456:7890", []string{"fc00:f853:ccd:e799::"}, ipfamily.IPv6)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "fc00:f853:ccd:e799::", 100, ipfamily.IPv6)
					}
				},
				splitCfg: splitByLocalPrefAndCommunities,
			}),
			ginkgo.Entry("DUALSTACK - Advertise with communities and localPref, AllowedMode all", params{
				ipFamily: ipfamily.DualStack,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.2.0/24", "192.169.2.0/24", "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV4[i].Neigh.ToAdvertise.PrefixesWithCommunity = []frrk8sv1beta1.CommunityPrefixes{
							{
								Community: "10:100",
								Prefixes:  []string{"192.168.2.0/24"},
							},
							{
								Community: "large:123:456:7890",
								Prefixes:  []string{"192.168.2.0/24"},
							},
							{
								Community: "10:101",
								Prefixes:  []string{"192.168.2.0/24", "192.169.2.0/24"},
							},
						}
						ppV4[i].Neigh.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{
							{
								Prefixes:  []string{"192.168.2.0/24"},
								LocalPref: 100,
							},
						}
					}
					for i := range ppV6 {
						ppV6[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV6[i].Neigh.ToAdvertise.PrefixesWithCommunity = []frrk8sv1beta1.CommunityPrefixes{
							{
								Community: "10:100",
								Prefixes:  []string{"fc00:f853:ccd:e799::/64"},
							},
							{
								Community: "large:123:456:7890",
								Prefixes:  []string{"fc00:f853:ccd:e799::/64"},
							},
							{
								Community: "10:101",
								Prefixes:  []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
							},
						}
						ppV6[i].Neigh.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{
							{
								Prefixes:  []string{"fc00:f853:ccd:e799::/64"},
								LocalPref: 100,
							},
						}
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, nodes []v1.Node) {
					for _, p := range ppV4 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "192.168.2.0/24", "192.169.2.0/24")
						ValidateNeighborCommunityPrefixes(p.FRR, "10:100", []string{"192.168.2.0"}, ipfamily.IPv4)
						ValidateNeighborCommunityPrefixes(p.FRR, "large:123:456:7890", []string{"192.168.2.0"}, ipfamily.IPv4)
						ValidateNeighborCommunityPrefixes(p.FRR, "10:101", []string{"192.168.2.0", "192.169.2.0"}, ipfamily.IPv4)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "192.168.2.0", 100, ipfamily.IPv4)
					}
					for _, p := range ppV6 {
						ValidatePrefixesForNeighbor(p.FRR, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
						ValidateNeighborCommunityPrefixes(p.FRR, "10:100", []string{"fc00:f853:ccd:e799::"}, ipfamily.IPv6)
						ValidateNeighborCommunityPrefixes(p.FRR, "large:123:456:7890", []string{"fc00:f853:ccd:e799::"}, ipfamily.IPv6)
						ValidateNeighborCommunityPrefixes(p.FRR, "10:101", []string{"fc00:f853:ccd:e799::", "fc00:f853:ccd:e800::"}, ipfamily.IPv6)
						ValidateNeighborLocalPrefForPrefix(p.FRR, "fc00:f853:ccd:e799::", 100, ipfamily.IPv6)
					}
				},
				splitCfg: splitByLocalPrefAndCommunities,
			}),
		)
	})
})
