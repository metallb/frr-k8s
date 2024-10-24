// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"errors"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/openshift-kni/k8sreporter"
	"go.universe.tf/e2etest/pkg/frr/container"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	"github.com/metallb/frrk8stests/pkg/routes"
	. "github.com/onsi/gomega"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	"go.universe.tf/e2etest/pkg/ipfamily"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

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
		Expect(err).NotTo(HaveOccurred(), "cleanup config in API and in infra containers")

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
		Expect(err).NotTo(HaveOccurred(), "cleanup config in API and in infra containers")
	})

	ginkgo.Context("When restarting the frrk8s deamon pods", func() {

		ginkgo.DescribeTable("external BGP peer maintains routes", func(ipFam ipfamily.Family, prefix string) {
			cnt, err := config.ContainerByName(infra.FRRContainers, "ebgp-single-hop")
			Expect(err).NotTo(HaveOccurred())
			err = container.PairWithNodes(cs, cnt, ipFam, func(container *frrcontainer.FRR) {
				container.NeighborConfig.BFDEnabled = true
			})
			Expect(err).NotTo(HaveOccurred(), "set frr config in infra containers failed")

			peersConfig := config.PeersForContainers([]*frrcontainer.FRR{cnt}, ipFam,
				config.EnableAllowAll, config.EnableGracefulRestart, config.EnableSimpleBFD)

			frrConfigCR := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "graceful-restart-test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/hostname": nodes[0].GetLabels()["kubernetes.io/hostname"],
						},
					},
					BGP: frrk8sv1beta1.BGPConfig{
						BFDProfiles: []frrk8sv1beta1.BFDProfile{
							{
								Name:             "simple",
								ReceiveInterval:  ptr.To[uint32](1000),
								DetectMultiplier: ptr.To[uint32](3),
							},
						},
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

			err = updater.Update(peersConfig.Secrets, frrConfigCR)
			Expect(err).NotTo(HaveOccurred(), "apply the CR in k8s api failed")

			Eventually(func() error {
				for _, p := range peersConfig.Peers() {
					err := routes.CheckNeighborHasPrefix(p.FRR, p.FRR.RouterConfig.VRF, prefix, nodes[:1])
					if err != nil {
						return fmt.Errorf("Neigh %s does not have prefix %s: %w", p.FRR.Name, prefix, err)
					}
				}
				return nil
			}, time.Minute, time.Second).ShouldNot(HaveOccurred(), "route should exist before we restart frr-k8s")

			time.Sleep(5 * time.Second)
			check := func() error {
				var returnError error

				for _, p := range peersConfig.Peers() {
					err = routes.CheckNeighborHasPrefix(p.FRR, p.FRR.RouterConfig.VRF, prefix, nodes[:1])
					if err != nil {
						returnError = errors.Join(returnError, fmt.Errorf("Neigh %s does not have prefix %s: %w", p.FRR.Name, prefix, err))
						ginkgo.By(fmt.Sprintf("%d Neigh %s does not have prefix %s: %s", 0, p.FRR.Name, prefix, err))
						for i := 0; i < 5; i++ {
							if err := routes.CheckNeighborHasPrefix(p.FRR, p.FRR.RouterConfig.VRF, prefix, nodes[:1]); err != nil {
								ginkgo.By(fmt.Sprintf("%d Neigh %s does not have prefix %s: %s", i, p.FRR.Name, prefix, err))
								time.Sleep(time.Second)
							} else {
								ginkgo.By(fmt.Sprintf("%d Neigh %s does have prefix %s: %s", i, p.FRR.Name, prefix, err))
							}
						}
					}
				}
				return returnError
			}

			ginkgo.By(fmt.Sprintf("Start GR test between %s and %s", nodes[0].GetName(), "ebgp-single-hop"))

			c := make(chan struct{})
			go func() { // go restart frr-k8s while Consistently check that route exists
				defer ginkgo.GinkgoRecover()
				err := k8s.RestartFRRK8sPodForNode(cs, nodes[0].GetName())
				Expect(err).NotTo(HaveOccurred(), "frr-k8s pods failed to restart")
				close(c)
			}()

			// 2*time.Minute is important because that is the Graceful Restart timer.
			Consistently(check, time.Minute, time.Second).ShouldNot(HaveOccurred(), "route check failed during frr-k8s-pod reboot")
			Eventually(c, time.Minute, time.Second).Should(BeClosed(), "restarted frr-k8s pods are not yet ready")
		},
			ginkgo.FEntry("IPV4", ipfamily.IPv4, "5.5.5.5/32"),
			ginkgo.Entry("IPV6", ipfamily.IPv6, "2001:db8:5555::5/128"),
		)
	})
})
