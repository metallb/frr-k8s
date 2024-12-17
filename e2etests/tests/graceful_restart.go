// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/openshift-kni/k8sreporter"
	"go.universe.tf/e2etest/pkg/container"
	"go.universe.tf/e2etest/pkg/executor"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/address"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	"github.com/metallb/frrk8stests/pkg/routes"
	. "github.com/onsi/gomega"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clientset "k8s.io/client-go/kubernetes"
)

var _ = ginkgo.Describe("Establish BGP session with EnableGracefulRestart", func() {
	var (
		cs       clientset.Interface
		updater  *config.Updater
		reporter *k8sreporter.KubernetesReporter
		nodes    []corev1.Node
	)

	cleanup := func(u *config.Updater) error {
		for _, c := range infra.FRRContainers {
			if err := c.UpdateBGPConfigFile(frrconfig.Empty); err != nil {
				return fmt.Errorf("clear config in the infra container failed: %w", err)
			}
		}
		if err := u.Clean(); err != nil {
			return fmt.Errorf("clear config in the API failed: %w", err)
		}
		return nil
	}

	ginkgo.BeforeEach(func() {
		var err error

		reporter = dump.NewK8sReporter(k8s.FRRK8sNamespace)
		updater, err = config.NewUpdater()
		Expect(err).NotTo(HaveOccurred())

		err = cleanup(updater)
		Expect(err).NotTo(HaveOccurred(), "cleanup config in API and infra containers")

		cs = k8sclient.New()
		nodes, err = k8s.Nodes(cs)
		Expect(err).NotTo(HaveOccurred())

	})

	ginkgo.AfterEach(func() {
		if ginkgo.CurrentSpecReport().Failed() {
			testName := ginkgo.CurrentSpecReport().LeafNodeText
			dump.K8sInfo(testName, reporter)
			dump.BGPInfo(testName, infra.FRRContainers, cs)
		}

		err := cleanup(updater)
		Expect(err).NotTo(HaveOccurred(), "cleanup config in API and infra containers")
	})

	ginkgo.Context("When restarting the frrk8s deamon pods", func() {
		ginkgo.DescribeTable("both routes on nodes and on the external peers are maintained", func(ipFam ipfamily.Family, prefix, learnRoute string) {

			frrs := config.ContainersForVRF(infra.FRRContainers, "")
			for _, c := range frrs {
				err := frrcontainer.PairWithNodes(cs, c, ipFam, func(frr *frrcontainer.FRR) {
					frr.NeighborConfig.ToAdvertiseV4 = address.FilterForFamily([]string{learnRoute}, ipFam)
					frr.NeighborConfig.ToAdvertiseV6 = address.FilterForFamily([]string{learnRoute}, ipFam)
				})
				Expect(err).NotTo(HaveOccurred(), "set frr config in infra containers failed")
			}

			peersConfig := config.PeersForContainers(frrs, ipFam,
				config.EnableAllowAll, config.EnableReceiveAllowAll, config.EnableGracefulRestart)

			frrConfigCR := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "graceful-restart-test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       infra.FRRK8sASN,
								Neighbors: config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6),
								Prefixes:  []string{prefix},
							},
						},
					},
				},
			}

			err := updater.Update(peersConfig.Secrets, frrConfigCR)
			Expect(err).NotTo(HaveOccurred(), "apply the CR in k8s api failed")

			check := func() error {
				for _, p := range peersConfig.Peers() {
					err := routes.CheckNeighborHasPrefix(p.FRR, p.FRR.RouterConfig.VRF, prefix, nodes)
					if err != nil {
						return fmt.Errorf("Neigh %s does not have prefix %s: %w", p.FRR.Name, prefix, err)
					}
				}
				if err := checkPrefixOnNodes(nodes, learnRoute); err != nil {
					return err
				}
				return nil
			}

			Eventually(check, time.Minute, time.Second).ShouldNot(HaveOccurred(),
				"route should exist before we restart frr-k8s")

			ginkgo.By("GR started")
			c := make(chan struct{})
			go func() { // go restart frr-k8s while Consistently check that route exists
				defer ginkgo.GinkgoRecover()
				err := k8s.RestartFRRK8sPods(cs)
				Expect(err).NotTo(HaveOccurred(), "frr-k8s pods failed to restart")
				close(c)
			}()

			// 2*time.Minute is important because that is the Graceful Restart timer.
			Consistently(check, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
			Eventually(c, time.Minute, time.Second).Should(BeClosed(), "restart FRRK8s pods are not yet ready")
		},
			ginkgo.Entry("IPV4", ipfamily.IPv4, "192.168.2.0/24", "200.200.200.0/24"),
			ginkgo.Entry("IPV6", ipfamily.IPv6, "fc00:f853:ccd:e799::/64", "2001:db8::/64"),
		)
	})
})

// checkPrefixOnNodes checks that a prefix has at least one route on the
// each node. There is no check on nexthops because not all routes are install
// on the host (iBGP yes, eBGP no when iBGP exist for example) and there is a
// different behavior between ipv4,ipv6.
// This function get routes directly from the node and not from `vtysh show route`
// because we want to get routes while FRR process restarts.
func checkPrefixOnNodes(nodes []corev1.Node, prefix string) error {
	want, err := netip.ParsePrefix(prefix)
	if err != nil {
		return err
	}

	for _, n := range nodes {
		exc := executor.ForContainer(n.Name)
		m, err := container.BGPRoutes(exc, "eth0")
		if err != nil {
			return err
		}
		if _, exist := m[want]; !exist {
			return fmt.Errorf("local k8s node route %s was not found", prefix)
		}
	}
	return nil
}
