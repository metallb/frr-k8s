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

var _ = ginkgo.Describe("Receiving routes", func() {
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

	ginkgo.Context("Receiving IPs", func() {
		type params struct {
			vrf           string
			ipFamily      ipfamily.Family
			myAsn         uint32
			toAdvertiseV4 []string
			toAdvertiseV6 []string
			modifyPeers   func([]config.Peer, []config.Peer)
			validate      func([]config.Peer, []config.Peer, []*v1.Pod)
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
			p.validate(peersConfig.PeersV4, peersConfig.PeersV6, pods)

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
			p.validate(peersConfig.PeersV4, peersConfig.PeersV6, pods)
		},
			ginkgo.Entry("IPV4 - receive ips from all", params{
				ipFamily:      ipfamily.IPv4,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					for _, p := range ppV4 {
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV4 - receive ips from some, all mode", params{
				ipFamily:      ipfamily.IPv4,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					ValidateNodesHaveRoutes(pods, ppV4[0].FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
					for _, p := range ppV4[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV4 - receive ips from some, explicit mode", params{
				ipFamily:      ipfamily.IPv4,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
						{Prefix: "192.168.2.0/24"},
						{Prefix: "192.169.2.0/24"}}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					ValidateNodesHaveRoutes(pods, ppV4[0].FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
					ValidateNodesDoNotHaveRoutes(pods, ppV4[0].FRR, []string{"192.170.2.0/24"}...)
					for _, p := range ppV4[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"}...)
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV4 - receive ips from some, explicit mode, selectors", params{
				ipFamily:      ipfamily.IPv4,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.1/32", "192.169.2.0/24", "192.170.2.1/32", "192.171.2.0/24", "192.171.2.1/32"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
						{Prefix: "192.168.2.0/24", LE: 32},
						{Prefix: "192.169.2.0/24", LE: 24},
						{Prefix: "192.171.2.0/24", GE: 28, LE: 31},
						{Prefix: "192.171.2.0/24", LE: 25}}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					ValidateNodesHaveRoutes(pods, ppV4[0].FRR, []string{"192.168.2.1/32", "192.169.2.0/24", "192.171.2.0/24"}...)
					ValidateNodesDoNotHaveRoutes(pods, ppV4[0].FRR, []string{"192.170.2.1/32", "192.171.2.1/32"}...)
					for _, p := range ppV4[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"192.168.2.1/32", "192.169.2.0/24", "192.170.2.1/32"}...)
					}
				},
			}),
			ginkgo.Entry("IPV6 - receive ips from all", params{
				ipFamily:      ipfamily.IPv6,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV6: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
					for i := range ppV6 {
						ppV6[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					for _, p := range ppV6 {
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"}...)
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV6 - receive ips from some, explicit mode", params{
				ipFamily:      ipfamily.IPv6,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV6: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV6[0].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
						{Prefix: "fc00:f853:ccd:e799::/64"},
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					ValidateNodesHaveRoutes(pods, ppV6[0].FRR, []string{"fc00:f853:ccd:e799::/64"}...)
					ValidateNodesDoNotHaveRoutes(pods, ppV6[0].FRR, []string{"fc00:f853:ccd:e800::/64"}...)
					for _, p := range ppV6[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"}...)
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV6 - receive ips from some, explicit mode, selectors", params{
				ipFamily:      ipfamily.IPv6,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV6: []string{"fc00:f853:ccd:e799::1/128", "fc00:f853:ccd:e800::/64", "fc00:f853:ccd:e801::1/128"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV6[0].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
						{Prefix: "fc00:f853:ccd:e799::/64", LE: 128},
						{Prefix: "fc00:f853:ccd:e800::/64", LE: 64},
						{Prefix: "fc00:f853:ccd:e801::/64", LE: 64},
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					ValidateNodesHaveRoutes(pods, ppV6[0].FRR, []string{"fc00:f853:ccd:e799::1/128", "fc00:f853:ccd:e800::/64"}...)
					ValidateNodesDoNotHaveRoutes(pods, ppV6[0].FRR, []string{"fc00:f853:ccd:e801::1/128"}...)
					for _, p := range ppV6[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e799::1/128", "fc00:f853:ccd:e800::/64", "fc00:f853:ccd:e801::1/128"}...)
					}
				},
			}),
			ginkgo.Entry("IPV4 - VRF - receive ips from some, all mode", params{
				ipFamily:      ipfamily.IPv4,
				vrf:           infra.VRFName,
				myAsn:         infra.FRRK8sASNVRF,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					ValidateNodesHaveRoutes(pods, ppV4[0].FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
					for _, p := range ppV4[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV4 - VRF - receive ips from some, explicit mode", params{
				ipFamily:      ipfamily.IPv4,
				vrf:           infra.VRFName,
				myAsn:         infra.FRRK8sASNVRF,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
						{Prefix: "192.168.2.0/24"},
						{Prefix: "192.169.2.0/24"},
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					ValidateNodesHaveRoutes(pods, ppV4[0].FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
					ValidateNodesDoNotHaveRoutes(pods, ppV4[0].FRR, []string{"192.170.2.0/24"}...)
					for _, p := range ppV4[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"}...)
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV6 - VRF - receive ips from all", params{
				ipFamily:      ipfamily.IPv6,
				vrf:           infra.VRFName,
				myAsn:         infra.FRRK8sASNVRF,
				toAdvertiseV6: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
					for i := range ppV6 {
						ppV6[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					for _, p := range ppV6 {
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"}...)
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("DUALSTACK - receive ips from all", params{
				ipFamily:      ipfamily.DualStack,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24"},
				toAdvertiseV6: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
					for i := range ppV6 {
						ppV6[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},

				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					for _, p := range ppV4 {
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
					}
					for _, p := range ppV6 {
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"}...)
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("DUALSTACK - receive ips from some, explicit mode", params{
				ipFamily:      ipfamily.DualStack,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24"},
				toAdvertiseV6: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
						{Prefix: "192.169.2.0/24"},
					}
					ppV6[0].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
						{Prefix: "fc00:f853:ccd:e799::/64"},
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					ValidateNodesHaveRoutes(pods, ppV4[0].FRR, []string{"192.169.2.0/24"}...)
					ValidateNodesDoNotHaveRoutes(pods, ppV4[0].FRR, []string{"192.168.2.0/24"}...)
					for _, p := range ppV4[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
					}
					ValidateNodesHaveRoutes(pods, ppV6[0].FRR, []string{"fc00:f853:ccd:e799::/64"}...)
					ValidateNodesDoNotHaveRoutes(pods, ppV6[0].FRR, []string{"fc00:f853:ccd:e800::/64"}...)
					for _, p := range ppV6[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"}...)
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV4 - receive ips from all, should block always block cidr", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				// the first two ips are being set via the alwaysBlock parameter when launching
				// make deploy / deploy-helm and should be blocked
				toAdvertiseV4: []string{"192.167.9.0/24", "192.167.9.3/32", "192.168.1.2/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					for _, p := range ppV4 {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, "192.167.9.0/24", "192.167.9.3/32")
						ValidateNodesHaveRoutes(pods, p.FRR, "192.168.1.2/24")
					}
				},
				splitCfg: splitByNeigh,
			}),
			ginkgo.Entry("IPV6 - receive ips from all, should block always block cidr", params{
				ipFamily: ipfamily.IPv6,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				// the first two ips are being set via the alwaysBlock parameter when launching
				// make deploy / deploy-helm and should be blocked
				toAdvertiseV6: []string{"fc00:f553:ccd:e799::/64", "fc00:f553:ccd:e799::1/128", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV6 {
						ppV6[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					for _, p := range ppV6 {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, "fc00:f553:ccd:e799::/64", "fc00:f553:ccd:e799::1/128")
						ValidateNodesHaveRoutes(pods, p.FRR, "fc00:f853:ccd:e800::/64")
					}
				},
				splitCfg: splitByNeigh,
			}),
		)

		ginkgo.DescribeTable("Multiple configs", func(p params) {
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

			cfgs, err := p.splitCfg(config)
			Expect(err).NotTo(HaveOccurred())

			err = updater.Update(peersConfig.Secrets, cfgs...)
			Expect(err).NotTo(HaveOccurred())

			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())

			for _, c := range frrs {
				ValidateFRRPeeredWithNodes(nodes, c, p.ipFamily)
			}

			pods, err := k8s.FRRK8sPods(cs)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("validating")
			p.validate(peersConfig.PeersV4, peersConfig.PeersV6, pods)
		},
			ginkgo.Entry("IPV4 - AllowMode=All should override explicit", params{
				ipFamily:      ipfamily.IPv4,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
						{Prefix: "192.168.2.0/24"},
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					for _, p := range ppV4 {
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"}...)
					}
				},
				splitCfg: duplicateNeighsWithReceiveAll,
			}),
			ginkgo.Entry("IPV6 - AllowMode=All should override explicit", params{
				ipFamily:      ipfamily.IPv6,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV6: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64", "fc00:f853:ccd:e801::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV6[0].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
						{Prefix: "fc00:f853:ccd:e799::/64"},
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					for _, p := range ppV6 {
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64", "fc00:f853:ccd:e801::/64"}...)
					}
				},
				splitCfg: duplicateNeighsWithReceiveAll,
			}),
			ginkgo.Entry("DUALSTACK - AllowMode=All should override explicit", params{
				ipFamily:      ipfamily.DualStack,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"},
				toAdvertiseV6: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64", "fc00:f853:ccd:e801::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
						{Prefix: "192.168.2.0/24"},
					}
					ppV6[0].Neigh.ToReceive.Allowed.Prefixes = []frrk8sv1beta1.PrefixSelector{
						{Prefix: "fc00:f853:ccd:e799::/64"},
					}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					for _, p := range ppV4 {
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"}...)
					}
					for _, p := range ppV6 {
						ValidateNodesHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64", "fc00:f853:ccd:e801::/64"}...)
					}
				},
				splitCfg: duplicateNeighsWithReceiveAll,
			}),
		)
	})
})
