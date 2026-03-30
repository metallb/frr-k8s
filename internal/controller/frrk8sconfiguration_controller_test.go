/*
Copyright 2026.

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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	v1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/metallb/frr-k8s/internal/logging"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Frrk8s logging", Serial, func() {
	Context("when logging configuration changes and FRRConfiguration and FRRState controller reconcilers are triggered", func() {

		AfterEach(func() {
			frrConfigToDel := &v1beta1.FRRConfiguration{}
			err := k8sClient.DeleteAllOf(context.Background(), frrConfigToDel, client.InNamespace(testNamespace))
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

			frrOperToDel := &v1beta1.FRRK8sConfiguration{}
			err = k8sClient.DeleteAllOf(context.Background(), frrOperToDel, client.InNamespace(testNamespace))
			if apierrors.IsNotFound(err) {
				return
			}
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() int {
				frrConfigList := &v1beta1.FRRK8sConfigurationList{}
				err := k8sClient.List(context.Background(), frrConfigList)
				Expect(err).ToNot(HaveOccurred())
				return len(frrConfigList.Items)
			}).Should(Equal(0))
		})

		DescribeTable("should log at the correct log level", func(logLevel logging.Level) {
			// Create the FRRK8sConfiguration with the desired log level.
			frrK8sConfig := &v1beta1.FRRK8sConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      frrK8sConfigurationName,
					Namespace: testNamespace,
				},
				Spec: v1beta1.FRRK8sConfigurationSpec{
					LogLevel: string(logLevel),
				},
			}
			err := k8sClient.Create(context.Background(), frrK8sConfig)
			Expect(err).ToNot(HaveOccurred())

			// Check if the singleton's log level changed to what we expected, and only once that happens continue.
			Eventually(func() logging.Level {
				return logging.GetLogger().GetLogLevel()
			}, 5*time.Second).Should(Equal(logLevel))

			// Only look at the log buffer content starting from here.
			logsBefore := logBuffer.String()

			// Now, create an FRRConfiguration - this will trigger reconciliation.
			frrConfig := &v1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: testNamespace,
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
			err = k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())

			// Wait for the fake FRRConfigHandler's lastConfig. At this point, we are guaranteed that the reconciler ran
			// and that the log level was updated.
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

			// Now, trigger the FRRNodeState reconciler so that we get some log output from it, as well.
			fakeStatus.lastApplied = "foo"
			fakeStatus.lastReloadResult = "baz"

			updateChan <- NewStateEvent()

			Eventually(func() v1beta1.FRRNodeState {
				nodeStatusList := v1beta1.FRRNodeStateList{}
				err := k8sClient.List(context.Background(), &nodeStatusList)
				Expect(err).ToNot(HaveOccurred())
				if len(nodeStatusList.Items) != 1 {
					return v1beta1.FRRNodeState{}
				}
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

			logsAfter := logBuffer.String()
			logsSince := strings.TrimPrefix(logsAfter, logsBefore)

			// Make sure that logs contain (or do not contain) specific log lines.
			if logLevel.IsAllOrDebug() {
				Expect(logsSince).To(ContainSubstring("FRRStateReconciler"))
				Expect(logsSince).To(ContainSubstring(`"FRRConfigurationReconciler","level":"debug","log level controller":"debug"`))
			} else {
				Expect(logsSince).NotTo(ContainSubstring("FRRStateReconciler"))
				Expect(logsSince).NotTo(ContainSubstring(`"FRRConfigurationReconciler","level":"debug","log level controller":"debug"`))
			}

		},
			Entry("with debug logging", logging.LevelDebug),
			Entry("with error logging", logging.LevelError),
		)
	})
})
