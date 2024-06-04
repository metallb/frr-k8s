// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"
	"time"

	"errors"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"go.universe.tf/e2etest/pkg/executor"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	"go.universe.tf/e2etest/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var (
	PrometheusNamespace string
)

var _ = ginkgo.Describe("Metrics", func() {
	var cs clientset.Interface
	var promPod *corev1.Pod

	defer ginkgo.GinkgoRecover()
	updater, err := config.NewUpdater()
	Expect(err).NotTo(HaveOccurred())
	reporter := dump.NewK8sReporter(k8s.FRRK8sNamespace)

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
		err := updater.Clean()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()

		promPod, err = metrics.PrometheusPod(cs, PrometheusNamespace)
		Expect(err).NotTo(HaveOccurred())
	})

	type params struct {
		vrf           string
		ipFamily      ipfamily.Family
		myAsn         uint32
		toAdvertiseV4 []string // V4 prefixes that the nodes receive from the containers
		toAdvertiseV6 []string // V6 prefixes that the nodes receive from the containers
		prefixes      []string // all prefixes that the nodes advertise to the containers
		modifyPeers   func([]config.Peer, []config.Peer)
		validate      func(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, ipfam ipfamily.Family)
	}

	ginkgo.DescribeTable("expose prometheus metrics", func(p params) {
		frrs := config.ContainersForVRF(infra.FRRContainers, p.vrf)
		peersConfig := config.PeersForContainers(frrs, p.ipFamily)
		p.modifyPeers(peersConfig.PeersV4, peersConfig.PeersV6)
		neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)

		config := frrk8sv1beta1.FRRConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: k8s.FRRK8sNamespace,
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
			err := frrcontainer.PairWithNodes(cs, c, p.ipFamily, func(frr *frrcontainer.FRR) {
				frr.NeighborConfig.ToAdvertiseV4 = p.toAdvertiseV4
				frr.NeighborConfig.ToAdvertiseV6 = p.toAdvertiseV6
			})
			Expect(err).NotTo(HaveOccurred())
		}

		err := updater.Update(peersConfig.Secrets, config)
		Expect(err).NotTo(HaveOccurred())

		nodes, err := k8s.Nodes(cs)
		Expect(err).NotTo(HaveOccurred())

		for _, c := range frrs {
			ValidateFRRPeeredWithNodes(nodes, c, p.ipFamily)
		}

		pods, err := k8s.FRRK8sPods(cs)
		Expect(err).NotTo(HaveOccurred())

		ginkgo.By("validating")
		p.validate(pods, frrs, promPod, p.ipFamily)
	},
		ginkgo.Entry("IPV4 - advertise and receive", params{
			ipFamily:      ipfamily.IPv4,
			vrf:           "",
			myAsn:         infra.FRRK8sASN,
			toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"},
			prefixes:      []string{"192.168.100.0/24", "192.169.101.0/24"},
			modifyPeers: func(ppV4 []config.Peer, _ []config.Peer) {
				for i := range ppV4 {
					ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					ppV4[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
				}
			},
			validate: func(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, ipfam ipfamily.Family) {
				ValidatePodBGPMetrics(frrPods, frrContainers, promPod, ipfam)
				ValidatePodAnnounceMetrics(frrPods, frrContainers, promPod, 2, ipfam)
				ValidatePodReceiveMetrics(frrPods, frrContainers, promPod, 3, ipfam)
				ValidatePodK8SClientUpMetrics(frrPods, frrContainers, promPod, ipfam)
			},
		}),
		ginkgo.Entry("IPV4 - VRF - advertise and receive", params{
			ipFamily:      ipfamily.IPv4,
			vrf:           infra.VRFName,
			myAsn:         infra.FRRK8sASNVRF,
			toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"},
			prefixes:      []string{"192.168.100.0/24", "192.169.101.0/24"},
			modifyPeers: func(ppV4 []config.Peer, _ []config.Peer) {
				for i := range ppV4 {
					ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					ppV4[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
				}
			},
			validate: func(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, ipfam ipfamily.Family) {
				ValidatePodBGPMetrics(frrPods, frrContainers, promPod, ipfam)
				ValidatePodAnnounceMetrics(frrPods, frrContainers, promPod, 2, ipfam)
				ValidatePodReceiveMetrics(frrPods, frrContainers, promPod, 3, ipfam)
				ValidatePodK8SClientUpMetrics(frrPods, frrContainers, promPod, ipfam)
			},
		}),
		ginkgo.Entry("IPV6 - advertise and receive", params{
			ipFamily:      ipfamily.IPv6,
			vrf:           "",
			myAsn:         infra.FRRK8sASN,
			toAdvertiseV6: []string{"fc00:f853:ccd:e899::/64", "fc00:f853:ccd:e900::/64", "fc00:f853:ccd:e901::/64"},
			prefixes:      []string{"fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
			modifyPeers: func(_ []config.Peer, ppV6 []config.Peer) {
				for i := range ppV6 {
					ppV6[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					ppV6[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
				}
			},
			validate: func(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, ipfam ipfamily.Family) {
				ValidatePodBGPMetrics(frrPods, frrContainers, promPod, ipfam)
				ValidatePodAnnounceMetrics(frrPods, frrContainers, promPod, 2, ipfam)
				ValidatePodReceiveMetrics(frrPods, frrContainers, promPod, 3, ipfam)
				ValidatePodK8SClientUpMetrics(frrPods, frrContainers, promPod, ipfam)
			},
		}),
		ginkgo.Entry("DUALSTACK - advertise and receive", params{
			ipFamily:      ipfamily.DualStack,
			vrf:           "",
			myAsn:         infra.FRRK8sASN,
			toAdvertiseV4: []string{"192.168.2.0/24", "192.169.2.0/24", "192.170.2.0/24"},
			toAdvertiseV6: []string{"fc00:f853:ccd:e899::/64", "fc00:f853:ccd:e900::/64", "fc00:f853:ccd:e901::/64"},
			prefixes:      []string{"192.168.100.0/24", "192.169.101.0/24", "fc00:f853:ccd:e799::/64", "fc00:f853:ccd:e800::/64"},
			modifyPeers: func(ppV4 []config.Peer, ppV6 []config.Peer) {
				for i := range ppV4 {
					ppV4[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					ppV4[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
				}

				for i := range ppV6 {
					ppV6[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
					ppV6[i].Neigh.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
				}
			},
			validate: func(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, ipfam ipfamily.Family) {
				ValidatePodBGPMetrics(frrPods, frrContainers, promPod, ipfam)
				ValidatePodAnnounceMetrics(frrPods, frrContainers, promPod, 2, ipfam)
				ValidatePodReceiveMetrics(frrPods, frrContainers, promPod, 3, ipfam)
				ValidatePodK8SClientUpMetrics(frrPods, frrContainers, promPod, ipfam)
			},
		}),
	)
	ginkgo.It("IPV4 - exposes metrics for bad config", func() {
		frrs := config.ContainersForVRF(infra.FRRContainers, "")
		peersConfig := config.PeersForContainers(frrs, ipfamily.IPv4)
		for i := range peersConfig.PeersV4 {
			peersConfig.PeersV4[i].Neigh.PasswordSecret = corev1.SecretReference{
				Name:      "nonexisting",
				Namespace: k8s.FRRK8sNamespace,
			}
		}
		neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)

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
							Prefixes:  []string{"192.168.2.0/24", "192.169.2.0/24"},
						},
					},
				},
			},
		}

		err := updater.Update(peersConfig.Secrets, config)
		Expect(err).NotTo(HaveOccurred())

		pods, err := k8s.FRRK8sPods(cs)
		Expect(err).NotTo(HaveOccurred())

		ginkgo.By("validating")
		ValidatePodK8SClientErrorMetrics(pods, frrs, promPod, ipfamily.IPv4)
	})

	ginkgo.DescribeTable("BFD metrics from FRR", func(bfdProfile frrk8sv1beta1.BFDProfile, ipFamily ipfamily.Family, vrfName string) {
		frrs := config.ContainersForVRF(infra.FRRContainers, vrfName)
		peersConfig := config.PeersForContainers(frrs, ipFamily)
		neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)
		for i := range neighbors {
			neighbors[i].BFDProfile = bfdProfile.Name
		}
		myAsn := infra.FRRK8sASN
		if vrfName != "" {
			myAsn = infra.FRRK8sASNVRF
		}
		config := frrk8sv1beta1.FRRConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: k8s.FRRK8sNamespace,
			},
			Spec: frrk8sv1beta1.FRRConfigurationSpec{
				BGP: frrk8sv1beta1.BGPConfig{
					Routers: []frrk8sv1beta1.Router{
						{
							ASN:       uint32(myAsn),
							VRF:       vrfName,
							Neighbors: neighbors,
						},
					},
					BFDProfiles: []frrk8sv1beta1.BFDProfile{
						bfdProfile,
					},
				},
			},
		}

		ginkgo.By("pairing with nodes")
		for _, c := range frrs {
			err := frrcontainer.PairWithNodes(cs, c, ipFamily, func(container *frrcontainer.FRR) {
				container.NeighborConfig.BFDEnabled = true
			})
			Expect(err).NotTo(HaveOccurred())
		}

		err := updater.Update(peersConfig.Secrets, config)
		Expect(err).NotTo(HaveOccurred())

		nodes, err := k8s.Nodes(cs)
		Expect(err).NotTo(HaveOccurred())

		for _, c := range frrs {
			ValidateFRRPeeredWithNodes(nodes, c, ipFamily)
		}

		pods, err := k8s.FRRK8sPods(cs)
		Expect(err).NotTo(HaveOccurred())

		ginkgo.By("validating")
		echoFor := func(e *bool) bool {
			return e != nil && *e
		}
		ValidatePodBFDUPMetrics(pods, frrs, promPod, ipFamily, echoFor(bfdProfile.EchoMode))

		ginkgo.By("disabling BFD in external FRR containers")
		for _, c := range frrs {
			err := frrcontainer.PairWithNodes(cs, c, ipFamily, func(container *frrcontainer.FRR) {
				container.NeighborConfig.BFDEnabled = false
			})
			Expect(err).NotTo(HaveOccurred())
		}

		ginkgo.By("validating session down metrics")
		ValidatePodBFDDownMetrics(pods, frrs, promPod, ipFamily, echoFor(bfdProfile.EchoMode))

	},
		ginkgo.Entry("IPV4 - default",
			frrk8sv1beta1.BFDProfile{
				Name: "simple",
			}, ipfamily.IPv4, infra.DefaultVRFName),
		ginkgo.Entry("IPV4 - full params",
			frrk8sv1beta1.BFDProfile{
				Name:             "full1",
				ReceiveInterval:  ptr.To[uint32](60),
				TransmitInterval: ptr.To[uint32](61),
				EchoInterval:     ptr.To[uint32](62),
				MinimumTTL:       ptr.To[uint32](254),
			}, ipfamily.IPv4, infra.DefaultVRFName),
		ginkgo.Entry("IPV4 - full params- VRF",
			frrk8sv1beta1.BFDProfile{
				Name:             "full1",
				ReceiveInterval:  ptr.To[uint32](60),
				TransmitInterval: ptr.To[uint32](61),
				EchoInterval:     ptr.To[uint32](62),
				MinimumTTL:       ptr.To[uint32](254),
			}, ipfamily.IPv4, infra.VRFName),
		ginkgo.Entry("IPV4 - echo mode enabled",
			frrk8sv1beta1.BFDProfile{
				Name:             "echo",
				ReceiveInterval:  ptr.To[uint32](80),
				TransmitInterval: ptr.To[uint32](81),
				EchoInterval:     ptr.To[uint32](82),
				EchoMode:         ptr.To(true),
				MinimumTTL:       ptr.To[uint32](254),
			}, ipfamily.IPv4, infra.DefaultVRFName),
		ginkgo.Entry("IPV6 - default",
			frrk8sv1beta1.BFDProfile{
				Name: "simple",
			}, ipfamily.IPv6, infra.DefaultVRFName),
		ginkgo.Entry("IPV6 - echo mode enabled",
			frrk8sv1beta1.BFDProfile{
				Name:             "echo",
				ReceiveInterval:  ptr.To[uint32](80),
				TransmitInterval: ptr.To[uint32](81),
				EchoInterval:     ptr.To[uint32](82),
				EchoMode:         ptr.To(true),
				MinimumTTL:       ptr.To[uint32](254),
			}, ipfamily.IPv6, infra.DefaultVRFName),
	)
})

type peerPrometheus struct {
	labelsForQueryBGP string
	labelsBGP         map[string]string
	labelsForQueryBFD string
	labelsBFD         map[string]string
	noEcho            bool
}

func labelsForPeers(peers []*frrcontainer.FRR, ipFamily ipfamily.Family) []peerPrometheus {
	res := make([]peerPrometheus, 0)
	for _, c := range peers {
		address := c.Ipv4
		if ipFamily == ipfamily.IPv6 {
			address = c.Ipv6
		}
		peerAddr := address + fmt.Sprintf(":%d", c.RouterConfig.BGPPort)

		// Note: we deliberately don't add the vrf label in case of the default vrf to validate that
		// it is still possible to list the metrics using only the peer label, which is what most users
		// who don't care about vrfs should do.
		labelsBGP := map[string]string{"peer": peerAddr}
		labelsForQueryBGP := fmt.Sprintf(`peer="%s"`, peerAddr)
		labelsBFD := map[string]string{"peer": address}
		labelsForQueryBFD := fmt.Sprintf(`peer="%s"`, address)
		noEcho := c.NeighborConfig.MultiHop

		if c.RouterConfig.VRF != "" {
			labelsBGP["vrf"] = c.RouterConfig.VRF
			labelsForQueryBGP = fmt.Sprintf(`peer="%s",vrf="%s"`, peerAddr, c.RouterConfig.VRF)
			labelsBFD["vrf"] = c.RouterConfig.VRF
			labelsForQueryBFD = fmt.Sprintf(`peer="%s",vrf="%s"`, address, c.RouterConfig.VRF)
		}
		res = append(res, peerPrometheus{
			labelsBGP:         labelsBGP,
			labelsForQueryBGP: labelsForQueryBGP,
			labelsBFD:         labelsBFD,
			labelsForQueryBFD: labelsForQueryBFD,
			noEcho:            noEcho,
		})
	}
	return res
}

// forPod returns the parsed metrics for the given pod, scraping them
// from the prometheus pod.
func forPod(promPod, target *corev1.Pod) ([]map[string]*dto.MetricFamily, error) {
	ports := make([]int, 0)
	allMetrics := make([]map[string]*dto.MetricFamily, 0)
	for _, c := range target.Spec.Containers {
		for _, p := range c.Ports {
			if p.Name == "metricshttps" || p.Name == "frrmetricshttps" {
				ports = append(ports, int(p.ContainerPort))
			}
		}
	}

	podExecutor := executor.ForPod(promPod.Namespace, promPod.Name, "prometheus")

	// We add a token header to the requests, without it kube-rbac-proxy returns Unauthorized.
	token, err := podExecutor.Exec("cat", "/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return nil, err
	}

	for _, p := range ports {
		metricsPath := path.Join(net.JoinHostPort(target.Status.PodIP, strconv.Itoa(p)), "metrics")
		metricsURL := fmt.Sprintf("https://%s", metricsPath)
		metrics, err := podExecutor.Exec("wget",
			"--no-check-certificate", "-qO-", metricsURL,
			"--header", fmt.Sprintf("Authorization: Bearer %s", token))
		if err != nil {
			return nil, errors.Join(err, fmt.Errorf("failed to scrape metrics for %s", target.Name))
		}
		res, err := metricsFromString(metrics)
		if err != nil {
			return nil, err
		}
		allMetrics = append(allMetrics, res)
	}

	return allMetrics, nil
}

func metricsFromString(metrics string) (map[string]*dto.MetricFamily, error) {
	var parser expfmt.TextParser
	mf, err := parser.TextToMetricFamilies(strings.NewReader(metrics))
	if err != nil {
		return nil, errors.Join(err, fmt.Errorf("failed to parse metrics %s", metrics))
	}
	return mf, nil
}

func ValidatePodBGPMetrics(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, ipfam ipfamily.Family) {
	selectors := labelsForPeers(frrContainers, ipfam)

	for _, pod := range frrPods {
		ginkgo.By(fmt.Sprintf("checking pod %s", pod.Name))
		Eventually(func() error {
			podMetrics, err := forPod(promPod, pod)
			if err != nil {
				return err
			}
			for _, selector := range selectors {
				err = metrics.ValidateGaugeValue(1, "frrk8s_bgp_session_up", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_session_up{%s} == 1`, selector.labelsForQueryBGP), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(0), "frrk8s_bgp_opens_sent", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}

				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_opens_sent{%s} >= 1`, selector.labelsForQueryBGP), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(0), "frrk8s_bgp_opens_received", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}

				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_opens_received{%s} >= 1`, selector.labelsForQueryBGP), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(1), "frrk8s_bgp_updates_total_received", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}

				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_updates_total_received{%s} >= 1`, selector.labelsForQueryBGP), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(0), "frrk8s_bgp_keepalives_sent", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}

				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_keepalives_sent{%s} >= 1`, selector.labelsForQueryBGP), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(0), "frrk8s_bgp_keepalives_received", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}

				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_keepalives_received{%s} >= 1`, selector.labelsForQueryBGP), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(0), "frrk8s_bgp_route_refresh_sent", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}

				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_route_refresh_sent{%s} >= 1`, selector.labelsForQueryBGP), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(0), "frrk8s_bgp_total_sent", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}

				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_total_sent{%s} >= 1`, selector.labelsForQueryBGP), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(0), "frrk8s_bgp_total_received", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}

				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_total_received{%s} >= 1`, selector.labelsForQueryBGP), metrics.There)
				if err != nil {
					return err
				}
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	}
}

func ValidatePodAnnounceMetrics(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, pfxsAmount int, ipfam ipfamily.Family) {
	selectors := labelsForPeers(frrContainers, ipfam)

	for _, pod := range frrPods {
		ginkgo.By(fmt.Sprintf("checking pod %s", pod.Name))
		Eventually(func() error {
			podMetrics, err := forPod(promPod, pod)
			if err != nil {
				return err
			}
			for _, selector := range selectors {
				err = metrics.ValidateGaugeValue(pfxsAmount, "frrk8s_bgp_announced_prefixes_total", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_announced_prefixes_total{%s} == %d`, selector.labelsForQueryBGP, pfxsAmount), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(1), "frrk8s_bgp_updates_total", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_updates_total{%s} >= 1`, selector.labelsForQueryBGP), metrics.There)
				if err != nil {
					return err
				}
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	}
}

func ValidatePodReceiveMetrics(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, pfxsAmount int, ipfam ipfamily.Family) {
	selectors := labelsForPeers(frrContainers, ipfam)

	for _, pod := range frrPods {
		ginkgo.By(fmt.Sprintf("checking pod %s", pod.Name))
		Eventually(func() error {
			podMetrics, err := forPod(promPod, pod)
			if err != nil {
				return err
			}
			for _, selector := range selectors {
				err = metrics.ValidateGaugeValue(pfxsAmount, "frrk8s_bgp_received_prefixes_total", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_received_prefixes_total{%s} == %d`, selector.labelsForQueryBGP, pfxsAmount), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(1), "frrk8s_bgp_updates_total", selector.labelsBGP, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bgp_updates_total{%s} >= 1`, selector.labelsForQueryBGP), metrics.There)
				if err != nil {
					return err
				}
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	}
}

func ValidatePodK8SClientUpMetrics(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, ipfam ipfamily.Family) {
	for _, pod := range frrPods {
		ginkgo.By(fmt.Sprintf("checking pod %s", pod.Name))
		Eventually(func() error {
			podMetrics, err := forPod(promPod, pod)
			if err != nil {
				return err
			}

			err = metrics.ValidateGaugeValue(0, "frrk8s_k8s_client_config_stale_bool", map[string]string{}, podMetrics)
			if err != nil {
				return err
			}
			err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_k8s_client_config_stale_bool{pod="%s"} == 0`, pod.Name), metrics.There)
			if err != nil {
				return err
			}

			err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(1), "frrk8s_k8s_client_updates_total", map[string]string{}, podMetrics)
			if err != nil {
				return err
			}
			err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_k8s_client_updates_total{pod="%s"} > 0`, pod.Name), metrics.There)
			if err != nil {
				return err
			}

			err = metrics.ValidateGaugeValue(1, "frrk8s_k8s_client_config_loaded_bool", map[string]string{}, podMetrics)
			if err != nil {
				return err
			}
			err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_k8s_client_config_loaded_bool{pod="%s"} == 1`, pod.Name), metrics.There)
			if err != nil {
				return err
			}

			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	}
}

func ValidatePodK8SClientErrorMetrics(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, ipfam ipfamily.Family) {
	for _, pod := range frrPods {
		ginkgo.By(fmt.Sprintf("checking pod %s", pod.Name))
		Eventually(func() error {
			podMetrics, err := forPod(promPod, pod)
			if err != nil {
				return err
			}

			err = metrics.ValidateGaugeValue(1, "frrk8s_k8s_client_config_stale_bool", map[string]string{}, podMetrics)
			if err != nil {
				return err
			}
			err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_k8s_client_config_stale_bool{pod="%s"} == 1`, pod.Name), metrics.There)
			if err != nil {
				return err
			}

			err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(1), "frrk8s_k8s_client_updates_total", map[string]string{}, podMetrics)
			if err != nil {
				return err
			}
			err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_k8s_client_updates_total{pod="%s"} > 0`, pod.Name), metrics.There)
			if err != nil {
				return err
			}

			err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(1), "frrk8s_k8s_client_update_errors_total", map[string]string{}, podMetrics)
			if err != nil {
				return err
			}
			err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_k8s_client_update_errors_total{pod="%s"} > 0`, pod.Name), metrics.There)
			if err != nil {
				return err
			}

			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	}
}

func ValidatePodBFDUPMetrics(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, ipfam ipfamily.Family, withEcho bool) {
	selectors := labelsForPeers(frrContainers, ipfam)

	for _, pod := range frrPods {
		ginkgo.By(fmt.Sprintf("checking pod %s", pod.Name))
		Eventually(func() error {
			podMetrics, err := forPod(promPod, pod)
			if err != nil {
				return err
			}
			for _, selector := range selectors {
				err = metrics.ValidateGaugeValue(1, "frrk8s_bfd_session_up", selector.labelsBFD, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bfd_session_up{%s} == 1`, selector.labelsForQueryBFD), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(1), "frrk8s_bfd_control_packet_input", selector.labelsBFD, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bfd_control_packet_input{%s} >= 1`, selector.labelsForQueryBFD), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(1), "frrk8s_bfd_control_packet_output", selector.labelsBFD, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bfd_control_packet_output{%s} >= 1`, selector.labelsForQueryBFD), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateGaugeValueCompare(metrics.GreaterOrEqualThan(0), "frrk8s_bfd_session_down_events", selector.labelsBFD, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bfd_session_down_events{%s} >= 0`, selector.labelsForQueryBFD), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(1), "frrk8s_bfd_session_up_events", selector.labelsBFD, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bfd_session_up_events{%s} >= 1`, selector.labelsForQueryBFD), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(1), "frrk8s_bfd_zebra_notifications", selector.labelsBFD, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bfd_zebra_notifications{%s} >= 1`, selector.labelsForQueryBFD), metrics.There)
				if err != nil {
					return err
				}

				if withEcho {
					echoVal := 1
					if selector.noEcho {
						echoVal = 0
					}
					err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(echoVal), "frrk8s_bfd_echo_packet_input", selector.labelsBFD, podMetrics)
					if err != nil {
						return err
					}
					err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bfd_echo_packet_input{%s} >= %d`, selector.labelsForQueryBFD, echoVal), metrics.There)
					if err != nil {
						return err
					}

					err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(echoVal), "frrk8s_bfd_echo_packet_output", selector.labelsBFD, podMetrics)
					if err != nil {
						return err
					}
					err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bfd_echo_packet_output{%s} >= %d`, selector.labelsForQueryBFD, echoVal), metrics.There)
					if err != nil {
						return err
					}
				}
			}
			return nil
		}, time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
	}
}

func ValidatePodBFDDownMetrics(frrPods []*corev1.Pod, frrContainers []*frrcontainer.FRR, promPod *corev1.Pod, ipfam ipfamily.Family, withEcho bool) {
	selectors := labelsForPeers(frrContainers, ipfam)

	for _, pod := range frrPods {
		ginkgo.By(fmt.Sprintf("checking pod %s", pod.Name))
		Eventually(func() error {
			podMetrics, err := forPod(promPod, pod)
			if err != nil {
				return err
			}

			for _, selector := range selectors {
				err = metrics.ValidateGaugeValue(0, "frrk8s_bfd_session_up", selector.labelsBFD, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bfd_session_up{%s} == 0`, selector.labelsForQueryBFD), metrics.There)
				if err != nil {
					return err
				}

				err = metrics.ValidateCounterValue(metrics.GreaterOrEqualThan(1), "frrk8s_bfd_session_down_events", selector.labelsBFD, podMetrics)
				if err != nil {
					return err
				}
				err = metrics.ValidateOnPrometheus(promPod, fmt.Sprintf(`frrk8s_bfd_session_down_events{%s} >= 1`, selector.labelsForQueryBFD), metrics.There)
				if err != nil {
					return err
				}
			}
			return nil
		}, time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
	}
}
