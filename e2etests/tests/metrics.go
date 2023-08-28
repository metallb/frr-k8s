// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"
	"time"

	frrk8sv1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	"go.universe.tf/e2etest/pkg/metrics"
	"go.universe.tf/metallb/e2etest/pkg/executor"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	admissionapi "k8s.io/pod-security-admission/api"
)

var (
	PrometheusNamespace string
)

var _ = ginkgo.Describe("Metrics", func() {
	var cs clientset.Interface
	var f *framework.Framework
	var promPod *corev1.Pod

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

		promPod, err = metrics.PrometheusPod(cs, PrometheusNamespace)
		framework.ExpectNoError(err)
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
			err := frrcontainer.PairWithNodes(cs, c, p.ipFamily, func(frr *frrcontainer.FRR) {
				frr.NeighborConfig.ToAdvertiseV4 = p.toAdvertiseV4
				frr.NeighborConfig.ToAdvertiseV6 = p.toAdvertiseV6
			})
			framework.ExpectNoError(err)
		}

		err := updater.Update(peersConfig.Secrets, config)
		framework.ExpectNoError(err)

		nodes, err := k8s.Nodes(cs)
		framework.ExpectNoError(err)

		for _, c := range frrs {
			ValidateFRRPeeredWithNodes(nodes, c, p.ipFamily)
		}

		pods, err := k8s.FRRK8sPods(cs)
		framework.ExpectNoError(err)

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
			peersConfig.PeersV4[i].Neigh.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{
				{
					LocalPref: 200,
					Prefixes:  []string{"1.2.3.0/24"},
				},
			}
		}
		neighbors := config.NeighborsFromPeers(peersConfig.PeersV4, peersConfig.PeersV6)

		config := frrk8sv1beta1.FRRConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
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
		framework.ExpectNoError(err)

		pods, err := k8s.FRRK8sPods(cs)
		framework.ExpectNoError(err)

		ginkgo.By("validating")
		ValidatePodK8SClientErrorMetrics(pods, frrs, promPod, ipfamily.IPv4)
	})
})

type peerPrometheus struct {
	labelsForQueryBGP string
	labelsBGP         map[string]string
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

		if c.RouterConfig.VRF != "" {
			labelsBGP["vrf"] = c.RouterConfig.VRF
			labelsForQueryBGP = fmt.Sprintf(`peer="%s",vrf="%s"`, peerAddr, c.RouterConfig.VRF)
		}
		res = append(res, peerPrometheus{
			labelsBGP:         labelsBGP,
			labelsForQueryBGP: labelsForQueryBGP,
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
			if p.Name == "monitoring" {
				ports = append(ports, int(p.ContainerPort))
			}
		}
	}

	podExecutor := executor.ForPod(promPod.Namespace, promPod.Name, "prometheus")
	for _, p := range ports {
		metricsURL := path.Join(net.JoinHostPort(target.Status.PodIP, strconv.Itoa(p)), "metrics")
		metrics, err := podExecutor.Exec("wget", "-qO-", metricsURL)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to scrape metrics for %s", target.Name)
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
		return nil, errors.Wrapf(err, "failed to parse metrics %s", metrics)
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
