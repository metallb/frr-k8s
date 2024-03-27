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
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	. "github.com/onsi/gomega"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

			frrconfig.Spec.BGP.Routers[0].Neighbors = []frrk8sv1beta1.Neighbor{}
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
			frrconfig.Spec.BGP = frrk8sv1beta1.BGPConfig{}
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
})

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
