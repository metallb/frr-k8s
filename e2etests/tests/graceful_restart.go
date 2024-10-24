// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"errors"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/openshift-kni/k8sreporter"
	"go.universe.tf/e2etest/pkg/frr/container"

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

	clientset "k8s.io/client-go/kubernetes"
)

var _ = ginkgo.FDescribe("Establish BGP session with EnableGracefulRestart", func() {
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
	})

	ginkgo.Context("When restarting the frrk8s deamon pods", func() {

		ginkgo.DescribeTable("external BGP peer maintains routes", func(ipFam ipfamily.Family, prefix string) {
			frrs := config.ContainersForVRF(infra.FRRContainers, "")
			for _, c := range frrs {
				err := container.PairWithNodes(cs, c, ipFam)
				Expect(err).NotTo(HaveOccurred(), "set frr config in infra containers failed")
			}

			peersConfig := config.PeersForContainers(frrs, ipFam,
				config.EnableAllowAll, config.EnableGracefulRestart)

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

			Eventually(func() error {
				for _, p := range peersConfig.Peers() {
					err := routes.CheckNeighborHasPrefix(p.FRR, p.FRR.RouterConfig.VRF, prefix, nodes)
					if err != nil {
						return fmt.Errorf("Neigh %s does not have prefix %s: %w", p.FRR.Name, prefix, err)
					}
				}
				return nil
			}, time.Minute, time.Second).ShouldNot(HaveOccurred(), "route should exist before we restart frr-k8s")

			time.Sleep(5 * time.Second)
			c := make(chan struct{})
			go func() { // go restart frr-k8s while Consistently check that route exists
				defer ginkgo.GinkgoRecover()
				err := k8s.RestartFRRK8sPods(cs)
				Expect(err).NotTo(HaveOccurred(), "frr-k8s pods failed to restart")
				close(c)
			}()

			check := func() error {
				var returnError error

				for _, p := range peersConfig.Peers() {
					err = routes.CheckNeighborHasPrefix(p.FRR, p.FRR.RouterConfig.VRF, prefix, nodes)
					if err != nil {
						returnError = errors.Join(returnError, fmt.Errorf("Neigh %s does not have prefix %s: %w", p.FRR.Name, prefix, err))
						for i := 0; i < 5; i++ {
							if err := routes.CheckNeighborHasPrefix(p.FRR, p.FRR.RouterConfig.VRF, prefix, nodes); err != nil {
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

			// 2*time.Minute is important because that is the Graceful Restart timer.
			Consistently(check, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
			Eventually(c, time.Minute, time.Second).Should(BeClosed(), "restart FRRK8s pods are not yet ready")
		},
			ginkgo.Entry("IPV4", ipfamily.IPv4, "5.5.5.5/32"),
			ginkgo.Entry("IPV6", ipfamily.IPv6, "2001:db8:5555::5/128"),
		)
	})
})
