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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/metallb/frr-k8s/internal/ipfamily"

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
					}},
					BFDProfiles: []frr.BFDProfile{},
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
					}},
					BFDProfiles: []frr.BFDProfile{},
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
					}},
					BFDProfiles: []frr.BFDProfile{},
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
					}},
					BFDProfiles: []frr.BFDProfile{},
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
						},
						{
							MyASN:        uint32(52),
							VRF:          "red",
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
						},
					},
					BFDProfiles: []frr.BFDProfile{},
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
						},
						{
							MyASN:        uint32(62),
							VRF:          "blue",
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
						},
						{
							MyASN:        uint32(52),
							VRF:          "red",
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
						},
					},
					BFDProfiles: []frr.BFDProfile{},
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
						},
						{
							MyASN:        uint32(62),
							VRF:          "blue",
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
						},
					},
					BFDProfiles: []frr.BFDProfile{},
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
						},
					},
					BFDProfiles: []frr.BFDProfile{},
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
						},
						{
							MyASN:        uint32(52),
							VRF:          "red",
							IPV4Prefixes: []string{},
							IPV6Prefixes: []string{},
							Neighbors:    []*frr.NeighborConfig{},
						},
					},
					BFDProfiles: []frr.BFDProfile{},
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
						},
					},
					BFDProfiles: []frr.BFDProfile{},
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
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      65012,
								Addr:     "192.0.2.7",
								Password: "password2",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{},
									PrefixesV6: []frr.OutgoingFilter{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
								AlwaysBlock: []frr.IncomingFilter{},
							},
						},
					}},
					BFDProfiles: []frr.BFDProfile{},
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
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      65012,
								Addr:     "192.0.2.7",
								Password: "password3",
								Outgoing: frr.AllowedOut{
									PrefixesV4: []frr.OutgoingFilter{},
									PrefixesV6: []frr.OutgoingFilter{},
								},
								Incoming: frr.AllowedIn{
									PrefixesV4: []frr.IncomingFilter{},
									PrefixesV6: []frr.IncomingFilter{},
								},
								AlwaysBlock: []frr.IncomingFilter{},
							},
						},
					}},
					BFDProfiles: []frr.BFDProfile{},
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
						Neighbors:    []*frr.NeighborConfig{},
					}},
					BFDProfiles: []frr.BFDProfile{},
					ExtraConfig: "foo\n",
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
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors:    []*frr.NeighborConfig{},
					}},
					BFDProfiles: []frr.BFDProfile{},
					ExtraConfig: "foo\nbar\n",
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

})
