// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"github.com/onsi/ginkgo/v2"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/frr"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	. "github.com/onsi/gomega"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = ginkgo.Describe("AllowAsIn", func() {
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
			err := c.UpdateConfigFile(frrconfig.Empty)
			Expect(err).NotTo(HaveOccurred())
		}
		err := updater.Clean()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
	})

	type params struct {
		ipFamily     ipfamily.Family
		allowAsIn    frrk8sv1beta1.AllowAsInMode
		advertiseV4  []string
		advertiseV6  []string
		expectRoutes bool
	}

	ginkgo.DescribeTable("routes with local AS in path", func(p params) {
		ebgpContainer := infra.FindContainer("ebgp-single-hop")
		Expect(ebgpContainer).NotTo(BeNil(), "ebgp-single-hop container not found")

		frrs := []*frrcontainer.FRR{ebgpContainer}
		peersConfig := config.PeersForContainers(frrs, p.ipFamily, config.EnableReceiveAllowAll, config.EnableAllowAsIn(p.allowAsIn), config.EnableDualStackAddressFamily)
		neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)

		frrCfg := frrk8sv1beta1.FRRConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-allowasin",
				Namespace: k8s.FRRK8sNamespace,
			},
			Spec: frrk8sv1beta1.FRRConfigurationSpec{
				BGP: frrk8sv1beta1.BGPConfig{
					Routers: []frrk8sv1beta1.Router{
						{
							ASN:       infra.FRRK8sASN,
							Neighbors: neighbors,
						},
					},
				},
			},
		}

		ginkgo.By("Pairing external FRR with nodes using AS path prepend")
		err := frr.PairWithNodesWithPrependASN(ebgpContainer, cs, infra.FRRK8sASN, p.ipFamily,
			func(c *frrcontainer.FRR) {
				c.NeighborConfig.ToAdvertiseV4 = p.advertiseV4
				c.NeighborConfig.ToAdvertiseV6 = p.advertiseV6
			},
		)
		Expect(err).NotTo(HaveOccurred())

		ginkgo.By("Applying FRRConfiguration")
		err = updater.Update(peersConfig.Secrets, frrCfg)
		Expect(err).NotTo(HaveOccurred())

		k8sNodes, err := k8s.Nodes(cs)
		Expect(err).NotTo(HaveOccurred())

		ginkgo.By("Validating BGP sessions are established")
		ValidateFRRPeeredWithNodes(k8sNodes, ebgpContainer, p.ipFamily)

		pods, err := k8s.FRRK8sPods(cs)
		Expect(err).NotTo(HaveOccurred())

		allPrefixes := append(p.advertiseV4, p.advertiseV6...)
		if p.expectRoutes {
			ginkgo.By("Validating nodes received the routes with local AS in path")
			ValidateNodesHaveRoutes(pods, *ebgpContainer, allPrefixes...)
		} else {
			ginkgo.By("Validating nodes rejected the routes with local AS in path")
			ValidateNodesDoNotHaveRoutes(pods, *ebgpContainer, allPrefixes...)
		}
	},
		ginkgo.Entry("should be accepted with AllowAsIn origin", params{
			ipFamily:     ipfamily.DualStack,
			allowAsIn:    frrk8sv1beta1.AllowAsInOrigin,
			advertiseV4:  []string{"192.168.100.0/24"},
			advertiseV6:  []string{"fc00:100::/64"},
			expectRoutes: true,
		}),
		ginkgo.Entry("should be rejected without AllowAsIn", params{
			ipFamily:     ipfamily.DualStack,
			advertiseV4:  []string{"192.168.100.0/24"},
			advertiseV6:  []string{"fc00:100::/64"},
			expectRoutes: false,
		}),
	)
})
