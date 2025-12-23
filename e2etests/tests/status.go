// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	frrk8sv1beta2 "github.com/metallb/frr-k8s/api/v1beta2"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	. "github.com/onsi/gomega"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientset "k8s.io/client-go/kubernetes"
)

const reloadSuccess = "success"

var _ = ginkgo.Describe("Exposing FRR status", func() {
	var cs clientset.Interface

	defer ginkgo.GinkgoRecover()
	updater, err := config.NewUpdater()
	Expect(err).NotTo(HaveOccurred())
	reporter := dump.NewK8sReporter(k8s.FRRK8sNamespace)

	myScheme := runtime.NewScheme()
	err = frrk8sv1beta1.AddToScheme(myScheme)
	Expect(err).NotTo(HaveOccurred())
	err = v1.AddToScheme(myScheme)
	Expect(err).NotTo(HaveOccurred())
	clientconfig := k8sclient.RestConfig()
	cl, err := client.New(clientconfig, client.Options{
		Scheme: myScheme,
	})
	Expect(err).NotTo(HaveOccurred())

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
	})

	ginkgo.Context("Exposing the frr status", func() {
		ginkgo.It("Works with valid config", func() {
			frrconfig := frrk8sv1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta2.FRRConfigurationSpec{
					BGP: frrk8sv1beta2.BGPConfig{
						Routers: []frrk8sv1beta2.Router{
							{
								ASN: 64515,
								VRF: "",
							},
						},
					},
				},
			}

			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())
			ginkgo.By("Creating a configuration with no neighbors")
			err = updater.Update([]corev1.Secret{}, frrconfig)
			Expect(err).NotTo(HaveOccurred())

			for _, node := range nodes {
				Eventually(func() error {
					return nodeMatchesStatus(cl, node.Name, func(status frrk8sv1beta1.FRRNodeState) error {
						if status.Status.LastReloadResult != reloadSuccess {
							return fmt.Errorf("LastReloadResult is not success for node %s", node.Name)
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
			frrconfig.Spec.BGP.Routers[0].Neighbors = []frrk8sv1beta2.Neighbor{
				{
					ASN:     123,
					Address: "192.168.5.1",
					PasswordSecret: frrk8sv1beta2.SecretReference{
						Name:      "neighsecret",
						Namespace: k8s.FRRK8sNamespace,
					},
				},
			}

			ginkgo.By("Adding neighbors")
			err = updater.Update([]corev1.Secret{s}, frrconfig)
			Expect(err).NotTo(HaveOccurred())

			for _, node := range nodes {
				Eventually(func() error {
					return nodeMatchesStatus(cl, node.Name, func(status frrk8sv1beta1.FRRNodeState) error {
						if status.Status.LastReloadResult != reloadSuccess {
							return fmt.Errorf("LastReloadResult is not success")
						}

						if err := stringMatches(status.Status.RunningConfig, Contains,
							"router bgp 64515",
							"neighbor 192.168.5.1 activate",
							"password <retracted>",
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

			frrconfig.Spec.BGP.Routers[0].Neighbors = []frrk8sv1beta2.Neighbor{}
			ginkgo.By("Removing neighbors")
			err = updater.Update([]corev1.Secret{}, frrconfig)
			Expect(err).NotTo(HaveOccurred())

			for _, node := range nodes {
				Eventually(func() error {
					return nodeMatchesStatus(cl, node.Name, func(status frrk8sv1beta1.FRRNodeState) error {
						if status.Status.LastReloadResult != reloadSuccess {
							return fmt.Errorf("LastReloadResult is not success")
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

			ginkgo.By("Applying an invalid config")
			frrconfig.Spec.BGP = frrk8sv1beta2.BGPConfig{}
			frrconfig.Spec.Raw.Config = "this is a non valid configuration"
			err = updater.Update([]corev1.Secret{}, frrconfig)
			Expect(err).NotTo(HaveOccurred())

			for _, node := range nodes {
				Eventually(func() error {
					return nodeMatchesStatus(cl, node.Name, func(status frrk8sv1beta1.FRRNodeState) error {
						if !strings.Contains(status.Status.LastReloadResult, "ERROR") {
							return fmt.Errorf("Last reload does not contain error")
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
	})

	ginkgo.Context("Exposing the configuration conversion status", func() {
		ginkgo.It("Works with valid config", func() {
			validFRRConfig := frrk8sv1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta2.FRRConfigurationSpec{
					BGP: frrk8sv1beta2.BGPConfig{
						Routers: []frrk8sv1beta2.Router{
							{
								ASN: 64515,
								VRF: "",
							},
						},
					},
				},
			}

			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())
			ginkgo.By("Creating a valid configuration")
			err = updater.Update([]corev1.Secret{}, validFRRConfig)
			Expect(err).NotTo(HaveOccurred())

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

			invalidConfig := frrk8sv1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta2.FRRConfigurationSpec{
					BGP: frrk8sv1beta2.BGPConfig{
						Routers: []frrk8sv1beta2.Router{
							{
								ASN: infra.FRRK8sASN,
								Neighbors: []frrk8sv1beta2.Neighbor{
									{
										ASN:     1234,
										Address: "192.168.1.1",
										PasswordSecret: frrk8sv1beta2.SecretReference{
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
			Expect(err).NotTo(HaveOccurred())

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

	ginkgo.Context("Writing FRR Config in a different namespace", func() {
		const testNamespace = "dontread"

		ginkgo.AfterEach(func() {
			ginkgo.By("Deleting the test namespace")
			err := cs.CoreV1().Namespaces().Delete(context.Background(), testNamespace, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				_, err := cs.CoreV1().Namespaces().Get(context.Background(), testNamespace, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, time.Minute, time.Second).Should(BeTrue())
		})

		ginkgo.BeforeEach(func() {
			ginkgo.By("Creating the test namespace")
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
			_, err := cs.CoreV1().Namespaces().Create(context.Background(), &ns, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() error {
				_, err := cs.CoreV1().Namespaces().Get(context.Background(), testNamespace, metav1.GetOptions{})
				return err
			}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
		})

		ginkgo.It("should not be processed and reflected in the frr status", func() {
			frrconfig := frrk8sv1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: testNamespace,
				},
				Spec: frrk8sv1beta2.FRRConfigurationSpec{
					BGP: frrk8sv1beta2.BGPConfig{
						Routers: []frrk8sv1beta2.Router{
							{
								ASN: 74515,
							},
						},
					},
				},
			}

			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Creating a configuration with no neighbors")
			err = updater.Update([]corev1.Secret{}, frrconfig)
			Expect(err).NotTo(HaveOccurred())

			node := nodes[0]
			Consistently(func() error {
				return nodeMatchesStatus(cl, node.Name, func(status frrk8sv1beta1.FRRNodeState) error {
					if err := stringMatches(status.Status.RunningConfig, DoesNotContain,
						"router bgp 74515",
					); err != nil {
						return err
					}
					return nil
				})
			}, 30*time.Second, time.Second).ShouldNot(HaveOccurred())
		})
	})

	ginkgo.Context("Node state cleaner functionality", func() {
		ginkgo.It("should delete FRRNodeState when node has no FRR-K8s pods", func() {
			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Verifying all node states exist")
			statuses := frrk8sv1beta1.FRRNodeStateList{}
			err = cl.List(context.Background(), &statuses)
			Expect(err).NotTo(HaveOccurred())
			initialStatusCount := len(statuses.Items)
			Expect(initialStatusCount).To(Equal(len(nodes)))

			ginkgo.By("Creating a FRRNodeState for a node without FRR-K8s pods")
			orphanedNodeName := "orphaned-node-without-frr-pods"
			orphanedNodeState := &frrk8sv1beta1.FRRNodeState{
				ObjectMeta: metav1.ObjectMeta{
					Name: orphanedNodeName,
				},
			}
			err = cl.Create(context.Background(), orphanedNodeState)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Waiting for orphaned FRRNodeState to be deleted")
			Eventually(func() error {
				statuses := frrk8sv1beta1.FRRNodeStateList{}
				err := cl.List(context.Background(), &statuses)
				if err != nil {
					return err
				}

				for _, status := range statuses.Items {
					if status.Name == orphanedNodeName {
						return fmt.Errorf("FRRNodeState for node %s without FRR-K8s pods still exists", orphanedNodeName)
					}
				}
				return nil
			}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
		})

		ginkgo.It("should remove and restore FRRNodeState when daemonset nodeSelector changes", func() {
			nodes, err := k8s.Nodes(cs)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(nodes)).To(BeNumerically(">=", 2), "Need at least 2 nodes for this test")

			excludedNode := nodes[0]
			remainingNodes := nodes[1:]

			ginkgo.By("Patching daemonset to exclude the first node")
			daemonSet, err := k8s.FRRK8sDaemonSet(cs)
			Expect(err).NotTo(HaveOccurred())

			excludedNode.Labels["test-exclude"] = "true"
			_, err = cs.CoreV1().Nodes().Update(context.Background(), &excludedNode, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())

			daemonSet.Spec.Template.Spec.Affinity = &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "test-exclude",
										Operator: corev1.NodeSelectorOpNotIn,
										Values:   []string{"true"},
									},
								},
							},
						},
					},
				},
			}

			_, err = cs.AppsV1().DaemonSets(k8s.FRRK8sNamespace).Update(context.Background(), daemonSet, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Waiting for FRRNodeState to be deleted for excluded node")
			Eventually(func() error {
				return validateStateForNode(cl, excludedNode.Name, DoesNotExist)
			}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Verifying FRRNodeStates still exist for remaining nodes")
			Eventually(func() error {
				for _, node := range remainingNodes {
					if err := validateStateForNode(cl, node.Name, Exists); err != nil {
						return err
					}
				}
				return nil
			}, 30*time.Second, time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Reverting daemonset configuration back to original")
			daemonSet, err = k8s.FRRK8sDaemonSet(cs)
			Expect(err).NotTo(HaveOccurred())

			daemonSet.Spec.Template.Spec.Affinity = nil

			_, err = cs.AppsV1().DaemonSets(k8s.FRRK8sNamespace).Update(context.Background(), daemonSet, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Waiting for FRRNodeState to be restored for the previously excluded node")
			Eventually(func() error {
				return validateStateForNode(cl, excludedNode.Name, Exists)
			}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

			ginkgo.By("Cleaning up test label from node")
			excludedNodePtr, err := cs.CoreV1().Nodes().Get(context.Background(), excludedNode.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			delete(excludedNodePtr.Labels, "test-exclude")
			_, err = cs.CoreV1().Nodes().Update(context.Background(), excludedNodePtr, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Waiting that all pods of the daemonset running")
			_, err = k8s.FRRK8sDaemonSet(cs)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

func validateStateForNode(cl client.Client, nodeName string, shouldExist bool) error {
	statuses := frrk8sv1beta1.FRRNodeStateList{}
	err := cl.List(context.Background(), &statuses)
	if err != nil {
		return err
	}

	for _, status := range statuses.Items {
		if status.Name == nodeName {
			if !shouldExist {
				return fmt.Errorf("FRRNodeState for node %s exists but should not", nodeName)
			}
			return nil
		}
	}

	if shouldExist {
		return fmt.Errorf("FRRNodeState for node %s not found but should exist", nodeName)
	}
	return nil
}

func nodeMatchesStatus(cl client.Client, nodeName string, validate func(status frrk8sv1beta1.FRRNodeState) error) error {
	statuses := frrk8sv1beta1.FRRNodeStateList{}
	err := cl.List(context.Background(), &statuses)
	Expect(err).NotTo(HaveOccurred())
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
	Exists         = true
	DoesNotExist   = false
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
