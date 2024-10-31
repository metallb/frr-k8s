// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/openshift-kni/k8sreporter"
	"go.universe.tf/e2etest/pkg/frr"
	"go.universe.tf/e2etest/pkg/frr/container"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	. "github.com/onsi/gomega"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	"go.universe.tf/e2etest/pkg/ipfamily"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clientset "k8s.io/client-go/kubernetes"
)

var _ = ginkgo.FDescribe("Establish BGP session", func() {
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

		err = k8s.RestartFRRK8sPods(cs)
		Expect(err).NotTo(HaveOccurred(), "frr-k8s pods failed to restart")

		ginkgo.By("Start")
	})

	ginkgo.AfterEach(func() {
		testName := ginkgo.CurrentSpecReport().LeafNodeText
		if ginkgo.CurrentSpecReport().Failed() {
			testName += "-failed"
		}
		dump.K8sInfo(testName, reporter)
		dump.BGPInfo(testName, infra.FRRContainers, cs)

		err := cleanup(updater)
		Expect(err).NotTo(HaveOccurred(), "cleanup config in API and in infra containers")
	})

	ginkgo.DescribeTable("dropped connection zero", func(bfd bool) {
		cnt, err := config.ContainerByName(infra.FRRContainers, "ebgp-single-hop")
		Expect(err).NotTo(HaveOccurred())
		if bfd {
			err = container.PairWithNodes(cs, cnt, ipfamily.IPv4, func(container *frrcontainer.FRR) {
				container.NeighborConfig.BFDEnabled = true
			})
		} else {
			err = container.PairWithNodes(cs, cnt, ipfamily.IPv4)
		}
		Expect(err).NotTo(HaveOccurred(), "set frr config in infra containers failed")

		var peersConfig config.PeersConfig
		if bfd {
			peersConfig = config.PeersForContainers([]*frrcontainer.FRR{cnt}, ipfamily.IPv4,
				config.EnableSimpleBFD)
		} else {
			peersConfig = config.PeersForContainers([]*frrcontainer.FRR{cnt}, ipfamily.IPv4)
		}

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
					Routers: []frrk8sv1beta1.Router{
						{
							ASN:       infra.FRRK8sASN,
							Neighbors: config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6),
							Prefixes:  []string{"5.5.5.5/32"},
						},
					},
				},
			},
		}
		if bfd {
			t := false
			frrConfigCR.Spec.BGP.BFDProfiles = []frrk8sv1beta1.BFDProfile{
				{
					Name: "simple",
					//					ReceiveInterval:  ptr.To[uint32](1000),
					//					DetectMultiplier: ptr.To[uint32](5),
					PassiveMode: &t,
				},
			}
		}

		err = updater.Update(peersConfig.Secrets, frrConfigCR)
		Expect(err).NotTo(HaveOccurred(), "apply the CR in k8s api failed")

		ValidateFRRPeeredWithNodes(nodes[:1], cnt, ipfamily.IPv4)
		neighbors, err := frr.NeighborsInfo(cnt)
		Expect(err).NotTo(HaveOccurred())
		for _, n := range neighbors {
			Expect(n.ConnectionsDropped).To(Equal(0))
		}
	},
		ginkgo.Entry("IPV4 without BFD", false),
		ginkgo.FEntry("IPV4 with BFD", true),
	)
})
