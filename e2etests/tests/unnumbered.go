// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/google/go-cmp/cmp"
	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.universe.tf/e2etest/pkg/container"
	"go.universe.tf/e2etest/pkg/executor"
	"go.universe.tf/e2etest/pkg/frr"
	"go.universe.tf/e2etest/pkg/netdev"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"

	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
)

var FRRImage string

// The Unnumbered test can run on IPv4, IPv6 or Dual because it needs only the
// IPv6 LLA addresses in the interface, not full IPv6 stack.
var _ = ginkgo.Describe("Unnumbered", func() {
	var (
		nodeWithP2PConnection corev1.Node
		externalP2PContainer  *frrcontainer.FRR
		cs                    clientset.Interface
		updater               *config.Updater
		prefixSend            = []string{"5.5.5.0/24", "5555::/64"}
	)

	ginkgo.BeforeEach(func() {
		var err error
		updater, err = config.NewUpdater()
		Expect(err).NotTo(HaveOccurred())

		err = updater.Clean()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		nodes, err := k8s.Nodes(cs)
		Expect(err).NotTo(HaveOccurred())
		nodeWithP2PConnection = nodes[0]

	})

	ginkgo.AfterEach(func() {
		if ginkgo.CurrentSpecReport().Failed() {
			reporter := dump.NewK8sReporter(k8s.FRRK8sNamespace)
			testName := ginkgo.CurrentSpecReport().FullText()
			dump.K8sInfo(testName, reporter)
			dump.BGPInfo(testName, []*frrcontainer.FRR{externalP2PContainer}, cs)
		}
	})

	ginkgo.DescribeTable("BGP session is established and routes are verified", func(rc frrconfig.RouterConfigUnnumbered) {
		iface := rc.Interface
		remoteASN := rc.ASNLocal

		ginkgo.By(fmt.Sprintf("creating p2p %s:%s -- %s:external-p2p-container", nodeWithP2PConnection.Name, iface, iface))
		externalP2PContainer, err := frrcontainer.CreateP2PPeerFor(nodeWithP2PConnection.Name, iface, FRRImage)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			err := frrcontainer.Delete([]*frrcontainer.FRR{externalP2PContainer})
			Expect(err).NotTo(HaveOccurred())
		})

		ginkgo.By(fmt.Sprintf("updating frrconfig to %s", externalP2PContainer.Name))
		c, err := rc.Config()
		Expect(err).NotTo(HaveOccurred())
		err = externalP2PContainer.UpdateBGPConfigFile(c)
		Expect(err).NotTo(HaveOccurred())

		ginkgo.By(fmt.Sprintf("peering node %s with its p2p container %s", nodeWithP2PConnection.Name, externalP2PContainer.Name))
		cr := frrk8sv1beta1.FRRConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "for-" + externalP2PContainer.Name,
				Namespace: k8s.FRRK8sNamespace,
			},
			Spec: frrk8sv1beta1.FRRConfigurationSpec{
				BGP: frrk8sv1beta1.BGPConfig{
					BFDProfiles: []frrk8sv1beta1.BFDProfile{{Name: "simple"}},
					Routers: []frrk8sv1beta1.Router{
						{
							ASN: infra.FRRK8sASN,
							Neighbors: []frrk8sv1beta1.Neighbor{
								{
									ASN:        remoteASN,
									Interface:  iface,
									BFDProfile: "simple",
									ToReceive: frrk8sv1beta1.Receive{
										Allowed: frrk8sv1beta1.AllowedInPrefixes{
											Mode: frrk8sv1beta1.AllowAll,
										},
									},
									ToAdvertise: frrk8sv1beta1.Advertise{
										Allowed: frrk8sv1beta1.AllowedOutPrefixes{
											Mode: frrk8sv1beta1.AllowAll,
										},
									},
								},
							},
							Prefixes: prefixSend,
						},
					},
				},
				NodeSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/hostname": nodeWithP2PConnection.GetLabels()["kubernetes.io/hostname"],
					},
				},
			},
		}
		err = updater.Update([]corev1.Secret{}, cr)
		Expect(err).NotTo(HaveOccurred())

		nodeP2PContainer := executor.ForContainer(nodeWithP2PConnection.Name)
		nodeLLA, err := netdev.LinkLocalAddressForDevice(nodeP2PContainer, iface)
		Expect(err).NotTo(HaveOccurred())
		peerLLA, err := netdev.LinkLocalAddressForDevice(externalP2PContainer, iface)
		Expect(err).NotTo(HaveOccurred())

		ginkgo.By("validating the node and p2p container peered")
		validateUnnumberedBGPPeering(externalP2PContainer, nodeLLA)

		ginkgo.By(fmt.Sprintf("validating the p2p peer %s received the routes from node", externalP2PContainer.Name))
		validatePrefixViaFor(externalP2PContainer, iface, nodeLLA, prefixSend)

		ginkgo.By(fmt.Sprintf("validating the node received %s received the routes from p2p peer", nodeWithP2PConnection.Name))
		validatePrefixViaFor(nodeP2PContainer, iface, peerLLA, rc.ToAdvertiseV4, rc.ToAdvertiseV6)
	},
		ginkgo.Entry("iBGP", frrconfig.RouterConfigUnnumbered{
			ASNLocal:      infra.FRRK8sASN,
			ASNRemote:     infra.FRRK8sASN,
			Hostname:      "tor1",
			Interface:     "net1",
			RouterID:      "1.1.1.1",
			ToAdvertiseV4: []string{"1.1.1.0/24"},
			ToAdvertiseV6: []string{"1111::/64"},
		}),
		ginkgo.Entry("eBGP", frrconfig.RouterConfigUnnumbered{
			ASNLocal:      4200000000,
			ASNRemote:     infra.FRRK8sASN,
			Hostname:      "tor2",
			Interface:     "net2",
			RouterID:      "2.2.2.2",
			ToAdvertiseV4: []string{"2.2.2.0/24"},
			ToAdvertiseV6: []string{"2222::/64"},
		}),
	)

})

func validateUnnumberedBGPPeering(peer *frrcontainer.FRR, nodeLLA string) {
	ginkgo.By(fmt.Sprintf("validating BGP peering to %s", peer.Name))
	Eventually(func() error {
		neighbors, err := frr.NeighborsInfo(peer)
		if err != nil {
			return err
		}
		for _, n := range neighbors {
			if n.BGPNeighborAddr == nodeLLA && n.Connected && n.BFDInfo.Status == "Up" {
				return nil
			}
		}
		return fmt.Errorf("no connected neighbor was found with %s", nodeLLA)
	}, 2*time.Minute, 10*time.Second).ShouldNot(HaveOccurred(),
		"timed out waiting to validate nodes peered with the frr instance")
}

// validatePrefixViaFor replaces the usual functions
// ValidatePrefixesForNeighbor(*externalP2PContainer, []corev1.Node{nodeWithP2PConnection}, prefixSend...)
// ValidateNodesHaveRoutes([]*corev1.Pod{pod}, *externalP2PContainer, rc.ToAdvertiseV4, rc.ToAdvertiseV6)
// because the LLA ip cannot be found as part of the node
func validatePrefixViaFor(peer executor.Executor, dev, nextHop string, prefixes ...[]string) {
	ginkgo.By(fmt.Sprintf("validating prefix %s to %s dev %s", prefixes, nextHop, dev))
	Eventually(func() error {
		nextHopAddr := netip.MustParseAddr(nextHop)
		want := make(map[netip.Prefix]map[netip.Addr]struct{})
		for _, prf := range prefixes {
			for _, p := range prf {
				want[netip.MustParsePrefix(p)] = map[netip.Addr]struct{}{nextHopAddr: {}}
			}
		}

		got, err := container.BGPRoutes(peer, dev)
		if err != nil {
			return err
		}
		if !cmp.Equal(want, got) {
			return fmt.Errorf("want %v\n got %v\n diff %v", want, got, cmp.Diff(want, got))
		}
		return nil
	}, 30*time.Second, 5*time.Second).ShouldNot(HaveOccurred(), fmt.Sprintf("peer should have the routes %s", prefixes))
}
