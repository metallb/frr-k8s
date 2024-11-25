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

	"go.universe.tf/e2etest/pkg/executor"
	"go.universe.tf/e2etest/pkg/frr"

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

var _ = ginkgo.Describe("Establish BGP session with EnableGracefulRestart", func() {
	var (
		cs         clientset.Interface
		updater    *config.Updater
		reporter   *k8sreporter.KubernetesReporter
		nodes      []corev1.Node
		prefixesV4 = scaleUP(200)
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

		seed := ginkgo.GinkgoRandomSeed()
		testName := fmt.Sprintf("%s-%d", ginkgo.CurrentSpecReport().LeafNodeText, seed)
		if ginkgo.CurrentSpecReport().Failed() {
			testName += "-failed"
		}
		ginkgo.By(testName)
		dump.K8sInfo(testName, reporter)
		dump.BGPInfo(testName, infra.FRRContainers, cs)
	})

	ginkgo.Context("When restarting the frrk8s deamon pods", func() {

		ginkgo.DescribeTable("external BGP peer maintains routes", func(ipFam ipfamily.Family, prefix []string) {
			frrs := config.ContainersForVRF(infra.FRRContainers, "")
			// cnt, err := config.ContainerByName(infra.FRRContainers, "ebgp-multi-hop")
			// frrs := []*frrcontainer.FRR{cnt}
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
					// NodeSelector: metav1.LabelSelector{
					// 	MatchLabels: map[string]string{
					// 		"kubernetes.io/hostname": nodes[0].GetLabels()["kubernetes.io/hostname"],
					// 	},
					//					},
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       infra.FRRK8sASN,
								Neighbors: config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6),
								Prefixes:  prefix,
							},
						},
					},
				},
			}
			ginkgo.By("Before GR test")

			err := updater.Update(peersConfig.Secrets, frrConfigCR)
			Expect(err).NotTo(HaveOccurred(), "apply the CR in k8s api failed")
			Eventually(func() error {
				for _, p := range peersConfig.Peers() {
					ValidateFRRPeeredWithNodes(nodes, &p.FRR, ipFam)
					neighbors, err := frr.NeighborsInfo(p.FRR)
					Expect(err).NotTo(HaveOccurred())
					for _, n := range neighbors {
						Expect(n.GRInfo.RemoteGrMode).Should(Equal("Restart"))
					}

					err = routes.CheckNeighborHasPrefix(p.FRR, p.FRR.RouterConfig.VRF, prefix[0], nodes)
					if err != nil {
						return fmt.Errorf("Neigh %s does not have prefixes: %w", p.FRR.Name, err)
					}
				}
				return nil
			}, time.Minute, time.Second).ShouldNot(HaveOccurred(), "route should exist before we restart frr-k8s")

			ginkgo.By("Start GR test")
			c := make(chan struct{})
			go func() { // go restart frr-k8s while Consistently check that route exists
				defer ginkgo.GinkgoRecover()
				err := k8s.RestartFRRK8sPods(cs)
				Expect(err).NotTo(HaveOccurred(), "frr-k8s pods failed to restart")
				for _, p := range peersConfig.Peers() {
					ValidateFRRPeeredWithNodes(nodes, &p.FRR, ipFam)
				}
				ginkgo.By("FRRK8s pod restarted and BGP established")
				close(c)
			}()

			check := func() error {
				var returnError error

				for _, p := range peersConfig.Peers() {
					err := checkRoutes(p.FRR, prefix)
					if err != nil {
						returnError = errors.Join(returnError, fmt.Errorf("Neigh %s : %w", p.FRR.Name, err))
						for i := 0; i < 20; i++ {
							if err := checkRoutes(p.FRR, prefix); err != nil {
								ginkgo.By(fmt.Sprintf("%d Neigh %s does NOT have prefix %v", i, p.FRR.Name, err))
							} else {
								ginkgo.By(fmt.Sprintf("%d Neigh %s does have prefix", i, p.FRR.Name))
							}
							time.Sleep(time.Second)
						}
					}
				}
				return returnError
			}

			// 2*time.Minute is important because that is the Graceful Restart timer.
			Consistently(check, 30*time.Second, time.Second).ShouldNot(HaveOccurred())
			Eventually(c, time.Minute, time.Second).Should(BeClosed(), "restart FRRK8s pods are not yet ready")
		},
			ginkgo.Entry("IPV4", ipfamily.IPv4, prefixesV4),
//			ginkgo.Entry("IPV6", ipfamily.IPv6, []string{"2001:db8:5555::5/128"}),
		)
	})
})

func checkRoutes(cnt frrcontainer.FRR, want []string) error {
	m := sliceToMap(want)
	v4, _, err := frr.Routes(cnt)
	if err != nil {
		// ignore the docker exec errors
		return nil
	}
	if len(m) == 0 {
		return fmt.Errorf("nil map m")
	}
	if len(v4) == 0 {
		IPRoutes(cnt)
		return fmt.Errorf("nil map v4")
	}
	for _, r := range v4 {
		// if r.Stale {
		// 	fmt.Printf("S")
		// }
		delete(m, r.Destination.String())
	}
	if len(m) > 0 {
		return fmt.Errorf("%d routes %+v not found ", len(m), getKeys(m))
	}
	return nil
}

func scaleUP(size int) []string {
	if size > 255 {
		panic("255 is max")
	}

	ret := []string{}
	for i := 0; i < size; i++ {
		ret = append(ret, fmt.Sprintf("5.5.5.%d/32", i))
	}

	return ret
}

func sliceToMap(slice []string) map[string]bool {
	m := make(map[string]bool)
	for _, v := range slice {
		m[v] = true
	}
	return m
}
func getKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m)) // Initialize slice with the capacity of the map length
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
func IPRoutes(exec executor.Executor) error {
	cmd := "show ip route bgp"
	res, err := exec.Exec("vtysh", "-c", cmd)
	if err != nil {
		return errors.Join(err, errors.New("Failed to query routes"))
	}
	fmt.Println("res", res)
	return nil
}
