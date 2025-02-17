// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/openshift-kni/k8sreporter"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	. "github.com/onsi/gomega"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	nodeLabel = "frrk8s.metallb.io/node"
	peerLabel = "frrk8s.metallb.io/peer"
	vrfLabel  = "frrk8s.metallb.io/vrf"
)

var _ = ginkgo.Describe("BGPSessionState", func() {
	var (
		cs       clientset.Interface
		cl       client.Client
		updater  *config.Updater
		reporter *k8sreporter.KubernetesReporter
	)
	defer ginkgo.GinkgoRecover()

	ginkgo.BeforeEach(func() {
		var err error
		updater, err = config.NewUpdater()
		Expect(err).NotTo(HaveOccurred())
		reporter = dump.NewK8sReporter(k8s.FRRK8sNamespace)

		myScheme := runtime.NewScheme()
		err = frrk8sv1beta1.AddToScheme(myScheme)
		Expect(err).NotTo(HaveOccurred())
		err = v1.AddToScheme(myScheme)
		Expect(err).NotTo(HaveOccurred())
		clientconfig := k8sclient.RestConfig()
		cl, err = client.New(clientconfig, client.Options{
			Scheme: myScheme,
		})
		Expect(err).NotTo(HaveOccurred())

		ginkgo.By("Clearing any previous configuration")
		for _, c := range infra.FRRContainers {
			err := c.UpdateBGPConfigFile(frrconfig.Empty)
			Expect(err).NotTo(HaveOccurred())
		}
		err = updater.Clean()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
	})

	ginkgo.AfterEach(func() {
		if ginkgo.CurrentSpecReport().Failed() {
			testName := ginkgo.CurrentSpecReport().LeafNodeText
			dump.K8sInfo(testName, reporter)
			dump.BGPInfo(testName, infra.FRRContainers, cs)
		}
		err := updater.Clean()
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.Context("BGP and BFD", func() {
		type params struct {
			vrf         string
			ipFamily    ipfamily.Family
			myAsn       uint32
			prefixes    []string // prefixes that the nodes advertise to the containers
			modifyPeers func([]config.Peer, []config.Peer)
		}
		type bgpPhase struct {
			sets.Set[string]
		}
		type bfdState string
		const (
			noBFD   bfdState = "N/A"
			bfdDown bfdState = "Down"
			bfdUp   bfdState = "Up"
		)
		bgpTryingToConnect := bgpPhase{sets.New("Idle", "Connect", "Active")}
		bgpEstablished := bgpPhase{sets.New("Established")}
		ginkgo.DescribeTable("Each node manages its statuses", func(p params) {
			validateStatusForNeigh := func(neighborIP string, nodes []corev1.Node, vrf string, expectedBGP bgpPhase, expectedBFD bfdState) error {
				for _, n := range nodes {
					s, err := bgpSessionStateFor(cl, n.Name, neighborIP, vrf)
					if err != nil {
						return err
					}
					key := fmt.Sprintf("node %s peer %s vrf %s", n.Name, neighborIP, vrf)
					if !expectedBGP.Has(s.Status.BGPStatus) {
						return fmt.Errorf("expected bgp status for %s to be one of %v, got %s", key, expectedBGP, s.Status.BGPStatus)

					}
					if string(expectedBFD) != s.Status.BFDStatus {
						return fmt.Errorf("expected bfd status for %s to be %v, got %s", key, expectedBFD, s.Status.BFDStatus)

					}
				}
				return nil
			}
			validateNoStatusForNeigh := func(neighborIP string, nodes []corev1.Node, vrf string) error {
				for _, n := range nodes {
					s, err := bgpSessionStateFor(cl, n.Name, neighborIP, vrf)
					if !k8serrors.IsNotFound(err) {
						return fmt.Errorf("expected status to not be there, got %v with err %w", s, err)
					}
				}
				return nil
			}
			validateStatusForAll := func(neighbors []frrk8sv1beta1.Neighbor, nodes []corev1.Node, expectedBGP bgpPhase, expectedBFD bfdState) error {
				for _, n := range neighbors {
					err := validateStatusForNeigh(n.Address, nodes, p.vrf, expectedBGP, expectedBFD)
					if err != nil {
						return err
					}
				}
				return nil
			}
			validateNoStatusForAll := func(neighbors []frrk8sv1beta1.Neighbor, nodes []corev1.Node) error {
				for _, n := range neighbors {
					err := validateNoStatusForNeigh(n.Address, nodes, p.vrf)
					if err != nil {
						return err
					}
				}
				return nil
			}

			frrs := config.ContainersForVRF(infra.FRRContainers, p.vrf)
			peersConfig := config.PeersForContainers(frrs, p.ipFamily)
			p.modifyPeers(peersConfig.PeersV4, peersConfig.PeersV6)
			neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)

			ginkgo.By("Creating the FRR configuration")
			conf := frrk8sv1beta1.FRRConfiguration{
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

			err := updater.Update(peersConfig.Secrets, conf)
			Expect(err).NotTo(HaveOccurred())

			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Verifying the status resources are created with bgp Idle/Connect/Active")
			Eventually(func() error {
				return validateStatusForAll(neighbors, nodes, bgpTryingToConnect, noBFD)
			}, time.Minute, 2*time.Second).ShouldNot(HaveOccurred())
			Consistently(func() error {
				return validateStatusForAll(neighbors, nodes, bgpTryingToConnect, noBFD)
			}, 10*time.Second, 1*time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Pairing the containers with the nodes")
			for _, c := range frrs {
				err := frrcontainer.PairWithNodes(cs, c, p.ipFamily)
				Expect(err).NotTo(HaveOccurred())
			}

			for _, c := range frrs {
				ValidateFRRPeeredWithNodes(nodes, c, p.ipFamily)
			}

			ginkgo.By("Verifying the statuses are updated with bgp Established")
			Eventually(func() error {
				return validateStatusForAll(neighbors, nodes, bgpEstablished, noBFD)
			}, time.Minute, 2*time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Updating the neighbors to have BFD")
			simpleProfile := frrk8sv1beta1.BFDProfile{
				Name: "simple",
			}
			for i := range neighbors {
				neighbors[i].BFDProfile = "simple"
			}
			conf = frrk8sv1beta1.FRRConfiguration{
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
						BFDProfiles: []frrk8sv1beta1.BFDProfile{
							simpleProfile,
						},
					},
				},
			}

			err = updater.Update(peersConfig.Secrets, conf)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Verifying the statuses are updated with BFD down")
			Eventually(func() error {
				return validateStatusForAll(neighbors, nodes, bgpEstablished, bfdDown)
			}, time.Minute, 2*time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Pairing the containers with the nodes with BFD")
			for _, c := range infra.FRRContainers {
				err := frrcontainer.PairWithNodes(cs, c, p.ipFamily, func(container *frrcontainer.FRR) {
					container.NeighborConfig.BFDEnabled = true
				})
				Expect(err).NotTo(HaveOccurred())
			}

			ginkgo.By("Verifying the statuses are updated with BFD up")
			Eventually(func() error {
				return validateStatusForAll(neighbors, nodes, bgpEstablished, bfdUp)
			}, time.Minute, 2*time.Second).ShouldNot(HaveOccurred())
			Consistently(func() error {
				return validateStatusForAll(neighbors, nodes, bgpEstablished, bfdUp)
			}, 10*time.Second, 1*time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Updating the config to target all nodes but the first")
			nodesNames := []string{}
			for _, n := range nodes {
				nodesNames = append(nodesNames, n.Name)
			}
			conf.Spec.NodeSelector = metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "kubernetes.io/hostname",
						Values:   nodesNames[1:],
						Operator: metav1.LabelSelectorOpIn,
					},
				},
			}
			err = updater.Update(peersConfig.Secrets, conf)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				return validateNoStatusForAll(neighbors, []corev1.Node{nodes[0]})
			}, time.Minute, 2*time.Second).ShouldNot(HaveOccurred())
			Eventually(func() error {
				return validateStatusForAll(neighbors, nodes[1:], bgpEstablished, bfdUp)
			}, time.Minute, 2*time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Resetting the FRR configuration on the first external container")
			err = frrs[0].UpdateBGPConfigFile(frrconfig.Empty)
			Expect(err).NotTo(HaveOccurred())
			frr0Addresses := sets.New(frrs[0].AddressesForFamily(p.ipFamily)...)
			frr0Neighbors := []frrk8sv1beta1.Neighbor{}
			otherNeighbors := []frrk8sv1beta1.Neighbor{}
			for _, n := range neighbors {
				if frr0Addresses.Has(n.Address) {
					frr0Neighbors = append(frr0Neighbors, n)
					continue
				}
				otherNeighbors = append(otherNeighbors, n)
			}
			Eventually(func() error {
				return validateStatusForAll(frr0Neighbors, nodes[1:], bgpTryingToConnect, bfdDown)
			}, time.Minute, 2*time.Second).ShouldNot(HaveOccurred())
			Eventually(func() error {
				return validateStatusForAll(otherNeighbors, nodes[1:], bgpEstablished, bfdUp)
			}, time.Minute, 2*time.Second).ShouldNot(HaveOccurred())
		},
			ginkgo.Entry("IPV4", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, _ []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV4[i].Neigh.ConnectTime = &metav1.Duration{Duration: 5 * time.Second}
					}
				},
			}),
			ginkgo.Entry("IPV6", params{
				ipFamily: ipfamily.IPv6,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"fc00:f853:ccd:e799::/64"},
				modifyPeers: func(_ []config.Peer, ppV6 []config.Peer) {
					for i := range ppV6 {
						ppV6[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV6[i].Neigh.ConnectTime = &metav1.Duration{Duration: 5 * time.Second}
					}
				},
			}),
			ginkgo.Entry("DUALSTACK", params{
				ipFamily: ipfamily.DualStack,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.2.0/24", "fc00:f853:ccd:e799::/64"},
				modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV4[i].Neigh.ConnectTime = &metav1.Duration{Duration: 5 * time.Second}
					}
					for i := range ppV6 {
						ppV6[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV6[i].Neigh.ConnectTime = &metav1.Duration{Duration: 5 * time.Second}
					}
				},
			}),
			ginkgo.Entry("IPV4 - VRF", params{
				ipFamily: ipfamily.IPv4,
				vrf:      infra.VRFName,
				myAsn:    infra.FRRK8sASNVRF,
				prefixes: []string{"192.168.2.0/24"},
				modifyPeers: func(ppV4 []config.Peer, _ []config.Peer) {
					for i := range ppV4 {
						ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
						ppV4[i].Neigh.ConnectTime = &metav1.Duration{Duration: 5 * time.Second}
					}
				},
			}))
	})
})

func bgpSessionStateFor(cl client.Client, node, neighborIP, vrf string) (*frrk8sv1beta1.BGPSessionState, error) {
	key := fmt.Sprintf("node %s peer %s vrf %s", node, neighborIP, vrf)
	l := frrk8sv1beta1.BGPSessionStateList{}
	err := cl.List(context.TODO(), &l, client.MatchingLabels{
		nodeLabel: node,
		peerLabel: statusFormatFor(neighborIP),
		vrfLabel:  vrf,
	})
	if err != nil {
		return nil, fmt.Errorf("could not get status for %s, err %w", key, err)
	}

	if len(l.Items) == 0 {
		return nil, k8serrors.NewNotFound(schema.ParseGroupResource("bgpsessionstate.frrk8s.metallb.io"), key)
	}
	if len(l.Items) > 1 {
		return nil, fmt.Errorf("got more than 1 BGPSessionState for %s: %v", key, l.Items)
	}

	s := &l.Items[0]
	if s.Status.Node != node {
		return nil, fmt.Errorf("got different node in the .Status for %s, %v", key, s.Status)
	}
	ip1 := net.ParseIP(ipFor(s.Status.Peer))
	ip2 := net.ParseIP(neighborIP)
	if !ip1.Equal(ip2) {
		return nil, fmt.Errorf("got different peer in the .Status for %s, %v", key, s.Status)
	}
	if s.Status.VRF != vrf {
		return nil, fmt.Errorf("got different vrf in the .Status for %s, %v", key, s.Status)
	}

	return s, nil
}

func statusFormatFor(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	if addr.Is4() {
		return ip
	}
	return strings.ReplaceAll(addr.StringExpanded(), ":", "-")
}

func ipFor(statusIP string) string {
	return strings.ReplaceAll(statusIP, "-", ":")
}
