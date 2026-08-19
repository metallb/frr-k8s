/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/metallb/frr-k8s/internal/ipfamily"
	"github.com/metallb/frr-k8s/internal/logging"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	fakeFRRConfigHandler fakeFRR
)

type fakeFRR struct {
	lastConfig *frr.Config
	mustError  bool
}

func (f *fakeFRR) ApplyConfig(config *frr.Config) error {
	f.lastConfig = config
	if f.mustError {
		return fmt.Errorf("error")
	}
	return nil
}

var reloadCalled bool

func fakeReloadStatus() {
	reloadCalled = true
}

var _ = Describe("Frrk8s controller", func() {
	// This is our expected log level because in common_test.go, we set	defaultLogLevel := logging.LevelDebug.
	logLevel := logging.LevelDebug

	Context("level-driven event mapping", func() {
		It("maps FRRConfiguration and Secret events to the same reconciliation key", func() {
			configuration := &v1beta1.FRRConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "configuration", Namespace: testNamespace}}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "password", Namespace: testNamespace}}

			configurationRequests := reconcileRequestForFRRConfiguration(context.Background(), configuration)
			secretRequests := reconcileRequestForFRRConfiguration(context.Background(), secret)
			expected := types.NamespacedName{Name: frrConfigurationReconcileName, Namespace: testNamespace}

			Expect(configurationRequests).To(HaveLen(1))
			Expect(secretRequests).To(HaveLen(1))
			Expect(configurationRequests[0].NamespacedName).To(Equal(expected))
			Expect(secretRequests[0].NamespacedName).To(Equal(expected))
		})
	})

	Context("node selector filtering", func() {
		It("validates selectors even when their exact labels do not match", func() {
			configs := []v1beta1.FRRConfiguration{{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{NodeSelector: metav1.LabelSelector{
					MatchLabels:      map[string]string{"node": "some-other-node"},
					MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "environment", Operator: metav1.LabelSelectorOperator("Invalid")}},
				}},
			}}
			_, err := configsForNode(configs, map[string]string{"node": testNodeName})
			Expect(err).To(HaveOccurred())
		})

		It("does not match a missing node label against an empty selector value", func() {
			configs := []v1beta1.FRRConfiguration{{
				ObjectMeta: metav1.ObjectMeta{Name: "empty-label-value", Namespace: "default"},
				Spec:       v1beta1.FRRConfigurationSpec{NodeSelector: metav1.LabelSelector{MatchLabels: map[string]string{"example": ""}}},
			}}
			matching, err := configsForNode(configs, map[string]string{})
			Expect(err).NotTo(HaveOccurred())
			Expect(matching).To(BeEmpty())
		})

		It("returns deep copies of matching configurations", func() {
			configs := []v1beta1.FRRConfiguration{{
				ObjectMeta: metav1.ObjectMeta{Name: "matching", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					NodeSelector: metav1.LabelSelector{MatchLabels: map[string]string{"node": testNodeName}},
					BGP:          v1beta1.BGPConfig{Routers: []v1beta1.Router{{ASN: 64512, Prefixes: []string{"192.0.2.0/24"}}}},
				},
			}}
			before := configs[0].DeepCopy()
			matching, err := configsForNode(configs, map[string]string{"node": testNodeName})
			Expect(err).NotTo(HaveOccurred())
			Expect(matching).To(HaveLen(1))
			matching[0].Spec.NodeSelector.MatchLabels["node"] = "mutated"
			matching[0].Spec.BGP.Routers[0].Prefixes[0] = "198.51.100.0/24"
			Expect(configs[0]).To(Equal(*before))
		})

		It("does not mutate informer-cached configurations during reconciliation", func() {
			configuration := &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: "cached-configuration", Namespace: "default"},
				Spec: v1beta1.FRRConfigurationSpec{
					NodeSelector: metav1.LabelSelector{MatchLabels: map[string]string{"test": "e2e"}},
					BGP:          v1beta1.BGPConfig{Routers: []v1beta1.Router{{ASN: 64512, Prefixes: []string{"192.0.2.0/24"}}}},
				},
			}
			Expect(k8sClient.Create(context.Background(), configuration)).To(Succeed())
			var before *v1beta1.FRRConfiguration
			Eventually(func() bool {
				list := &v1beta1.FRRConfigurationList{}
				if err := cacheClient.List(context.Background(), list, client.UnsafeDisableDeepCopy); err != nil {
					return false
				}
				for i := range list.Items {
					if list.Items[i].Name == configuration.Name {
						before = list.Items[i].DeepCopy()
						return true
					}
				}
				return false
			}).Should(BeTrue())

			reconciler := &FRRConfigurationReconciler{Client: cacheClient, Scheme: scheme.Scheme, FRRHandler: &fakeFRRConfigHandler, NodeName: testNodeName, Namespace: testNamespace, ReloadStatus: fakeReloadStatus, DefaultLogLevel: logLevel}
			_, err := reconciler.Reconcile(context.Background(), ctrl.Request{})
			Expect(err).NotTo(HaveOccurred())
			afterList := &v1beta1.FRRConfigurationList{}
			Expect(cacheClient.List(context.Background(), afterList, client.UnsafeDisableDeepCopy)).To(Succeed())
			for i := range afterList.Items {
				if afterList.Items[i].Name == configuration.Name {
					Expect(afterList.Items[i]).To(Equal(*before))
					return
				}
			}
			Fail("cached configuration was not found after reconciliation")
		})
	})

	AfterEach(func() {
		toDel := &v1beta1.FRRConfiguration{}
		err := k8sClient.DeleteAllOf(context.Background(), toDel, client.InNamespace("default"))
		if apierrors.IsNotFound(err) {
			return
		}
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() int {
			frrConfigList := &v1beta1.FRRConfigurationList{}
			err := k8sClient.List(context.Background(), frrConfigList)
			Expect(err).ToNot(HaveOccurred())
			return len(frrConfigList.Items)
		}).Should(Equal(0))
	})

	Context("when a FRRConfiguration is created", func() {

		It("should apply the configuration to FRR", func() {
			frrConfig := &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: uint32(42),
							},
						},
					},
				},
			}

			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(42),
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors:    []*frr.NeighborConfig{},
						ImportVRFs:   []string{},
					}},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))
		})

		It("should apply and modify the configuration to FRR", func() {
			frrConfig := &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: uint32(42),
							},
						},
					},
				},
			}
			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(42),
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors:    []*frr.NeighborConfig{},
						ImportVRFs:   []string{},
					}},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))

			frrConfig.Spec.BGP.Routers[0].ASN = uint32(43)
			frrConfig.Spec.BGP.Routers[0].Prefixes = []string{"192.168.1.0/32"}

			err = k8sClient.Update(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(43),
						IPV4Prefixes: []string{"192.168.1.0/32"},
						IPV6Prefixes: []string{},
						Neighbors:    []*frr.NeighborConfig{},
						ImportVRFs:   []string{},
					}},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))

		})

		It("should create and delete the configuration to FRR", func() {
			frrConfig := &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: uint32(42),
							},
						},
					},
				},
			}
			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(42),
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors:    []*frr.NeighborConfig{},
						ImportVRFs:   []string{},
					}},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))

			err = k8sClient.Delete(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers:     []*frr.RouterConfig{},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))
		})

		It("should respect the nodeSelector of configurations and react to their create/update/delete events", func() {
			configWithoutSelector := &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-selector",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: uint32(42),
							},
						},
					},
				},
			}
			err := k8sClient.Create(context.Background(), configWithoutSelector)
			Expect(err).ToNot(HaveOccurred())

			configWithMatchingSelector := &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "with-matching-selector",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: uint32(52),
								VRF: "red",
							},
						},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"test": "e2e"},
					},
				},
			}
			err = k8sClient.Create(context.Background(), configWithMatchingSelector)
			Expect(err).ToNot(HaveOccurred())

			configWithNonMatchingSelectorAtFirst := &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "with-non-matching-selector-at-first",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: uint32(62),
								VRF: "blue",
							},
						},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"some": "label"},
					},
				},
			}
			err = k8sClient.Create(context.Background(), configWithNonMatchingSelectorAtFirst)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying the matching config is handled and the non-matching is ignored")
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{
						{
							MyASN:        uint32(42),
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
						},
						{
							MyASN:        uint32(52),
							VRF:          "red",
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
						},
					},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))

			By("Updating the non-matching config to match our node")
			configWithNonMatchingSelectorAtFirst.Spec.NodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"test": "e2e"},
			}
			err = k8sClient.Update(context.Background(), configWithNonMatchingSelectorAtFirst)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying all of the configs are handled")
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{
						{
							MyASN:        uint32(42),
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
						},
						{
							MyASN:        uint32(62),
							VRF:          "blue",
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
						},
						{
							MyASN:        uint32(52),
							VRF:          "red",
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
						},
					},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))

			By("Deleting a matching config")
			err = k8sClient.Delete(context.Background(), configWithMatchingSelector)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying it does not handle the deleted config anymore")
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{
						{
							MyASN:        uint32(42),
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
						},
						{
							MyASN:        uint32(62),
							VRF:          "blue",
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
						},
					},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))
		})

		It("should respect the nodeSelector of configurations when node create/update/delete events happen", func() {
			configWithoutSelector := &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-selector",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: uint32(42),
							},
						},
					},
				},
			}
			err := k8sClient.Create(context.Background(), configWithoutSelector)
			Expect(err).ToNot(HaveOccurred())

			configWithSelector := &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "with-selector",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: uint32(52),
								VRF: "red",
							},
						},
					},
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"color": "red"},
					},
				},
			}
			err = k8sClient.Create(context.Background(), configWithSelector)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying the the non-matching config is ignored")
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{
						{
							MyASN:        uint32(42),
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
						},
					},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))

			By("Updating the node labels to match the config with the selector")
			node := &corev1.Node{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: testNodeName}, node)
			Expect(err).ToNot(HaveOccurred())

			node.Labels["color"] = "red"
			err = k8sClient.Update(context.Background(), node)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying all of the configs are handled")
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{
						{
							MyASN:        uint32(42),
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
						},
						{
							MyASN:        uint32(52),
							VRF:          "red",
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
						},
					},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))

			By("Updating the node labels to not match the config with the selector")
			node.Labels = map[string]string{}
			err = k8sClient.Update(context.Background(), node)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying the the non-matching config is ignored")
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{
						{
							MyASN:        uint32(42),
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
						},
					},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))
		})

		It("should handle the secrets as passwords to FRR", func() {
			frrConfig := &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: uint32(42),
								Neighbors: []v1beta1.Neighbor{
									{
										ASN:     65012,
										Address: "192.0.2.7",
										PasswordSecret: v1beta1.SecretReference{
											Name:      "secret1",
											Namespace: testNamespace,
										},
									},
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())

			secret := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret1",
					Namespace: testNamespace,
				},
				Type: corev1.SecretTypeBasicAuth,
				Data: map[string][]byte{
					"password": []byte("password2"),
				},
			}

			err = k8sClient.Create(context.Background(), &secret)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() frr.Config {
				return *fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(42),
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						ImportVRFs:   []string{},
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      "65012",
								Addr:     "192.0.2.7",
								Password: "password2",
								Outgoing: frr.AllowedOut{
									PrefixesV4:                     []string{},
									PrefixesV6:                     []string{},
									LocalPrefPrefixesModifiers:     []frr.LocalPrefPrefixList{},
									CommunityPrefixesModifiers:     []frr.CommunityPrefixList{},
									AsPathPrependPrefixesModifiers: []frr.AsPathPrependPrefixList{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
								AlwaysBlock:     []frr.IncomingFilter{},
								AddressFamilies: []string{"unicast"},
							},
						},
					}},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))

			// To ensure we collect secret events
			By("changing the password and updating the secret")
			secret.Data["password"] = []byte("password3")
			err = k8sClient.Update(context.Background(), &secret)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() frr.Config {
				return *fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(42),
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						ImportVRFs:   []string{},
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      "65012",
								Addr:     "192.0.2.7",
								Password: "password3",
								Outgoing: frr.AllowedOut{
									PrefixesV4:                     []string{},
									PrefixesV6:                     []string{},
									LocalPrefPrefixesModifiers:     []frr.LocalPrefPrefixList{},
									CommunityPrefixesModifiers:     []frr.CommunityPrefixList{},
									AsPathPrependPrefixesModifiers: []frr.AsPathPrependPrefixList{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
								AlwaysBlock:     []frr.IncomingFilter{},
								AddressFamilies: []string{"unicast"},
							},
						},
					}},
					BFDProfiles: []frr.BFDProfile{},
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))
		})

		It("should handle the raw FRR configuration", func() {
			frrConfig := &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: uint32(42),
							},
						},
					},
					Raw: v1beta1.RawConfig{
						Config: "foo",
					},
				},
			}
			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(42),
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						ImportVRFs:   []string{},
						Neighbors:    []*frr.NeighborConfig{},
					}},
					BFDProfiles: []frr.BFDProfile{},
					ExtraConfig: "foo\n",
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))

			secondConfig := &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test1",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{},
					},
					Raw: v1beta1.RawConfig{
						Priority: 10,
						Config:   "bar",
					},
				},
			}
			err = k8sClient.Create(context.Background(), secondConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(42),
						ImportVRFs:   []string{},
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors:    []*frr.NeighborConfig{},
					}},
					BFDProfiles: []frr.BFDProfile{},
					ExtraConfig: "foo\nbar\n",
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))

			err = k8sClient.Delete(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers:     []*frr.RouterConfig{},
					BFDProfiles: []frr.BFDProfile{},
					ExtraConfig: "bar\n",
					Loglevel:    frr.LevelFrom(logLevel),
				},
			))
		})

		It("should handle the BFD profile", func() {
			frrConfig := &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: uint32(42),
							},
						},
						BFDProfiles: []v1beta1.BFDProfile{
							{
								Name: "foo",
							},
							{
								Name:             "bar",
								ReceiveInterval:  ptr.To[uint32](47),
								TransmitInterval: ptr.To[uint32](300),
								DetectMultiplier: ptr.To[uint32](3),
								EchoInterval:     ptr.To[uint32](50),
								MinimumTTL:       ptr.To[uint32](254),
							},
						},
					},
				},
			}
			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(42),
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						ImportVRFs:   []string{},
						Neighbors:    []*frr.NeighborConfig{},
					}},
					BFDProfiles: []frr.BFDProfile{
						{
							Name:             "bar",
							ReceiveInterval:  ptr.To[uint32](47),
							TransmitInterval: ptr.To[uint32](300),
							DetectMultiplier: ptr.To[uint32](3),
							EchoInterval:     ptr.To[uint32](50),
							MinimumTTL:       ptr.To[uint32](254),
						}, {
							Name: "foo",
						},
					},
					Loglevel: frr.LevelFrom(logLevel),
				},
			))
		})

	})

	Context("when reporting the conversion status", func() {
		BeforeEach(func() {
			reloadCalled = false
		})

		It("should notify when the last conversion status changed", func() {
			By("making the conversion fail")
			frrConfig := &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						BFDProfiles: []v1beta1.BFDProfile{
							{
								Name: "foo",
							},
							{
								Name: "foo",
							},
						},
					},
				},
			}
			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool {
				return reloadCalled
			}, 5*time.Second).Should(BeTrue())
			reloadCalled = false

			By("updating to a valid config")
			frrConfig.Spec = v1beta1.FRRConfigurationSpec{
				BGP: v1beta1.BGPConfig{
					Routers: []v1beta1.Router{
						{
							ASN: uint32(42),
						},
					},
				},
			}
			err = k8sClient.Update(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool {
				return reloadCalled
			}, 5*time.Second).Should(BeTrue())
			reloadCalled = false

			By("updating with another valid config")
			frrConfig.Spec.BGP.Routers[0].ASN = uint32(44)

			err = k8sClient.Update(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Consistently(func() bool {
				return reloadCalled
			}, 5*time.Second).Should(BeFalse())
		})
	})

	Context("when a FRRConfiguration with EVPN is created", func() {
		It("should apply EVPN configuration to FRR", func() {
			frrConfig := &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test-evpn",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: 65000,
								Neighbors: []v1beta1.Neighbor{
									{
										ASN:             65001,
										Address:         "192.0.2.10",
										AddressFamilies: []v1beta1.AddressFamily{"unicast", "evpn"},
									},
								},
								EVPN: &v1beta1.EVPNConfig{
									AdvertiseVNIs: ptr.To(v1beta1.VNIAdvertisementAll),
									L2VNIs: []v1beta1.L2VNI{
										{VNI: 1000, VNIProperties: v1beta1.VNIProperties{
											RD:        "65000:1000",
											ImportRTs: []v1beta1.ImportRouteTarget{"65000:1000"},
											ExportRTs: []v1beta1.ExportRouteTarget{"65000:1000"},
										}},
									},
								},
							},
							{
								ASN: 65000,
								VRF: "red",
								EVPN: &v1beta1.EVPNConfig{
									L3VNI: &v1beta1.L3VNI{
										VNI: 3000, VNIProperties: v1beta1.VNIProperties{
											RD:        "65000:3000",
											ImportRTs: []v1beta1.ImportRouteTarget{"65000:3000"},
											ExportRTs: []v1beta1.ExportRouteTarget{"65000:3000"},
										},
										AdvertisePrefixes: []v1beta1.AdvertisePrefixType{"unicast"},
									},
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(Equal(
				&frr.Config{
					Loglevel: frr.LevelFrom(logLevel),
					Routers: []*frr.RouterConfig{
						{
							MyASN:        65000,
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							ImportVRFs:   []string{},
							Neighbors: []*frr.NeighborConfig{
								{
									IPFamily:        ipfamily.IPv4,
									Name:            "65001@192.0.2.10",
									ASN:             "65001",
									Addr:            "192.0.2.10",
									AddressFamilies: []string{"evpn", "unicast"},
									Outgoing: frr.AllowedOut{
										PrefixesV4:                     []string{},
										PrefixesV6:                     []string{},
										LocalPrefPrefixesModifiers:     []frr.LocalPrefPrefixList{},
										CommunityPrefixesModifiers:     []frr.CommunityPrefixList{},
										AsPathPrependPrefixesModifiers: []frr.AsPathPrependPrefixList{},
									},
									Incoming: frr.AllowedIn{
										PrefixesV4: []frr.IncomingFilter{},
										PrefixesV6: []frr.IncomingFilter{},
									},
									AlwaysBlock: []frr.IncomingFilter{},
								},
							},
							EVPN: &frr.EVPNConfig{
								AdvertiseVNIs: ptr.To("All"),
								L2VNIs: []frr.L2VNI{
									{VNI: 1000, VNIProperties: frr.VNIProperties{
										RD:        "65000:1000",
										ImportRTs: []string{"65000:1000"},
										ExportRTs: []string{"65000:1000"},
									}},
								},
							},
						},
						{
							MyASN:        65000,
							VRF:          "red",
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
							ImportVRFs:   []string{},
							EVPN: &frr.EVPNConfig{
								L3VNI: &frr.L3VNI{
									VNI: 3000, VNIProperties: frr.VNIProperties{
										RD:        "65000:3000",
										ImportRTs: []string{"65000:3000"},
										ExportRTs: []string{"65000:3000"},
									},
									AdvertisePrefixes: []string{"unicast"},
								},
							},
						},
					},
					BFDProfiles: []frr.BFDProfile{},
				},
			))
		})

		It("should apply and modify the EVPN configuration", func() {
			frrConfig := &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test-evpn-modify",
					Namespace: "default",
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								ASN: 65000,
								Neighbors: []v1beta1.Neighbor{
									{
										ASN:             65001,
										Address:         "192.0.2.10",
										AddressFamilies: []v1beta1.AddressFamily{"unicast", "evpn"},
									},
								},
								EVPN: &v1beta1.EVPNConfig{
									AdvertiseVNIs: ptr.To(v1beta1.VNIAdvertisementAll),
									L2VNIs: []v1beta1.L2VNI{
										{VNI: 1000},
									},
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).ShouldNot(BeNil())

			By("adding another L2VNI")
			frrConfig.Spec.BGP.Routers[0].EVPN.L2VNIs = append(
				frrConfig.Spec.BGP.Routers[0].EVPN.L2VNIs,
				v1beta1.L2VNI{VNI: 2000},
			)
			err = k8sClient.Update(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() *frr.Config {
				return fakeFRRConfigHandler.lastConfig
			}).Should(SatisfyAll(
				Not(BeNil()),
				WithTransform(func(c *frr.Config) int {
					if len(c.Routers) == 0 || c.Routers[0].EVPN == nil {
						return 0
					}
					return len(c.Routers[0].EVPN.L2VNIs)
				}, Equal(2)),
			))
		})
	})

})
