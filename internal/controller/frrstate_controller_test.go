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
	})
})

func TestClean(t *testing.T) {
	toClean := "foo\n password supersecret\n"
	cleaned := cleanPasswords(toClean)
	if cleaned != "foo\n password <retracted>\n" {
		t.Fatalf("Expected foo\n password <retracted> got %s", cleaned)
	}
}
