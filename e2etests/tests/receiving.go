// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"github.com/onsi/ginkgo/v2"
	"go.universe.tf/e2etest/pkg/frr/container"

	frrk8sv1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	"go.universe.tf/e2etest/pkg/ipfamily"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	admissionapi "k8s.io/pod-security-admission/api"
)

var _ = ginkgo.Describe("Receiving routes", func() {
	var cs clientset.Interface
	var f *framework.Framework

	defer ginkgo.GinkgoRecover()
	clientconfig, err := framework.LoadConfig()
	framework.ExpectNoError(err)
	updater, err := config.NewUpdater(clientconfig)
	framework.ExpectNoError(err)
	reporter := dump.NewK8sReporter(framework.TestContext.KubeConfig, k8s.FRRK8sNamespace)

	f = framework.NewDefaultFramework("bgpfrr")
	f.NamespacePodSecurityEnforceLevel = admissionapi.LevelPrivileged

	ginkgo.AfterEach(func() {
		if ginkgo.CurrentSpecReport().Failed() {
			testName := ginkgo.CurrentSpecReport().LeafNodeText
			dump.K8sInfo(testName, reporter)
			dump.BGPInfo(testName, infra.FRRContainers, f.ClientSet, f)
		}
	})

	ginkgo.BeforeEach(func() {
		ginkgo.By("Clearing any previous configuration")

		for _, c := range infra.FRRContainers {
			err := c.UpdateBGPConfigFile(frrconfig.Empty)
			framework.ExpectNoError(err)
		}
		err := updater.Clean()
		framework.ExpectNoError(err)

		cs = f.ClientSet
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
		}

		ginkgo.DescribeTable("Works with external frrs", func(p params) {
			frrs := config.ContainersForVRF(infra.FRRContainers, p.vrf)
			peersV4, peersV6 := config.PeersForContainers(frrs, p.ipFamily)
			p.modifyPeers(peersV4, peersV6)
			neighbors := config.NeighborsFromPeers(peersV4, peersV6)

			config := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
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
				framework.ExpectNoError(err)
			}
			err := updater.Update(config)
			framework.ExpectNoError(err)

			nodes, err := k8s.Nodes(cs)
			framework.ExpectNoError(err)

			for _, c := range frrs {
				ValidateFRRPeeredWithNodes(nodes, c, p.ipFamily)
			}

			pods, err := k8s.FRRK8sPods(cs)
			framework.ExpectNoError(err)

			ginkgo.By("validating")
			p.validate(peersV4, peersV6, pods)
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
			}),
			ginkgo.Entry("IPV4 - receive ips from some, explicit mode", params{
				ipFamily:      ipfamily.IPv4,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Prefixes = []string{"192.168.2.0/24", "192.169.2.0/24"}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					ValidateNodesHaveRoutes(pods, ppV4[0].FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
					ValidateNodesDoNotHaveRoutes(pods, ppV4[0].FRR, []string{"192.170.2.0/24"}...)
					for _, p := range ppV4[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"}...)
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
			}),
			ginkgo.Entry("IPV6 - receive ips from some, explicit mode", params{
				ipFamily:      ipfamily.IPv6,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV6: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV6[0].Neigh.ToReceive.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64"}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					ValidateNodesHaveRoutes(pods, ppV6[0].FRR, []string{"fc00:f853:ccd:e799::/64"}...)
					ValidateNodesDoNotHaveRoutes(pods, ppV6[0].FRR, []string{"fc00:f853:ccd:e800::/64"}...)
					for _, p := range ppV6[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"}...)
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
			}),
			ginkgo.Entry("IPV4 - VRF - receive ips from some, explicit mode", params{
				ipFamily:      ipfamily.IPv4,
				vrf:           infra.VRFName,
				myAsn:         infra.FRRK8sASNVRF,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Prefixes = []string{"192.168.2.0/24", "192.169.2.0/24"}
				},
				validate: func(ppV4 []config.Peer, ppV6 []config.Peer, pods []*v1.Pod) {
					ValidateNodesHaveRoutes(pods, ppV4[0].FRR, []string{"192.168.2.0/24", "192.169.2.0/24"}...)
					ValidateNodesDoNotHaveRoutes(pods, ppV4[0].FRR, []string{"192.170.2.0/24"}...)
					for _, p := range ppV4[1:] {
						ValidateNodesDoNotHaveRoutes(pods, p.FRR, []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"}...)
					}
				},
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
			}),
			ginkgo.Entry("DUALSTACK - receive ips from some, explicit mode", params{
				ipFamily:      ipfamily.DualStack,
				vrf:           "",
				myAsn:         infra.FRRK8sASN,
				toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24"},
				toAdvertiseV6: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					ppV4[0].Neigh.ToReceive.Allowed.Prefixes = []string{"192.169.2.0/24"}
					ppV6[0].Neigh.ToReceive.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64"}
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
			}),
		)
	})
})
