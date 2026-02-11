// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	"go.universe.tf/e2etest/pkg/frr"
	metallbfrr "go.universe.tf/e2etest/pkg/frr"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	"go.universe.tf/e2etest/pkg/frr/container"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	metallbipfamily "go.universe.tf/e2etest/pkg/ipfamily"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var _ = ginkgo.Describe("BFD", func() {
	var cs clientset.Interface
	var nodes []corev1.Node

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
		nodes, err = k8s.Nodes(cs)
		Expect(err).NotTo(HaveOccurred())
	})

	simpleProfile := frrk8sv1beta1.BFDProfile{
		Name: "simple",
	}
	fullProfile := frrk8sv1beta1.BFDProfile{
		Name:             "full1",
		ReceiveInterval:  ptr.To[uint32](60),
		TransmitInterval: ptr.To[uint32](61),
		EchoInterval:     ptr.To[uint32](62),
		MinimumTTL:       ptr.To[uint32](254),
	}
	withEchoMode := frrk8sv1beta1.BFDProfile{
		Name:             "echo",
		ReceiveInterval:  ptr.To[uint32](80),
		TransmitInterval: ptr.To[uint32](81),
		EchoInterval:     ptr.To[uint32](82),
		EchoMode:         ptr.To(true),
		MinimumTTL:       ptr.To[uint32](254),
	}

	ginkgo.DescribeTable("should work with the given bfd profile", func(bfdProfileDefault frrk8sv1beta1.BFDProfile, bfdProfileRed frrk8sv1beta1.BFDProfile, pairingFamily ipfamily.Family) {

		ginkgo.By("pairing with nodes")
		for _, c := range infra.FRRContainers {
			err := container.PairWithNodes(cs, c, pairingFamily, func(container *frrcontainer.FRR) {
				container.NeighborConfig.BFDEnabled = true
			})
			Expect(err).NotTo(HaveOccurred())
		}

		config, secrets := frrConfigFor(infra.FRRK8sASN, pairingFamily, withProfile(bfdProfileDefault))
		err = updater.Update(secrets, config)
		Expect(err).NotTo(HaveOccurred())

		vrfConfig, redSecrets := frrConfigFor(infra.FRRK8sASNVRF, pairingFamily, withProfile(bfdProfileRed), withVRF(infra.VRFName))
		err = updater.Update(redSecrets, vrfConfig)
		Expect(err).NotTo(HaveOccurred())

		for _, c := range infra.FRRContainers {
			ValidateFRRPeeredWithNodes(nodes, c, pairingFamily)
		}
		checkBFDParameters(nodes, pairingFamily, infra.DefaultVRFName, bfdProfileDefault)
		checkBFDParameters(nodes, pairingFamily, infra.VRFName, bfdProfileRed)

		previousNeighbors := map[string]frr.NeighborsMap{}
		for _, c := range infra.FRRContainers {
			neighbors, err := frr.NeighborsInfo(c)
			Expect(err).NotTo(HaveOccurred())
			previousNeighbors[c.Name] = neighbors
		}
		ginkgo.By("adding prefixes to the router ")
		config.Spec.BGP.Routers[0].Prefixes = []string{"192.168.2.0/24", "192.169.2.0/24"}
		err = updater.Update(secrets, config)
		Expect(err).NotTo(HaveOccurred())

		Consistently(func() error {
			for _, c := range infra.FRRContainers {
				neighbors, err := frr.NeighborsInfo(c)
				Expect(err).NotTo(HaveOccurred())
				Expect(neighbors).To(HaveLen(len(previousNeighbors[c.Name])))

				for _, n := range neighbors {
					previousDropped := previousNeighbors[c.Name][n.ID].ConnectionsDropped
					if n.ConnectionsDropped > previousDropped {
						return fmt.Errorf("increased connections dropped from %s to %s, previous: %d current %d", c.Name, n.ID, previousDropped, n.ConnectionsDropped)
					}
				}
			}
			return nil
		}, 10*time.Second, 1*time.Second).ShouldNot(HaveOccurred())

	},
		ginkgo.Entry("IPV4 - default", simpleProfile, simpleProfile, ipfamily.IPv4),
		ginkgo.Entry("IPV4 - full params", fullProfile, fullProfile, ipfamily.IPv4),
		ginkgo.Entry("IPV4 - echo mode enabled", withEchoMode, fullProfile, ipfamily.IPv4), // echo mode doesn't work with VRF
		ginkgo.Entry("IPV6 - default", simpleProfile, simpleProfile, ipfamily.IPv6),
		ginkgo.Entry("IPV6 - full params", fullProfile, fullProfile, ipfamily.IPv6),
		ginkgo.Entry("IPV6 - echo mode enabled", withEchoMode, fullProfile, ipfamily.IPv6), // echo mode doesn't work with VRF
	)

	// We test only the happy path here. We test webhooks rejection inside the webhooks E2E tests.
	ginkgo.It("should handle merging NeighborConfig with and without BFD profile name", func() {
		pairingFamily := ipfamily.IPv4

		for _, c := range infra.FRRContainers {
			err := container.PairWithNodes(cs, c, pairingFamily, func(container *frrcontainer.FRR) {
				container.NeighborConfig.BFDEnabled = true
			})
			Expect(err).NotTo(HaveOccurred())
		}

		// Run all tests inside the default VRF.
		vrfName := infra.DefaultVRFName
		asn := uint32(infra.FRRK8sASN)

		// Create first resource with no BFD profile.
		firstConfig, firstSecrets := frrConfigFor(asn, pairingFamily, withSuffix("a"))
		err = updater.Update(firstSecrets, firstConfig)
		Expect(err).NotTo(HaveOccurred())

		// Create second resource with simple BFD profile.
		secondConfig, secondSecrets := frrConfigFor(asn, pairingFamily, withSuffix("b"), withProfile(simpleProfile))
		err = updater.Update(secondSecrets, secondConfig)
		Expect(err).NotTo(HaveOccurred())

		for _, c := range infra.FRRContainers {
			if c.RouterConfig.VRF != vrfName {
				continue
			}
			ValidateFRRPeeredWithNodes(nodes, c, pairingFamily)
		}
		// We expect that the simple BFD profile from "b" was merged with the non-existing profile from "a".
		checkBFDParameters(nodes, pairingFamily, vrfName, simpleProfile)
	})
})

// checkBFDParameters validates that our nodes are correctly peering with the BFD peers in the provided VRF
// It makes sure that the BFD Peers status is up and that the BFDProfile's timer configuration shows up in the
// Peers' Remote timers status.
func checkBFDParameters(nodes []corev1.Node, pairingFamily metallbipfamily.Family, vrfName string,
	profile frrk8sv1beta1.BFDProfile) {
	Eventually(func() error {
		for _, c := range infra.FRRContainers {
			// Only look at remote containers inside the VRF that we are interested in.
			if c.RouterConfig.VRF != vrfName {
				continue
			}
			// In each external FRRContainer, we run `show bfd peers`. We then compare that to all nodes in the cluster.
			bfdPeers, err := metallbfrr.BFDPeers(c.Executor)
			if err != nil {
				return err
			}
			// We make sure that the BFD peers' IP addresses configured in external FRRContainers exactly match the
			// nodes' IP addresses for the address family and VRF that we are looking at.
			err = metallbfrr.BFDPeersMatchNodes(nodes, bfdPeers, metallbipfamily.Family(pairingFamily), c.RouterConfig.VRF)
			if err != nil {
				return err
			}
			// Now that we know that the BFDPeer IPs match our nodes, go through each peer.
			// The BFDPeer reports the status (it should be `up`) and it reports the `Remote timers`. Those are the
			// timers that the FRR processes inside the FRRK8s containers use (the 'is' state), so they should match our
			// bfdProfile (the 'expected' state).
			for _, peerConfig := range bfdPeers {
				// The profile lacks default fields, so before comparing, fill in defaults if a given field isn't set.
				toCompare := bfdProfileWithDefaults(profile, peerConfig.Multihop)
				ginkgo.By(fmt.Sprintf("Checking bfd parameters on %s", peerConfig.Peer))
				err := checkBFDConfigPropagated(toCompare, peerConfig)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
}

// frrConfigFor creates an FRRConfiguration and associated secrets for testing BFD with the specified ASN,
// IP family, and optional configuration options (VRF, BFD profile, suffix).
func frrConfigFor(asn uint32, pairingFamily ipfamily.Family, opts ...configOption) (frrk8sv1beta1.FRRConfiguration, []corev1.Secret) {
	options := configOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	modify := options.neighborModifier()
	bfdProfiles := options.getBFDProfiles()
	frrs := config.ContainersForVRF(infra.FRRContainers, options.vrf)
	peersConfig := config.PeersForContainers(frrs, pairingFamily)
	neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)
	for i := range neighbors {
		modify(&neighbors[i])
	}

	config := frrk8sv1beta1.FRRConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      options.frrCfgName(),
			Namespace: k8s.FRRK8sNamespace,
		},
		Spec: frrk8sv1beta1.FRRConfigurationSpec{
			BGP: frrk8sv1beta1.BGPConfig{
				Routers: []frrk8sv1beta1.Router{
					{
						ASN:       asn,
						VRF:       options.vrf,
						Neighbors: neighbors,
					},
				},
				BFDProfiles: bfdProfiles,
			},
		},
	}
	return config, peersConfig.Secrets
}

type configOptions struct {
	vrf     string
	suffix  string
	profile *frrk8sv1beta1.BFDProfile
}

// frrCfgName generates the FRRConfiguration resource name based on VRF and suffix options.
// The name follows the pattern "test[-vrf][-suffix]" where vrf and suffix are optional.
func (co configOptions) frrCfgName() string {
	vrf := ""
	suffix := ""
	if co.vrf != "" {
		vrf = fmt.Sprintf("-%s", co.vrf)
	}
	if co.suffix != "" {
		suffix = fmt.Sprintf("-%s", co.suffix)
	}
	return fmt.Sprintf("test%s%s", vrf, suffix)
}

// getBFDProfiles returns a slice containing the BFD profile if one is configured,
// or an empty slice if no profile is set.
func (co configOptions) getBFDProfiles() []frrk8sv1beta1.BFDProfile {
	var bfdProfile []frrk8sv1beta1.BFDProfile
	if co.profile != nil {
		bfdProfile = []frrk8sv1beta1.BFDProfile{*co.profile}
	}
	return bfdProfile
}

// neighborModifier returns a function that modifies Neighbor configurations.
// If a BFD profile is set, the returned function assigns the profile name to the provided neighbor.
// Otherwise, it returns a no-op function that leaves neighbors unchanged.
func (co configOptions) neighborModifier() func(neigh *frrk8sv1beta1.Neighbor) {
	modify := func(neigh *frrk8sv1beta1.Neighbor) {}
	if co.profile != nil {
		modify = func(neigh *frrk8sv1beta1.Neighbor) {
			neigh.BFDProfile = co.profile.Name
		}
	}
	return modify
}

type configOption func(*configOptions)

func withVRF(vrf string) configOption {
	return func(opt *configOptions) {
		opt.vrf = vrf
	}
}

func withSuffix(suffix string) configOption {
	return func(opt *configOptions) {
		opt.suffix = suffix
	}
}

func withProfile(profile frrk8sv1beta1.BFDProfile) configOption {
	return func(opt *configOptions) {
		opt.profile = &profile
	}
}

func bfdProfileWithDefaults(profile frrk8sv1beta1.BFDProfile, multiHop bool) frrk8sv1beta1.BFDProfile {
	res := frrk8sv1beta1.BFDProfile{}
	res.Name = profile.Name
	res.ReceiveInterval = valueWithDefault(profile.ReceiveInterval, 300)
	res.TransmitInterval = valueWithDefault(profile.TransmitInterval, 300)
	res.DetectMultiplier = valueWithDefault(profile.DetectMultiplier, 3)
	res.EchoInterval = valueWithDefault(profile.EchoInterval, 50)
	res.MinimumTTL = valueWithDefault(profile.MinimumTTL, 254)
	res.EchoMode = profile.EchoMode
	res.PassiveMode = profile.PassiveMode

	if multiHop {
		res.EchoMode = ptr.To(false)
		res.EchoInterval = ptr.To[uint32](50)
	}

	return res
}

func valueWithDefault(v *uint32, def uint32) *uint32 {
	if v != nil {
		return v
	}
	return &def
}
