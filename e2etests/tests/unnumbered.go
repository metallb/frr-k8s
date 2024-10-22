// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.universe.tf/e2etest/pkg/executor"
	"go.universe.tf/e2etest/pkg/frr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
)

var _ = ginkgo.Describe("Unnumbered configure BGP peering", func() {
	var (
		nodes   []corev1.Node
		updater *config.Updater
	)

	setup := func(peerName string) *frrcontainer.FRR {
		c := frrcontainer.Config{
			Name:    fmt.Sprintf("unnumbered-p2p-%s", peerName),
			Image:   "quay.io/frrouting/frr:9.1.0",
			Network: "none",
			Router: frrconfig.RouterConfig{
				ASN:     650001,
				BGPPort: 179,
			},
		}

		peers, err := frrcontainer.Create(map[string]frrcontainer.Config{"peer": c})
		Expect(err).NotTo(HaveOccurred())
		peer := peers[0]

		nodes, err = k8s.Nodes(k8sclient.New())
		Expect(err).NotTo(HaveOccurred())
		ginkgo.By(fmt.Sprintf("Picked node %s", nodes[0].GetName()))
		err = wirePeer(peer.Name, nodes[:1])
		Expect(err).NotTo(HaveOccurred())
		err = peer.UpdateBGPConfigFile(unnumberedPeerFRRConfig)
		Expect(err).NotTo(HaveOccurred())
		return peer
	}

	ginkgo.BeforeEach(func() {
		if _, found := os.LookupEnv("RUN_FRR_CONTAINER_ON_HOST_NETWORK"); found {
			ginkgo.Skip("Skipping this test because SKIP_TEST is set to true")
		}
		var err error
		updater, err = config.NewUpdater()
		Expect(err).NotTo(HaveOccurred())
		err = updater.Clean()
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.AfterEach(func() {
	})

	ginkgo.Context("with native neighbor config", func() {
		var peer *frrcontainer.FRR
		ginkgo.BeforeEach(func() {
			peer = setup("native")
		})
		ginkgo.AfterEach(func() {
			reporter := dump.NewK8sReporter(k8s.FRRK8sNamespace)
			if ginkgo.CurrentSpecReport().Failed() {
				testName := ginkgo.CurrentSpecReport().FullText()
				dump.K8sInfo(testName, reporter)
				dump.BGPInfo(testName, []*frrcontainer.FRR{peer}, k8sclient.New())
			}

			err := frrcontainer.Delete([]*frrcontainer.FRR{peer})
			Expect(err).NotTo(HaveOccurred())
		})

		ginkgo.It("session should be established and routes to be validated", func() {
			cr := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unnumbered-native",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN: infra.FRRK8sASN,
								Neighbors: []frrk8sv1beta1.Neighbor{
									{
										DynamicASN: frrk8sv1beta1.ExternalASNMode,
										Interface:  "net0",
										ToAdvertise: frrk8sv1beta1.Advertise{
											Allowed: frrk8sv1beta1.AllowedOutPrefixes{
												Mode: frrk8sv1beta1.AllowAll,
											},
										},
									},
								},
								Prefixes: []string{"5.5.5.5/32"},
							},
						},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/hostname": nodes[0].GetLabels()["kubernetes.io/hostname"],
						},
					},
				},
			}

			updater, err := config.NewUpdater()
			Expect(err).NotTo(HaveOccurred())
			err = updater.Update([]corev1.Secret{}, cr)
			Expect(err).NotTo(HaveOccurred())

			validate(peer)
		})
	})

	ginkgo.Context("with raw config", func() {
		var peer *frrcontainer.FRR

		ginkgo.BeforeEach(func() {
			peer = setup("raw")
		})
		ginkgo.AfterEach(func() {
			reporter := dump.NewK8sReporter(k8s.FRRK8sNamespace)
			testName := ginkgo.CurrentSpecReport().FullText()
			dump.K8sInfo(testName, reporter)
			dump.BGPInfo(testName, []*frrcontainer.FRR{peer}, k8sclient.New())

			err := frrcontainer.Delete([]*frrcontainer.FRR{peer})
			Expect(err).NotTo(HaveOccurred())
		})
		ginkgo.It("session should be established and routes to be validated", func() {
			raw := `
		frr defaults traditional
		no ipv6 forwarding
		router bgp 65000
			no bgp ebgp-requires-policy
			no bgp network import-check

			neighbor net0 interface remote-as external
			neighbor net0 description TOR
			neighbor net0 port 179
			neighbor net0 bfd
			neighbor net0 bfd profile fornet0
			neighbor net0 timers 30 90
			neighbor net0 timers connect 30
		!	neighbor net0 graceful-restart
		!	neighbor net0 password "xyz"

			address-family ipv6 unicast
				network fc00:f888:ccd:e793::1/128
				neighbor net0 activate
				neighbor net0 route-map net0-in in
				neighbor net0 route-map net0-out out
			exit-address-family
			address-family ipv4 unicast
				network 5.5.5.5/32
				neighbor net0 activate
				neighbor net0 route-map net0-in in
				neighbor net0 route-map net0-out out
			exit-address-family
		exit
		bfd
		 profile fornet0
			transmit-interval 61
			receive-interval 60
			echo transmit-interval 62
			echo receive-interval 62
		 exit
		 !
		exit
		!
		ip prefix-list net0-pl-ipv6 seq 1 permit 5.5.5.5/32
		!
		ipv6 prefix-list net0-pl-ipv6 seq 2 deny any
		!
		route-map net0-in deny 20
		exit
		!
		route-map net0-out permit 1
		 match ip address prefix-list net0-pl-ipv6
		exit
		!
		route-map net0-out permit 2
		 match ipv6 address prefix-list net0-pl-ipv6
		exit
		!
		end
`

			cr := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unnumbered-raw",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/hostname": nodes[0].GetLabels()["kubernetes.io/hostname"],
						},
					},
					Raw: frrk8sv1beta1.RawConfig{
						Config:   raw,
						Priority: 5,
					},
				},
			}

			var err error
			updater, err = config.NewUpdater()
			Expect(err).NotTo(HaveOccurred())
			err = updater.Update([]corev1.Secret{}, cr)
			Expect(err).NotTo(HaveOccurred())

			validate(peer)
		})
	})
})

func validate(peer *frrcontainer.FRR) {
	Eventually(func() error {
		neighbors, err := frr.NeighborsInfo(peer)
		Expect(err).NotTo(HaveOccurred())
		for _, n := range neighbors {
			if !n.Connected {
				return fmt.Errorf("node %v BGP session not established", n)
			}
		}
		return nil
	}, 2*time.Minute, 10*time.Second).ShouldNot(HaveOccurred(),
		"timed out waiting to validate nodes peered with the frr instance")

	// NOTE: we define the MAC address of net0, and therefore we can define the LLA
	nextHops := []net.IP{net.ParseIP("fe80::dcad:beff:feff:1160")} //		net.ParseIP("fe80::dcad:beff:feff:1161"),

	Eventually(func() error {
		v4, _, err := frr.Routes(peer)
		if err != nil {
			return err
		}
		v, exist := v4["5.5.5.5"]
		if !exist {
			return fmt.Errorf("missing entry")
		}
		Expect(v.NextHops).To(ConsistOf(nextHops))
		// v, exist = v6["fc00:f888:ccd:e793::1"]
		// if !exist {
		//	return fmt.Errorf("missing entry")
		// }
		// Expect(v.NextHops).To(ConsistOf(nextHops))
		return nil
	}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred(), "peer should have the routes")
}

func wirePeer(peerName string, toNode []corev1.Node) error {
	from, err := exec.Command(executor.ContainerRuntime, "inspect", "-f", "{{ .NetworkSettings.SandboxKey }}", peerName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s - %w", from, err)
	}

	from = bytes.TrimSpace(from)
	peer := executor.ForContainer(peerName)
	for i, n := range toNode {
		to, err := exec.Command(executor.ContainerRuntime,
			"inspect", "-f", "{{ .NetworkSettings.SandboxKey }}", n.GetName()).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s - %w", to, err)
		}
		to = bytes.TrimSpace(to)

		//sudo asks password if needs to
		c := fmt.Sprintf("ip link add eth0%d netns %s type veth peer name net0", i, from)
		if out, err := executor.Host.Exec("sudo", strings.Split(c, " ")...); err != nil {
			return fmt.Errorf("%s - %w", out, err)
		}

		c = fmt.Sprintf("ip link set dev net0 netns %s address de:ad:be:ff:11:6%d", to, i)
		if out, err := executor.Host.Exec("sudo", strings.Split(c, " ")...); err != nil {
			return fmt.Errorf("%s - %w", out, err)
		}

		node := executor.ForContainer(n.GetName())
		// c = fmt.Sprintf("-6 addr add 2001:db8:85a3::10%d/64 dev net0", i)
		// out, err = node.Exec("ip", strings.Split(c, " ")...)
		// if err != nil {
		//	panic(out)
		// }

		if out, err := node.Exec("ip", "link", "set", "dev", "net0", "up"); err != nil {
			return fmt.Errorf("%s - %w", out, err)
		}

		if out, err := peer.Exec("ip", "link", "set", "dev", fmt.Sprintf("eth0%d", i), "up"); err != nil {
			return fmt.Errorf("%s - %w", out, err)
		}

		// if out, err := peer.Exec("ip", "-6", "addr", "add", fmt.Sprintf("2001:db8:85a3::1%d/64", i), "dev", fmt.Sprintf("eth0%d", i)); err != nil {
		//return fmt.Errorf("%s - %w", out, err)
		// }
	}
	return nil
}

var unnumberedPeerFRRConfig = `
frr defaults datacenter
hostname tor
no ipv6 forwarding
log file /tmp/frr.log
!
interface eth00
		ipv6 nd ra-interval 10
		no ipv6 nd suppress-ra
exit
!
interface eth01
		ipv6 nd ra-interval 10
		no ipv6 nd suppress-ra
exit
!
interface eth02
		ipv6 nd ra-interval 10
		no ipv6 nd suppress-ra
exit
!
interface lo
		ip address 200.100.100.1/24
exit
!
router bgp 65004
		bgp router-id 11.11.11.254
		neighbor MTLB peer-group
		neighbor MTLB passive
		neighbor MTLB remote-as external
		neighbor MTLB description LEAF-MTLB
		neighbor eth00 interface peer-group MTLB
		neighbor eth00 description k8s-node
		!
		address-family ipv4 unicast
				redistribute connected
				neighbor MTLB activate
		exit-address-family
		!
		address-family ipv6 unicast
				redistribute connected
				neighbor MTLB activate
		exit-address-family
	exit`
