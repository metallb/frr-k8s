// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"go.universe.tf/e2etest/pkg/frr/container"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"

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

var _ = ginkgo.Describe("Advertisement", func() {
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

	ginkgo.Context("Advertising IPs", func() {
		type params struct {
			vrf             string
			ipFamily        ipfamily.Family
			myAsn           uint32
			prefixes        []string
			modifyNeighbors func([]frrk8sv1beta1.Neighbor)
			validate        func([]*frrcontainer.FRR, []v1.Node)
		}

		ginkgo.DescribeTable("Works with external frrs", func(p params) {
			frrs := config.ContainersForVRF(infra.FRRContainers, p.vrf)
			neighbors := config.NeighborsForContainers(frrs, p.ipFamily)
			p.modifyNeighbors(neighbors)

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
								Prefixes:  p.prefixes,
							},
						},
					},
				},
			}

			ginkgo.By("pairing with nodes")
			for _, c := range frrs {
				err := container.PairWithNodes(cs, c, p.ipFamily)
				framework.ExpectNoError(err)
			}
			err := updater.Update(config)
			framework.ExpectNoError(err)

			nodes, err := k8s.Nodes(cs)
			framework.ExpectNoError(err)

			for _, c := range frrs {
				ValidateFRRPeeredWithNodes(nodes, c, p.ipFamily)
			}

			ginkgo.By("validating")
			p.validate(frrs, nodes)
		},
			ginkgo.Entry("IPV4 - Advertise with mode allowall", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.2.0/24", "192.169.2.0/24"},
				modifyNeighbors: func(nn []frrk8sv1beta1.Neighbor) {
					for i := range nn {
						nn[i].ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(frrs []*frrcontainer.FRR, nodes []v1.Node) {
					for _, f := range frrs {
						ginkgo.By(fmt.Sprintf("checking prefixes on %s", f.Name))
						ValidatePrefixesForNeighbor(f, nodes, "192.168.2.0/24", "192.169.2.0/24")
					}
				},
			}),
			ginkgo.Entry("IPV4 - Advertise a subset of ips", params{
				ipFamily: ipfamily.IPv4,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"192.168.2.0/24", "192.169.2.0/24"},
				modifyNeighbors: func(nn []frrk8sv1beta1.Neighbor) {
					nn[0].ToAdvertise.Allowed.Prefixes = []string{"192.168.2.0/24"}
					for i := 1; i < len(nn); i++ {
						nn[i].ToAdvertise.Allowed.Prefixes = []string{"192.168.2.0/24", "192.169.2.0/24"}
					}
				},
				validate: func(frrs []*frrcontainer.FRR, nodes []v1.Node) {
					ginkgo.By(fmt.Sprintf("checking prefixes on %s", frrs[0].Name))
					ValidatePrefixesForNeighbor(frrs[0], nodes, "192.168.2.0/24")
					ValidateNeighborNoPrefixes(frrs[0], nodes, "192.169.2.0/24")
					for _, f := range frrs[1:] {
						ginkgo.By(fmt.Sprintf("checking prefixes on %s", f.Name))
						ValidatePrefixesForNeighbor(f, nodes, "192.168.2.0/24", "192.169.2.0/24")
					}
				},
			}),
			ginkgo.Entry("IPV6 - Advertise with mode allowall", params{
				ipFamily: ipfamily.IPv6,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyNeighbors: func(nn []frrk8sv1beta1.Neighbor) {
					for i := range nn {
						nn[i].ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(frrs []*frrcontainer.FRR, nodes []v1.Node) {
					for _, f := range frrs {
						ginkgo.By(fmt.Sprintf("checking prefixes on %s", f.Name))
						ValidatePrefixesForNeighbor(f, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
					}
				},
			}),
			ginkgo.Entry("IPV6 - Advertise a subset of ips", params{
				ipFamily: ipfamily.IPv6,
				vrf:      "",
				myAsn:    infra.FRRK8sASN,
				prefixes: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyNeighbors: func(nn []frrk8sv1beta1.Neighbor) {
					nn[0].ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64"}
					for i := 1; i < len(nn); i++ {
						nn[i].ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"}
					}
				},
				validate: func(frrs []*frrcontainer.FRR, nodes []v1.Node) {
					ginkgo.By(fmt.Sprintf("checking prefixes on %s", frrs[0].Name))
					ValidatePrefixesForNeighbor(frrs[0], nodes, "fc00:f853:ccd:e799::/64")
					ValidateNeighborNoPrefixes(frrs[0], nodes, "fc00:f853:ccd:e800::/64")
					for _, f := range frrs[1:] {
						ginkgo.By(fmt.Sprintf("checking prefixes on %s", f.Name))
						ValidatePrefixesForNeighbor(f, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
					}
				},
			}),
			ginkgo.Entry("IPV4 - VRF - Advertise with mode allowall", params{
				ipFamily: ipfamily.IPv4,
				vrf:      infra.VRFName,
				myAsn:    infra.FRRK8sASNVRF,
				prefixes: []string{"192.168.2.0/24", "192.169.2.0/24"},
				modifyNeighbors: func(nn []frrk8sv1beta1.Neighbor) {
					for i := range nn {
						nn[i].ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					}
				},
				validate: func(frrs []*frrcontainer.FRR, nodes []v1.Node) {
					for _, f := range frrs {
						ginkgo.By(fmt.Sprintf("checking prefixes on %s", f.Name))
						ValidatePrefixesForNeighbor(f, nodes, "192.168.2.0/24", "192.169.2.0/24")
					}
				},
			}),
			ginkgo.Entry("IPV6 - VRF - Advertise a subset of ips", params{
				ipFamily: ipfamily.IPv6,
				vrf:      infra.VRFName,
				myAsn:    infra.FRRK8sASNVRF,
				prefixes: []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
				modifyNeighbors: func(nn []frrk8sv1beta1.Neighbor) {
					nn[0].ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64"}
					for i := 1; i < len(nn); i++ {
						nn[i].ToAdvertise.Allowed.Prefixes = []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"}
					}
				},
				validate: func(frrs []*frrcontainer.FRR, nodes []v1.Node) {
					ginkgo.By(fmt.Sprintf("checking prefixes on %s", frrs[0].Name))
					ValidatePrefixesForNeighbor(frrs[0], nodes, "fc00:f853:ccd:e799::/64")
					ValidateNeighborNoPrefixes(frrs[0], nodes, "fc00:f853:ccd:e800::/64")
					for _, f := range frrs[1:] {
						ginkgo.By(fmt.Sprintf("checking prefixes on %s", f.Name))
						ValidatePrefixesForNeighbor(f, nodes, "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64")
					}
				},
			}),
		)
	})
})
