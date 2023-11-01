// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frrk8sv1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	. "github.com/onsi/gomega"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	admissionapi "k8s.io/pod-security-admission/api"
)

const (
	reloadSuccess       = "success"
	healthResultSuccess = "success"
	healthResultFailed  = "failed"
)

var _ = ginkgo.Describe("Exposing FRR status", func() {
	var cs clientset.Interface
	var f *framework.Framework

	defer ginkgo.GinkgoRecover()
	clientconfig, err := framework.LoadConfig()
	framework.ExpectNoError(err)
	updater, err := config.NewUpdater(clientconfig)
	framework.ExpectNoError(err)
	reporter := dump.NewK8sReporter(framework.TestContext.KubeConfig, k8s.FRRK8sNamespace)

	myScheme := runtime.NewScheme()
	err = frrk8sv1beta1.AddToScheme(myScheme)
	framework.ExpectNoError(err)
	err = v1.AddToScheme(myScheme)
	framework.ExpectNoError(err)
	err = corev1.AddToScheme(myScheme)
	framework.ExpectNoError(err)

	cl, err := client.New(clientconfig, client.Options{
		Scheme: myScheme,
	})
	framework.ExpectNoError(err)

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

	ginkgo.Context("Exposing the frr status", func() {
		ginkgo.It("Works with valid config", func() {
			frrconfig := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN: 64515,
								VRF: "",
							},
						},
					},
				},
			}

			nodes, err := k8s.Nodes(cs)
			framework.ExpectNoError(err)
			ginkgo.By("Creating a configuration with no neighbors")
			err = updater.Update([]corev1.Secret{}, frrconfig)
			framework.ExpectNoError(err)

			for _, node := range nodes {
				Eventually(func() error {
					return nodeMatchesStatus(cl, node.Name, func(status frrk8sv1beta1.FRRNodeState) error {
						if status.Status.LastReloadResult != reloadSuccess {
							return fmt.Errorf("LastReloadResult is not success for node %s", node.Name)
						}
						if err := stringMatches(status.Status.DesiredConfig, Contains,
							"router bgp 64515",
						); err != nil {
							return err
						}

						if err := stringMatches(status.Status.RunningConfig, Contains,
							"router bgp 64515",
						); err != nil {
							return err
						}
						return nil
					})
				}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
			}

			s := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neighsecret",
					Namespace: k8s.FRRK8sNamespace,
				},
				Type: corev1.SecretTypeBasicAuth,
				Data: map[string][]byte{
					"password": []byte("supersecret"),
				},
			}
			frrconfig.Spec.BGP.Routers[0].Neighbors = []frrk8sv1beta1.Neighbor{
				{
					ASN:     123,
					Address: "192.168.5.1",
					PasswordSecret: corev1.SecretReference{
						Name:      "neighsecret",
						Namespace: k8s.FRRK8sNamespace,
					},
				},
			}

			ginkgo.By("Adding neighbors")
			err = updater.Update([]corev1.Secret{s}, frrconfig)
			framework.ExpectNoError(err)

			for _, node := range nodes {
				Eventually(func() error {
					return nodeMatchesStatus(cl, node.Name, func(status frrk8sv1beta1.FRRNodeState) error {
						if status.Status.LastReloadResult != reloadSuccess {
							return fmt.Errorf("LastReloadResult is not success")
						}
						if err := stringMatches(status.Status.DesiredConfig, Contains,
							"router bgp 64515",
							"neighbor 192.168.5.1 activate",
							"password <retracted>",
						); err != nil {
							return err
						}

						if err := stringMatches(status.Status.RunningConfig, Contains,
							"router bgp 64515",
							"neighbor 192.168.5.1 activate",
							"password <retracted>",
						); err != nil {
							return err
						}

						if err := stringMatches(status.Status.DesiredConfig, DoesNotContain,
							"supersecret",
						); err != nil {
							return err
						}

						if err := stringMatches(status.Status.RunningConfig, DoesNotContain,
							"supersecret",
						); err != nil {
							return err
						}

						return nil
					})
				}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
			}

			frrconfig.Spec.BGP.Routers[0].Neighbors = []frrk8sv1beta1.Neighbor{}
			ginkgo.By("Removing neighbors")
			err = updater.Update([]corev1.Secret{}, frrconfig)
			framework.ExpectNoError(err)

			for _, node := range nodes {
				Eventually(func() error {
					return nodeMatchesStatus(cl, node.Name, func(status frrk8sv1beta1.FRRNodeState) error {
						if status.Status.LastReloadResult != reloadSuccess {
							return fmt.Errorf("LastReloadResult is not success")
						}
						if err := stringMatches(status.Status.DesiredConfig, Contains,
							"router bgp 64515",
						); err != nil {
							return err
						}
						if err := stringMatches(status.Status.DesiredConfig, DoesNotContain,
							"neighbor 192.168.5.1 activate",
						); err != nil {
							return err
						}

						if err := stringMatches(status.Status.RunningConfig, Contains,
							"router bgp 64515",
						); err != nil {
							return err
						}
						if err := stringMatches(status.Status.DesiredConfig, DoesNotContain,
							"neighbor 192.168.5.1 activate",
						); err != nil {
							return err
						}
						return nil
					})
				}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
			}

			ginkgo.By("Applying an invalid config")
			frrconfig.Spec.BGP = frrk8sv1beta1.BGPConfig{}
			frrconfig.Spec.Raw.Config = "this is a non valid configuration"
			err = updater.Update([]corev1.Secret{}, frrconfig)
			framework.ExpectNoError(err)

			for _, node := range nodes {
				Eventually(func() error {
					return nodeMatchesStatus(cl, node.Name, func(status frrk8sv1beta1.FRRNodeState) error {
						if !strings.Contains(status.Status.LastReloadResult, "ERROR") {
							return fmt.Errorf("Last reload does not contain error")
						}
						if err := stringMatches(status.Status.DesiredConfig, Contains,
							"this is a non valid configuration",
						); err != nil {
							return err
						}
						if err := stringMatches(status.Status.DesiredConfig, DoesNotContain,
							"router bgp 64515",
						); err != nil {
							return err
						}
						if err := stringMatches(status.Status.RunningConfig, Contains,
							"router bgp 64515",
						); err != nil {
							return err
						}
						return nil
					})
				}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
			}
		})

		ginkgo.It("Works with a config that breaks worker nodes apiserver connectivity", func() {
			// The nodes that we want to use for this test are non-control planes
			// that do not host a webhook endpoint to minimize disruption to the cluster.
			ginkgo.By("Getting the non control plane nodes")
			nonCPNodes := corev1.NodeList{}
			nonCPReq, err := labels.NewRequirement("node-role.kubernetes.io/control-plane", selection.DoesNotExist, []string{})
			framework.ExpectNoError(err)
			nonCPSelector := labels.NewSelector().Add(*nonCPReq)
			err = cl.List(context.Background(), &nonCPNodes, &client.ListOptions{LabelSelector: nonCPSelector})
			framework.ExpectNoError(err)

			ginkgo.By("Getting the nodes that are hosting the webhooks")
			webhookPods := corev1.PodList{}
			webhookLabels := map[string]string{"app.kubernetes.io/component": "frr-k8s-webhook-server"}
			err = cl.List(context.Background(), &webhookPods, &client.ListOptions{
				Namespace:     k8s.FRRK8sNamespace,
				LabelSelector: labels.SelectorFromSet(webhookLabels)})
			framework.ExpectNoError(err)

			webhookNodes := map[string]bool{}
			for _, p := range webhookPods.Items {
				webhookNodes[p.Spec.NodeName] = true
			}

			ginkgo.By("Setting the test nodes")
			testNodes := []string{}
			for _, n := range nonCPNodes.Items {
				if webhookNodes[n.Name] {
					continue
				}
				testNodes = append(testNodes, n.Name)
			}
			Expect(testNodes).ToNot(BeEmpty())

			ginkgo.By("Verifying the last reset result for the test nodes success")
			Eventually(func() error {
				for _, n := range testNodes {
					state := frrk8sv1beta1.FRRNodeState{}
					err = cl.Get(context.Background(), types.NamespacedName{Name: n}, &state)
					if err != nil {
						return err
					}

					if state.Status.LastHealthcheckResult != healthResultSuccess {
						return fmt.Errorf("last reset result %s for node %s is not success", state.Status.LastHealthcheckResult, n)
					}
				}
				return nil
			}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Creating a config that blocks access to the apiserver on the test nodes")
			config := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					NodeSelector: metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: metav1.LabelSelectorOpIn,
								Values:   testNodes,
							},
						},
					},
					Raw: frrk8sv1beta1.RawConfig{
						Priority: 5,
						Config:   rawConfigToBlockAPIServer(cl),
					},
				},
			}
			err = updater.Update([]corev1.Secret{}, config)
			framework.ExpectNoError(err)

			ginkgo.By("Verifying the last reset result for the test nodes is failed")
			Eventually(func() error {
				for _, n := range testNodes {
					state := frrk8sv1beta1.FRRNodeState{}
					err = cl.Get(context.Background(), types.NamespacedName{Name: n}, &state)
					if err != nil {
						return err
					}

					if state.Status.LastHealthcheckResult != healthResultFailed {
						return fmt.Errorf("last reset result %s for node %s is not failed", state.Status.LastHealthcheckResult, n)
					}
				}
				return nil
			}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())

			Consistently(func() error {
				for _, n := range testNodes {
					state := frrk8sv1beta1.FRRNodeState{}
					err = cl.Get(context.Background(), types.NamespacedName{Name: n}, &state)
					if err != nil {
						return err
					}

					if state.Status.LastHealthcheckResult != healthResultFailed {
						return fmt.Errorf("last reset result %s for node %s is not failed", state.Status.LastHealthcheckResult, n)
					}
				}
				return nil
			}, 30*time.Second, 2*time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Deleting the bad config the result should eventually be success")
			err = cl.Delete(context.Background(), &config)
			framework.ExpectNoError(err)

			Eventually(func() error {
				for _, n := range testNodes {
					state := frrk8sv1beta1.FRRNodeState{}
					err = cl.Get(context.Background(), types.NamespacedName{Name: n}, &state)
					if err != nil {
						return err
					}

					if state.Status.LastHealthcheckResult != healthResultSuccess {
						return fmt.Errorf("last reset result %s for node %s is not success", state.Status.LastHealthcheckResult, n)
					}
				}
				return nil
			}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
		})
	})

	ginkgo.Context("Exposing the configuration conversion status", func() {
		ginkgo.It("Works with valid config", func() {
			validFRRConfig := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN: 64515,
								VRF: "",
							},
						},
					},
				},
			}

			nodes, err := k8s.Nodes(cs)
			framework.ExpectNoError(err)
			ginkgo.By("Creating a valid configuration")
			err = updater.Update([]corev1.Secret{}, validFRRConfig)
			framework.ExpectNoError(err)

			for _, node := range nodes {
				Eventually(func() error {
					return nodeMatchesStatus(cl, node.Name, func(status frrk8sv1beta1.FRRNodeState) error {
						if status.Status.LastConversionResult != reloadSuccess {
							return fmt.Errorf("LastConversionResult is not success for node %s", node.Name)
						}
						return nil
					})
				}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
			}

			ginkgo.By("Applying an invalid config")

			invalidConfig := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN: infra.FRRK8sASN,
								Neighbors: []frrk8sv1beta1.Neighbor{
									{
										ASN:     1234,
										Address: "192.168.1.1",
										PasswordSecret: corev1.SecretReference{
											Name:      "nonexisting",
											Namespace: k8s.FRRK8sNamespace,
										},
									},
								},
							},
						},
					},
				},
			}

			err = updater.Update([]corev1.Secret{}, invalidConfig)
			framework.ExpectNoError(err)

			for _, node := range nodes {
				Eventually(func() error {
					return nodeMatchesStatus(cl, node.Name, func(status frrk8sv1beta1.FRRNodeState) error {
						if !strings.Contains(status.Status.LastConversionResult, "secret nonexisting not found") {
							return fmt.Errorf("LastConversionResult does not contain secret nonexisting not found for node %s", node.Name)
						}
						return nil
					})
				}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
			}
		})
	})
})

func nodeMatchesStatus(cl client.Client, nodeName string, validate func(status frrk8sv1beta1.FRRNodeState) error) error {
	statuses := frrk8sv1beta1.FRRNodeStateList{}
	err := cl.List(context.Background(), &statuses)
	framework.ExpectNoError(err)
	for _, status := range statuses.Items {
		if status.Name == nodeName {
			return validate(status)
		}
	}
	return fmt.Errorf("Status not found for node %s", nodeName)
}

const (
	Contains       = true
	DoesNotContain = false
)

func stringMatches(toCheck string, mustContain bool, values ...string) error {
	for _, value := range values {
		modifier := ""
		if mustContain {
			modifier = "not"
		}
		if strings.Contains(toCheck, value) != mustContain {
			return fmt.Errorf("String %s does %s contain %s", toCheck, modifier, value)
		}
	}
	return nil

}

func rawConfigToBlockAPIServer(cl client.Client) string {
	raw := ""

	controlPlaneNodes := corev1.NodeList{}
	err := cl.List(context.Background(), &controlPlaneNodes, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{"node-role.kubernetes.io/control-plane": ""}),
	})
	framework.ExpectNoError(err)
	Expect(controlPlaneNodes.Items).ToNot(BeEmpty())

	for _, n := range controlPlaneNodes.Items {
		for _, addr := range n.Status.Addresses {
			if addr.Type != corev1.NodeInternalIP {
				continue
			}
			nodeip := net.ParseIP(addr.Address)
			Expect(nodeip).ToNot(BeNil())
			ip := "ip"
			length := 32
			if nodeip.To4() == nil {
				ip = "ipv6"
				length = 128
			}
			raw += fmt.Sprintf("%s route %s/%d blackhole\n", ip, addr.Address, length)
		}
	}

	return raw
}
