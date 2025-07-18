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
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"k8s.io/apimachinery/pkg/types"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	fakeStatus        = &fakeFRRStatus{}
	updateChan        chan event.GenericEvent
	fakeConversionRes = &fakeConversionResult{}
)

type fakeFRRStatus struct {
	lastApplied      string
	lastReloadResult string
}

func (f *fakeFRRStatus) GetStatus() frr.Status {
	return frr.Status{
		Current:          f.lastApplied,
		LastReloadResult: f.lastReloadResult,
	}
}

type fakeConversionResult struct {
	result string
}

func (f *fakeConversionResult) ConversionResult() string {
	return f.result
}

var _ = Describe("Frrk8s node status", func() {
	Context("when a FRRConfiguration is created", func() {

		It("should report the status when notified", func() {
			fakeStatus.lastApplied = "foo"
			fakeStatus.lastReloadResult = "baz"

			updateChan <- NewStateEvent()
			lastGeneration := ""

			Eventually(func() frrk8sv1beta1.FRRNodeState {
				nodeStatusList := frrk8sv1beta1.FRRNodeStateList{}
				err := k8sClient.List(context.Background(), &nodeStatusList)
				Expect(err).ToNot(HaveOccurred())
				if len(nodeStatusList.Items) != 1 {
					return frrk8sv1beta1.FRRNodeState{}
				}
				lastGeneration = nodeStatusList.Items[0].ResourceVersion
				return nodeStatusList.Items[0]
			}, 5*time.Second, time.Second).Should(
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"ObjectMeta": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"Name": Equal(testNodeName),
					}),
					"Status": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"RunningConfig":    Equal("foo"),
						"LastReloadResult": Equal("baz"),
					}),
				}))

			updateChan <- NewStateEvent()
			Consistently(func() string {
				nodeStatusList := frrk8sv1beta1.FRRNodeStateList{}
				err := k8sClient.List(context.Background(), &nodeStatusList)
				Expect(err).ToNot(HaveOccurred())
				if len(nodeStatusList.Items) != 1 {
					return ""
				}
				return nodeStatusList.Items[0].ResourceVersion
			}, 5*time.Second, time.Second).Should(
				Equal(lastGeneration))

			fakeStatus.lastReloadResult = "error"
			updateChan <- NewStateEvent()

			Eventually(func() frrk8sv1beta1.FRRNodeState {
				nodeStatusList := frrk8sv1beta1.FRRNodeStateList{}
				err := k8sClient.List(context.Background(), &nodeStatusList)
				Expect(err).ToNot(HaveOccurred())
				if len(nodeStatusList.Items) != 1 {
					return frrk8sv1beta1.FRRNodeState{}
				}
				return nodeStatusList.Items[0]
			}, time.Minute, 5*time.Second).Should(
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"ObjectMeta": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"Name": Equal(testNodeName),
					}),
					"Status": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"RunningConfig":    Equal("foo"),
						"LastReloadResult": Equal("error"),
					}),
				}))

			fakeConversionRes.result = "aaaa"
			updateChan <- NewStateEvent()

			Eventually(func() frrk8sv1beta1.FRRNodeState {
				nodeStatusList := frrk8sv1beta1.FRRNodeStateList{}
				err := k8sClient.List(context.Background(), &nodeStatusList)
				Expect(err).ToNot(HaveOccurred())
				if len(nodeStatusList.Items) != 1 {
					return frrk8sv1beta1.FRRNodeState{}
				}
				return nodeStatusList.Items[0]
			}, time.Minute, 5*time.Second).Should(
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"ObjectMeta": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"Name": Equal(testNodeName),
					}),
					"Status": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"RunningConfig":        Equal("foo"),
						"LastReloadResult":     Equal("error"),
						"LastConversionResult": Equal("aaaa"),
					}),
				}))

		})

		It("should obfuscate the passwords", func() {
			fakeStatus.lastApplied = "foo\n password supersecret\n"

			updateChan <- NewStateEvent()

			Eventually(func() frrk8sv1beta1.FRRNodeState {
				nodeStatusList := frrk8sv1beta1.FRRNodeStateList{}
				err := k8sClient.List(context.Background(), &nodeStatusList)
				Expect(err).ToNot(HaveOccurred())
				if len(nodeStatusList.Items) != 1 {
					return frrk8sv1beta1.FRRNodeState{}
				}
				return nodeStatusList.Items[0]
			}, time.Minute, time.Second).Should(
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"ObjectMeta": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"Name": Equal(testNodeName),
					}),
					"Status": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"RunningConfig": Equal("foo\n password <retracted>\n"),
					}),
				}))
		})

		It("should assign pod name annotation when creating new FRRNodeState", func() {
			fakeStatus.lastApplied = "new-config"
			fakeStatus.lastReloadResult = "success"

			nodeStatusList := frrk8sv1beta1.FRRNodeStateList{}
			err := k8sClient.List(context.Background(), &nodeStatusList)
			Expect(err).ToNot(HaveOccurred())
			for _, nodeState := range nodeStatusList.Items {
				err := k8sClient.Delete(context.Background(), &nodeState)
				Expect(err).ToNot(HaveOccurred())
			}

			Eventually(func() int {
				nodeStatusList := frrk8sv1beta1.FRRNodeStateList{}
				err := k8sClient.List(context.Background(), &nodeStatusList)
				Expect(err).ToNot(HaveOccurred())
				return len(nodeStatusList.Items)
			}, 5*time.Second, time.Second).Should(Equal(0))

			updateChan <- NewStateEvent()

			Eventually(func() frrk8sv1beta1.FRRNodeState {
				nodeStatusList := frrk8sv1beta1.FRRNodeStateList{}
				err := k8sClient.List(context.Background(), &nodeStatusList)
				Expect(err).ToNot(HaveOccurred())
				if len(nodeStatusList.Items) != 1 {
					return frrk8sv1beta1.FRRNodeState{}
				}
				return nodeStatusList.Items[0]
			}, 5*time.Second, time.Second).Should(
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"ObjectMeta": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"Name": Equal(testNodeName),
						"Annotations": gstruct.MatchKeys(gstruct.IgnoreExtras, gstruct.Keys{
							PodNameAnnotation: Equal("test-pod"),
						}),
					}),
					"Status": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"RunningConfig":    Equal("new-config"),
						"LastReloadResult": Equal("success"),
					}),
				}))
		})

		It("should update pod name annotation when it changes", func() {
			fakeStatus.lastApplied = "updated-config"
			fakeStatus.lastReloadResult = "success"

			var existingNodeState frrk8sv1beta1.FRRNodeState
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, &existingNodeState)
			Expect(err).ToNot(HaveOccurred())

			existingNodeState.Annotations[PodNameAnnotation] = "old-pod"
			err = k8sClient.Update(context.Background(), &existingNodeState)
			Expect(err).ToNot(HaveOccurred())

			updateChan <- NewStateEvent()

			Eventually(func() frrk8sv1beta1.FRRNodeState {
				var retrieved frrk8sv1beta1.FRRNodeState
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, &retrieved)
				Expect(err).ToNot(HaveOccurred())
				return retrieved
			}, 5*time.Second, time.Second).Should(
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"ObjectMeta": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"Name": Equal(testNodeName),
						"Annotations": gstruct.MatchKeys(gstruct.IgnoreExtras, gstruct.Keys{
							PodNameAnnotation: Equal("test-pod"),
						}),
					}),
					"Status": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"RunningConfig":    Equal("updated-config"),
						"LastReloadResult": Equal("success"),
					}),
				}))
		})

		It("should preserve existing annotations when updating pod name", func() {
			fakeStatus.lastApplied = "preserve-config"
			fakeStatus.lastReloadResult = "success"

			var existingNodeState frrk8sv1beta1.FRRNodeState
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, &existingNodeState)
			Expect(err).ToNot(HaveOccurred())

			existingNodeState.Annotations[PodNameAnnotation] = "old-pod"
			existingNodeState.Annotations["frrk8s.metallb.io/dual-ips"] = "true"
			existingNodeState.Annotations["frrk8s.metallb.io/node-name"] = "worker-1"
			existingNodeState.Annotations["custom-annotation"] = "custom-value"
			err = k8sClient.Update(context.Background(), &existingNodeState)
			Expect(err).ToNot(HaveOccurred())

			updateChan <- NewStateEvent()

			Eventually(func() frrk8sv1beta1.FRRNodeState {
				var retrieved frrk8sv1beta1.FRRNodeState
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, &retrieved)
				Expect(err).ToNot(HaveOccurred())
				return retrieved
			}, 5*time.Second, time.Second).Should(
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"ObjectMeta": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"Name": Equal(testNodeName),
						"Annotations": gstruct.MatchKeys(gstruct.IgnoreExtras, gstruct.Keys{
							PodNameAnnotation:             Equal("test-pod"),
							"frrk8s.metallb.io/dual-ips":  Equal("true"),
							"frrk8s.metallb.io/node-name": Equal("worker-1"),
							"custom-annotation":           Equal("custom-value"),
						}),
					}),
					"Status": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"RunningConfig":    Equal("preserve-config"),
						"LastReloadResult": Equal("success"),
					}),
				}))
		})

		It("should handle empty pod name gracefully", func() {
			fakeStatus.lastApplied = "empty-pod-config"
			fakeStatus.lastReloadResult = "success"

			var existingNodeState frrk8sv1beta1.FRRNodeState
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, &existingNodeState)
			Expect(err).ToNot(HaveOccurred())

			existingNodeState.Annotations[PodNameAnnotation] = ""
			err = k8sClient.Update(context.Background(), &existingNodeState)
			Expect(err).ToNot(HaveOccurred())

			updateChan <- NewStateEvent()

			Eventually(func() frrk8sv1beta1.FRRNodeState {
				var retrieved frrk8sv1beta1.FRRNodeState
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, &retrieved)
				Expect(err).ToNot(HaveOccurred())
				return retrieved
			}, 5*time.Second, time.Second).Should(
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"ObjectMeta": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"Name": Equal(testNodeName),
						"Annotations": gstruct.MatchKeys(gstruct.IgnoreExtras, gstruct.Keys{
							PodNameAnnotation: Equal("test-pod"),
						}),
					}),
					"Status": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"RunningConfig":    Equal("empty-pod-config"),
						"LastReloadResult": Equal("success"),
					}),
				}))
		})
	})
})

func TestClean(t *testing.T) {
	toClean := "foo\n password supersecret\n"
	cleaned := cleanPasswords(toClean)
	if cleaned != "foo\n password <retracted>\n" {
		t.Fatalf("Expected foo\n password <retracted> got %s", cleaned)
	}
}
