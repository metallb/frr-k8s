// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"net"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/address"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	"github.com/openshift-kni/k8sreporter"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	"go.universe.tf/e2etest/pkg/ipfamily"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = ginkgo.Describe("Leaked routes with import vrfs should work", func() {
	var (
		cs clientset.Interface

		updater  *config.Updater
		reporter *k8sreporter.KubernetesReporter
	)

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
		reporter = dump.NewK8sReporter(k8s.FRRK8sNamespace)
		var err error
		updater, err = config.NewUpdater()
		Expect(err).NotTo(HaveOccurred())
		err = updater.Clean()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
	})

	initNeighbors := func(useVrf bool, ipFamily ipfamily.Family) ([]*frrcontainer.FRR, config.PeersConfig, []frrk8sv1beta1.Neighbor) {
		frrs := config.ContainersForVRF(infra.FRRContainers, "")
		if useVrf {
			frrs = config.ContainersForVRF(infra.FRRContainers, infra.VRFName)
		}
		peersConfig := config.PeersForContainers(frrs, ipFamily)
		neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)
		return frrs, peersConfig, neighbors
	}

	pairWithNodes := func(frrs []*frrcontainer.FRR, family ipfamily.Family, toAdvertise []string) error {
		for _, c := range frrs {
			err := frrcontainer.PairWithNodes(cs, c, family, func(frr *frrcontainer.FRR) {
				frr.NeighborConfig.ToAdvertiseV4 = address.FilterForFamily(toAdvertise, ipfamily.IPv4)
				frr.NeighborConfig.ToAdvertiseV6 = address.FilterForFamily(toAdvertise, ipfamily.IPv6)
			})
			if err != nil {
				return err
			}
		}
		return nil
	}

	updateAndCheckPeered := func(config frrk8sv1beta1.FRRConfiguration, peersDefault, peersVRF config.PeersConfig, frrsDefault, frrsVRF []*frrcontainer.FRR, family ipfamily.Family) {
		err := updater.Update(peersDefault.Secrets, config)
		Expect(err).NotTo(HaveOccurred())

		err = updater.Update(peersVRF.Secrets, config)
		Expect(err).NotTo(HaveOccurred())

		nodes, err := k8s.Nodes(cs)
		Expect(err).NotTo(HaveOccurred())

		for _, c := range frrsDefault {
			ValidateFRRPeeredWithNodes(nodes, c, family)
		}
		for _, c := range frrsVRF {
			ValidateFRRPeeredWithNodes(nodes, c, family)
		}
	}

	baseConfig := frrk8sv1beta1.FRRConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: k8s.FRRK8sNamespace,
		},
		Spec: frrk8sv1beta1.FRRConfigurationSpec{
			BGP: frrk8sv1beta1.BGPConfig{
				Routers: []frrk8sv1beta1.Router{
					{
						ASN: infra.FRRK8sASN,
					},
					{
						ASN: infra.FRRK8sASNVRF,
						VRF: infra.VRFName,
					},
				},
			},
		},
	}

	prefixesToSelectors := func(prefixes []string) []frrk8sv1beta1.PrefixSelector {
		res := []frrk8sv1beta1.PrefixSelector{}
		for _, p := range prefixes {
			selector := frrk8sv1beta1.PrefixSelector{
				Prefix: p,
				LE:     32,
				GE:     0,
			}
			ip, _, err := net.ParseCIDR(p)
			if err != nil {
				panic(err)
			}
			if ip.To4() == nil {
				selector.LE = 64
			}
			res = append(res, selector)
		}
		return res
	}

	ginkgo.Context("while receiving IPs", func() {

		ginkgo.DescribeTable("when the default VRF imports a vrf", func(
			ipFamily ipfamily.Family,
			toAdvertise,
			toAdvertiseVRF []string,
			allowMode frrk8sv1beta1.AllowMode) {

			frrsDefault, peersDefault, neighborsDefault := initNeighbors(false, ipFamily)
			frrsVRF, peersVRF, neighborsVRF := initNeighbors(true, ipFamily)

			ginkgo.By("pairing with nodes")
			err := pairWithNodes(frrsDefault, ipFamily, toAdvertise)
			Expect(err).NotTo(HaveOccurred())
			err = pairWithNodes(frrsVRF, ipFamily, toAdvertiseVRF)
			Expect(err).NotTo(HaveOccurred())

			config := *baseConfig.DeepCopy()
			config.Spec.BGP.Routers[0].Neighbors = neighborsDefault
			config.Spec.BGP.Routers[0].Imports = []frrk8sv1beta1.Import{{VRF: infra.VRFName}}
			config.Spec.BGP.Routers[1].Neighbors = neighborsVRF

			for i := range config.Spec.BGP.Routers[0].Neighbors {
				if allowMode == frrk8sv1beta1.AllowRestricted {
					config.Spec.BGP.Routers[0].Neighbors[i].ToReceive.Allowed.Prefixes = prefixesToSelectors(append(toAdvertise, toAdvertiseVRF...))
				}
				config.Spec.BGP.Routers[0].Neighbors[i].ToReceive.Allowed.Mode = allowMode
			}
			for i := range config.Spec.BGP.Routers[1].Neighbors {
				if allowMode == frrk8sv1beta1.AllowRestricted {
					config.Spec.BGP.Routers[1].Neighbors[i].ToReceive.Allowed.Prefixes = prefixesToSelectors(toAdvertiseVRF)
				}
				config.Spec.BGP.Routers[1].Neighbors[i].ToReceive.Allowed.Mode = allowMode

			}

			updateAndCheckPeered(config, peersDefault, peersVRF, frrsDefault, frrsVRF, ipFamily)

			ginkgo.By("validating")
			pods, err := k8s.FRRK8sPods(cs)
			Expect(err).NotTo(HaveOccurred())

			for _, frr := range frrsDefault {
				ValidateNodesHaveRoutes(pods, *frr, toAdvertise...)
			}
			for _, frr := range frrsVRF {
				// The routes advertised from the peers through red must appear in the default VRF too
				ValidateNodesHaveRoutesVRF(pods, *frr, "", toAdvertiseVRF...)
				ValidateNodesHaveRoutes(pods, *frr, toAdvertiseVRF...)
			}
		},
			ginkgo.Entry("with specific IPV4 imports",
				ipfamily.IPv4,
				[]string{"192.168.2.0/24"},
				[]string{"192.169.2.0/24"},
				frrk8sv1beta1.AllowRestricted,
			),
			ginkgo.Entry("with allow all IPV4 imports",
				ipfamily.IPv4,
				[]string{"192.168.2.0/24"},
				[]string{"192.169.2.0/24"},
				frrk8sv1beta1.AllowAll,
			),
			ginkgo.Entry("with specific IPV6 imports",
				ipfamily.IPv6,
				[]string{"fc00:f853:ccd:e799::/64"},
				[]string{"fc00:f853:ccd:e800::/64"},
				frrk8sv1beta1.AllowRestricted,
			),
			ginkgo.Entry("with allow all IPV6 imports",
				ipfamily.IPv6,
				[]string{"fc00:f853:ccd:e799::/64"},
				[]string{"fc00:f853:ccd:e800::/64"},
				frrk8sv1beta1.AllowAll,
			),
		)

		ginkgo.DescribeTable("when the red VRF imports the default VRF", func(
			ipFamily ipfamily.Family,
			toAdvertise,
			toAdvertiseVRF []string,
			allowMode frrk8sv1beta1.AllowMode) {

			frrsDefault, peersDefault, neighborsDefault := initNeighbors(false, ipFamily)
			frrsVRF, peersVRF, neighborsVRF := initNeighbors(true, ipFamily)

			ginkgo.By("pairing with nodes")
			err := pairWithNodes(frrsDefault, ipFamily, toAdvertise)
			Expect(err).NotTo(HaveOccurred())
			err = pairWithNodes(frrsVRF, ipFamily, toAdvertiseVRF)
			Expect(err).NotTo(HaveOccurred())

			config := *baseConfig.DeepCopy()
			config.Spec.BGP.Routers[0].Neighbors = neighborsDefault
			config.Spec.BGP.Routers[1].Neighbors = neighborsVRF
			config.Spec.BGP.Routers[1].Imports = []frrk8sv1beta1.Import{{VRF: "default"}}

			for i := range config.Spec.BGP.Routers[0].Neighbors {
				if allowMode == frrk8sv1beta1.AllowRestricted {
					config.Spec.BGP.Routers[0].Neighbors[i].ToReceive.Allowed.Prefixes = prefixesToSelectors(toAdvertise)
				}
				config.Spec.BGP.Routers[0].Neighbors[i].ToReceive.Allowed.Mode = allowMode
			}
			for i := range config.Spec.BGP.Routers[1].Neighbors {
				if allowMode == frrk8sv1beta1.AllowRestricted {
					config.Spec.BGP.Routers[1].Neighbors[i].ToReceive.Allowed.Prefixes = prefixesToSelectors(append(toAdvertise, toAdvertiseVRF...))
				}
				config.Spec.BGP.Routers[1].Neighbors[i].ToReceive.Allowed.Mode = allowMode
			}

			updateAndCheckPeered(config, peersDefault, peersVRF, frrsDefault, frrsVRF, ipFamily)

			ginkgo.By("validating")
			pods, err := k8s.FRRK8sPods(cs)
			Expect(err).NotTo(HaveOccurred())

			// The routes advertised from the peers through the default must appear in the red VRF too
			for _, frr := range frrsDefault {
				ValidateNodesHaveRoutes(pods, *frr, toAdvertise...)
				ValidateNodesHaveRoutesVRF(pods, *frr, infra.VRFName, toAdvertise...)
			}
			for _, frr := range frrsVRF {
				ValidateNodesHaveRoutes(pods, *frr, toAdvertiseVRF...)
			}
		},
			ginkgo.Entry("with specific IPV4 imports",
				ipfamily.IPv4,
				[]string{"192.168.2.0/24"},
				[]string{"192.169.2.0/24"},
				frrk8sv1beta1.AllowRestricted,
			),
			ginkgo.Entry("with allow all IPV4 imports",
				ipfamily.IPv4,
				[]string{"192.168.2.0/24"},
				[]string{"192.169.2.0/24"},
				frrk8sv1beta1.AllowAll,
			),
			ginkgo.Entry("with specific IPV6 imports",
				ipfamily.IPv6,
				[]string{"fc00:f853:ccd:e799::/64"},
				[]string{"fc00:f853:ccd:e800::/64"},
				frrk8sv1beta1.AllowRestricted,
			),
			ginkgo.Entry("with allow all IPV6 imports",
				ipfamily.IPv6,
				[]string{"fc00:f853:ccd:e799::/64"},
				[]string{"fc00:f853:ccd:e800::/64"},
				frrk8sv1beta1.AllowAll,
			),
		)

	})

	ginkgo.Context("while advertising IPs", func() {

		ginkgo.DescribeTable("when the default VRF imports a vrf", func(
			ipFamily ipfamily.Family,
			toAdvertise,
			toAdvertiseVRF []string,
			allowMode frrk8sv1beta1.AllowMode) {

			frrsDefault, peersDefault, neighborsDefault := initNeighbors(false, ipFamily)
			frrsVRF, peersVRF, neighborsVRF := initNeighbors(true, ipFamily)

			ginkgo.By("pairing with nodes")
			err := pairWithNodes(frrsDefault, ipFamily, []string{})
			Expect(err).NotTo(HaveOccurred())
			err = pairWithNodes(frrsVRF, ipFamily, []string{})
			Expect(err).NotTo(HaveOccurred())

			config := *baseConfig.DeepCopy()
			config.Spec.BGP.Routers[0].Neighbors = neighborsDefault
			config.Spec.BGP.Routers[0].Prefixes = toAdvertise
			config.Spec.BGP.Routers[0].Imports = []frrk8sv1beta1.Import{{VRF: infra.VRFName}}
			config.Spec.BGP.Routers[1].Neighbors = neighborsVRF
			config.Spec.BGP.Routers[1].Prefixes = toAdvertiseVRF

			for i := range config.Spec.BGP.Routers[0].Neighbors {
				if allowMode == frrk8sv1beta1.AllowRestricted {
					config.Spec.BGP.Routers[0].Neighbors[i].ToAdvertise.Allowed.Prefixes = append(toAdvertise, toAdvertiseVRF...)
				}
				config.Spec.BGP.Routers[0].Neighbors[i].ToAdvertise.Allowed.Mode = allowMode
			}
			for i := range config.Spec.BGP.Routers[1].Neighbors {
				if allowMode == frrk8sv1beta1.AllowRestricted {
					config.Spec.BGP.Routers[1].Neighbors[i].ToAdvertise.Allowed.Prefixes = toAdvertiseVRF
				}
				config.Spec.BGP.Routers[1].Neighbors[i].ToAdvertise.Allowed.Mode = allowMode
			}

			updateAndCheckPeered(config, peersDefault, peersVRF, frrsDefault, frrsVRF, ipFamily)

			ginkgo.By("validating")

			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())

			for _, frr := range frrsDefault {
				ValidatePrefixesForNeighbor(*frr, nodes, toAdvertise...)
				ValidatePrefixesForNeighborVRF(*frr, nodes, "", toAdvertiseVRF...)
			}
			for _, frr := range frrsVRF {
				ValidatePrefixesForNeighbor(*frr, nodes, toAdvertiseVRF...)
			}
		},
			ginkgo.Entry("with specific IPV4 exports",
				ipfamily.IPv4,
				[]string{"192.168.2.0/24"},
				[]string{"192.169.2.0/24"},
				frrk8sv1beta1.AllowRestricted,
			),
			ginkgo.Entry("with allow all IPV4 exports",
				ipfamily.IPv4,
				[]string{"192.168.2.0/24"},
				[]string{"192.169.2.0/24"},
				frrk8sv1beta1.AllowAll,
			),
			ginkgo.Entry("with specific IPV6 exports",
				ipfamily.IPv6,
				[]string{"fc00:f853:ccd:e799::/64"},
				[]string{"fc00:f853:ccd:e800::/64"},
				frrk8sv1beta1.AllowRestricted,
			),
			ginkgo.Entry("with allow all IPV6 exports",
				ipfamily.IPv6,
				[]string{"fc00:f853:ccd:e799::/64"},
				[]string{"fc00:f853:ccd:e800::/64"},
				frrk8sv1beta1.AllowAll,
			),
		)

		ginkgo.DescribeTable("when the red VRF imports from the default vrf", func(
			ipFamily ipfamily.Family,
			toAdvertise,
			toAdvertiseVRF []string,
			allowMode frrk8sv1beta1.AllowMode) {

			frrsDefault, peersDefault, neighborsDefault := initNeighbors(false, ipFamily)
			frrsVRF, peersVRF, neighborsVRF := initNeighbors(true, ipFamily)

			ginkgo.By("pairing with nodes")
			err := pairWithNodes(frrsDefault, ipFamily, []string{})
			Expect(err).NotTo(HaveOccurred())
			err = pairWithNodes(frrsVRF, ipFamily, []string{})
			Expect(err).NotTo(HaveOccurred())

			config := *baseConfig.DeepCopy()
			config.Spec.BGP.Routers[0].Neighbors = neighborsDefault
			config.Spec.BGP.Routers[0].Prefixes = toAdvertise
			config.Spec.BGP.Routers[1].Neighbors = neighborsVRF
			config.Spec.BGP.Routers[1].Prefixes = toAdvertiseVRF
			config.Spec.BGP.Routers[1].Imports = []frrk8sv1beta1.Import{{VRF: "default"}}

			for i := range config.Spec.BGP.Routers[0].Neighbors {
				if allowMode == frrk8sv1beta1.AllowRestricted {
					config.Spec.BGP.Routers[0].Neighbors[i].ToAdvertise.Allowed.Prefixes = toAdvertise
				}
				config.Spec.BGP.Routers[0].Neighbors[i].ToAdvertise.Allowed.Mode = allowMode
			}
			for i := range config.Spec.BGP.Routers[1].Neighbors {
				if allowMode == frrk8sv1beta1.AllowRestricted {
					config.Spec.BGP.Routers[1].Neighbors[i].ToAdvertise.Allowed.Prefixes = append(toAdvertise, toAdvertiseVRF...)
				}
				config.Spec.BGP.Routers[1].Neighbors[i].ToAdvertise.Allowed.Mode = allowMode
			}

			updateAndCheckPeered(config, peersDefault, peersVRF, frrsDefault, frrsVRF, ipFamily)

			ginkgo.By("validating")

			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())

			for _, frr := range frrsDefault {
				ValidatePrefixesForNeighbor(*frr, nodes, toAdvertise...)
			}
			for _, frr := range frrsVRF {
				ValidatePrefixesForNeighborVRF(*frr, nodes, infra.VRFName, toAdvertiseVRF...)
				ValidatePrefixesForNeighbor(*frr, nodes, toAdvertiseVRF...)
			}
		},
			ginkgo.Entry("with specific IPV4 exports",
				ipfamily.IPv4,
				[]string{"192.168.2.0/24"},
				[]string{"192.169.2.0/24"},
				frrk8sv1beta1.AllowRestricted,
			),
			ginkgo.Entry("with allow all IPV4 exports",
				ipfamily.IPv4,
				[]string{"192.168.2.0/24"},
				[]string{"192.169.2.0/24"},
				frrk8sv1beta1.AllowAll,
			),
			ginkgo.Entry("with specific IPV6 exports",
				ipfamily.IPv6,
				[]string{"fc00:f853:ccd:e799::/64"},
				[]string{"fc00:f853:ccd:e800::/64"},
				frrk8sv1beta1.AllowRestricted,
			),
			ginkgo.Entry("with allow all IPV6 exports",
				ipfamily.IPv6,
				[]string{"fc00:f853:ccd:e799::/64"},
				[]string{"fc00:f853:ccd:e800::/64"},
				frrk8sv1beta1.AllowAll,
			),
		)
	})
})
