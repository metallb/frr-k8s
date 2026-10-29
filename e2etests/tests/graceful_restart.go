// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/openshift-kni/k8sreporter"
	"go.universe.tf/e2etest/pkg/container"
	"go.universe.tf/e2etest/pkg/executor"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
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
			if err := c.UpdateConfigFile(frrconfig.Empty); err != nil {
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
		ginkgo.DescribeTable("both routes on nodes and on the external peers are maintained", func(ipFam ipfamily.Family, prefix string) {
			externalPrefixes := []string{}
			frrs := config.ContainersForVRF(infra.FRRContainers, "")
			for i, c := range frrs {
				l := fmt.Sprintf("200.200.%d.0/24", i)
				modify := func(frr *frrcontainer.FRR) {
					frr.NeighborConfig.ToAdvertiseV4 = []string{l}
				}

				if ipFam == ipfamily.IPv6 {
					l = fmt.Sprintf("2001:db%d::/64", i)
					modify = func(frr *frrcontainer.FRR) {
						frr.NeighborConfig.ToAdvertiseV6 = []string{l}
					}
				}

				err := frrcontainer.PairWithNodes(cs, c, ipFam, modify)
				Expect(err).NotTo(HaveOccurred(), "set frr config in infra containers failed")
				externalPrefixes = append(externalPrefixes, l)
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
					if err := routes.CheckNeighborHasPrefix(p.FRR, p.FRR.RouterConfig.VRF, prefix, nodes); err != nil {
						return fmt.Errorf("Neigh %s does not have prefix %s: %w", p.FRR.Name, prefix, err)
					}
				}
				for _, n := range nodes {
					if err := checkPrefixesOnNode(n.Name, externalPrefixes); err != nil {
						return err
					}
				}
				return nil
			}

			Eventually(check, time.Minute, time.Second).ShouldNot(HaveOccurred(),
				"routes should exist before we restart frr-k8s")

			// The external FRRs get their "router bgp" recreated by the
			// cleanup/pairing done at the beginning of the test, so for the
			// next restart-time (120s by default) they advertise the Restart
			// State bit in their graceful restart capability. Per RFC 4724
			// section 4.2 the restarting speaker does not wait for the
			// End-of-RIB of such peers, which makes the restarted frr-k8s
			// bgpd end its deferral right away and zebra purge the preserved
			// routes before the peers re-advertise them. That is not the
			// scenario this test covers, so we wait for the external peers
			// to act as steady helpers before restarting the pods.
			ginkgo.By("waiting for the external peers to leave their restart window")
			Eventually(func() error {
				return peersOutOfRestartWindow(cs)
			}, 2*frrStartupRestartWindow, 5*time.Second).ShouldNot(HaveOccurred(),
				"external peers should have been up for longer than their restart window")

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
			ginkgo.Entry("IPV4", ipfamily.IPv4, "192.168.2.0/24"),
			ginkgo.Entry("IPV6", ipfamily.IPv6, "fc00:f853:ccd:e799::/64"),
		)
	})
})

// checkPrefixesOnNode checks that prefixes have at one kernel route on the
// the node. This function get routes directly from the node `docker exec ip
// route` and not from `vtysh show route` because we need to get routes while
// FRR process restarts.
func checkPrefixesOnNode(node string, prefixes []string) error {
	exc := executor.ForContainer(node)
	routes, err := container.BGPRoutes(exc, "")
	if err != nil {
		return err
	}

	for _, prefix := range prefixes {
		want, err := netip.ParsePrefix(prefix)
		if err != nil {
			return err
		}

		if _, exist := routes[want]; !exist {
			return fmt.Errorf("no route for prefix %s was found on node %s", prefix, node)
		}
	}
	return nil
}

// frrStartupRestartWindow is how long a freshly created FRR BGP instance
// advertises the Restart State bit (bgpd arms its startup timer with the
// default restart time, regardless of the configured one).
const frrStartupRestartWindow = 120 * time.Second

// bgpNeighborGRState is the subset of "show bgp neighbors json" needed to
// tell whether a peer advertised the Restart State bit when the session came
// up and how long ago that was.
type bgpNeighborGRState struct {
	BGPState   string `json:"bgpState"`
	UptimeMsec int64  `json:"bgpTimerUpMsec"`
	GRInfo     struct {
		RBit   bool `json:"rBit"`
		Timers struct {
			ReceivedRestartTimer int64 `json:"receivedRestartTimer"`
		} `json:"timers"`
	} `json:"gracefulRestartInfo"`
}

// peersOutOfRestartWindow returns an error if any peer of any frr-k8s pod
// advertised the Restart State bit and its session has been up for less than
// the restart window. FRR sets the bit for a fixed time after a BGP instance
// is created, and a session cannot be older than the instance it belongs to,
// so once the session uptime exceeds the window the peer is guaranteed to
// leave the bit unset in its next OPEN. The bit reported by vtysh is the one
// received at session setup and never clears while the session stays up.
func peersOutOfRestartWindow(cs clientset.Interface) error {
	pods, err := k8s.FRRK8sPods(cs)
	if err != nil {
		return err
	}
	for _, pod := range pods {
		exc := executor.ForPod(pod.Namespace, pod.Name, k8s.FRRContainerName)
		out, err := exc.Exec("vtysh", "-c", "show bgp neighbors json")
		if err != nil {
			return fmt.Errorf("failed to query the neighbors of pod %s: %w", pod.Name, err)
		}
		neighbors := map[string]bgpNeighborGRState{}
		if err := json.Unmarshal([]byte(out), &neighbors); err != nil {
			return fmt.Errorf("failed to parse the neighbors of pod %s: %w", pod.Name, err)
		}
		if len(neighbors) == 0 {
			return fmt.Errorf("pod %s has no neighbors", pod.Name)
		}
		for ip, n := range neighbors {
			if n.BGPState != "Established" {
				return fmt.Errorf("peer %s of pod %s is %s", ip, pod.Name, n.BGPState)
			}
			if !n.GRInfo.RBit {
				continue
			}
			// A peer that configured a restart time advertises that value,
			// but the startup window stays at the default, so the effective
			// window is never shorter than the startup one.
			restartWindow := frrStartupRestartWindow
			if advertised := time.Duration(n.GRInfo.Timers.ReceivedRestartTimer) * time.Second; advertised > restartWindow {
				restartWindow = advertised
			}
			// bgpTimerUpMsec has a one second resolution, keep a margin.
			uptime := time.Duration(n.UptimeMsec) * time.Millisecond
			if uptime < restartWindow+time.Second {
				return fmt.Errorf("peer %s of pod %s advertised the restart state bit %s ago, restart window is %s",
					ip, pod.Name, uptime, restartWindow)
			}
		}
	}
	return nil
}
