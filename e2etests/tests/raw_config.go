// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/onsi/ginkgo/v2"
	"github.com/pkg/errors"
	"go.universe.tf/e2etest/pkg/frr/container"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	admissionapi "k8s.io/pod-security-admission/api"
)

var _ = ginkgo.Describe("Injecting raw config", func() {
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

	ginkgo.Context("Works when", func() {
		ginkgo.It("adding manually the configuration to connect to an IPV4 peer", func() {
			frrs := config.ContainersForVRF(infra.FRRContainers, "")

			ginkgo.By(fmt.Sprintf("excluding %s from the configured neighbors", frrs[0].Name))
			peersConfig := config.PeersForContainers(frrs[1:], ipfamily.IPv4)
			neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)

			ginkgo.By("pairing with nodes")
			for _, c := range frrs {
				err := container.PairWithNodes(cs, c, ipfamily.IPv4)
				framework.ExpectNoError(err)
			}

			ginkgo.By(fmt.Sprintf("Manually generating the configuration for %s", frrs[0].Name))
			rawAddress, err := rawConfigForFRR(neighborAddressTemplate, infra.FRRK8sASN, frrs[0])
			framework.ExpectNoError(err)
			rawFamily, err := rawConfigForFRR(neighborFamilyTemplate, infra.FRRK8sASN, frrs[0])
			framework.ExpectNoError(err)

			config := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
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
					Raw: frrk8sv1beta1.RawConfig{
						Config: rawFamily,
					},
				},
			}

			err = updater.Update(peersConfig.Secrets, config)
			framework.ExpectNoError(err)

			configRawSecondBit := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					Raw: frrk8sv1beta1.RawConfig{
						Config:   rawAddress,
						Priority: 5,
					},
				},
			}
			err = updater.Update(peersConfig.Secrets, configRawSecondBit)
			framework.ExpectNoError(err)

			nodes, err := k8s.Nodes(cs)
			framework.ExpectNoError(err)

			for _, c := range frrs {
				ValidateFRRPeeredWithNodes(nodes, c, ipfamily.IPv4)
			}
		})
	})
})

const neighborAddressTemplate = `router bgp {{.RouterASN}}
 neighbor {{.NeighborIP}} remote-as {{.NeighborASN}}
 neighbor {{.NeighborIP}} timers 0 0
 {{- if .NeighborPort}}
 neighbor {{.NeighborIP}} port {{.NeighborPort}}
 {{- end }}
 {{- if .NeighborMultiHop}}
 neighbor {{.NeighborIP}} ebgp-multihop
 {{- end }}
 {{- if .NeighborPassword}}
 neighbor {{.NeighborIP}} password {{.NeighborPassword}}
 {{- end }}`

const neighborFamilyTemplate = `router bgp {{.RouterASN}}
 address-family ipv4 unicast
  neighbor {{.NeighborIP}} activate
  neighbor {{.NeighborIP}} route-map {{.NeighborIP}}-in in
  neighbor {{.NeighborIP}} route-map {{.NeighborIP}}-out out
 exit-address-family
 address-family ipv6 unicast
  neighbor {{.NeighborIP}} activate
  neighbor {{.NeighborIP}} route-map {{.NeighborIP}}-in in
  neighbor {{.NeighborIP}} route-map {{.NeighborIP}}-out out
 exit-address-family`

func rawConfigForFRR(configTemplate string, myASN uint32, frr *frrcontainer.FRR) (string, error) {
	t, err := template.New("bgp Config Template").Parse(configTemplate)
	if err != nil {
		return "", errors.Wrapf(err, "Failed to create bgp template")
	}

	var b bytes.Buffer
	err = t.Execute(&b,
		struct {
			RouterASN        uint32
			NeighborASN      uint32
			NeighborIP       string
			NeighborPort     uint16
			NeighborPassword string
			NeighborMultiHop bool
		}{
			RouterASN:        myASN,
			NeighborASN:      frr.RouterConfig.ASN,
			NeighborIP:       frr.Ipv4,
			NeighborPort:     frr.RouterConfig.BGPPort,
			NeighborPassword: frr.RouterConfig.Password,
			NeighborMultiHop: frr.NeighborConfig.MultiHop,
		})
	if err != nil {
		return "", errors.Wrapf(err, "Failed to update bgp template")
	}

	return b.String(), nil

}
