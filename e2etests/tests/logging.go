// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	frrk8sv1beta2 "github.com/metallb/frr-k8s/api/v1beta2"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	"go.universe.tf/e2etest/pkg/executor"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	e2ek8s "go.universe.tf/e2etest/pkg/k8s"
	"go.universe.tf/e2etest/pkg/metallb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	frrK8SContainerName = "controller"
	frrContainerName    = "frr"
)

var _ = ginkgo.Describe("Verifyng dynamic logging levels for Operator", func() {
	var cs clientset.Interface

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

		err := updater.Clean()
		Expect(err).NotTo(HaveOccurred())
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

	ginkgo.Context("Syntax checks", func() {
		ginkgo.When("more than 1 FRROperatorConfiguration is present", func() {
			ginkgo.It("should fail", func() {
				operatorConfig1 := frrk8sv1beta2.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test1",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRROperatorConfigurationSpec{
						LogLevel: "debug",
					},
				}

				operatorConfig2 := frrk8sv1beta2.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test2",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRROperatorConfigurationSpec{
						LogLevel: "debug",
					},
				}

				// We need to wait 1 second for PodLogsSinceTime not to include previous output (as granularity is 1s).
				time.Sleep(1 * time.Second)
				beforeUpdateTime := metav1.Now()

				err = updater.UpdateFrrOperatorConfiguration(operatorConfig1)
				Expect(err).NotTo(HaveOccurred())
				err = updater.UpdateFrrOperatorConfiguration(operatorConfig2)
				Expect(err).NotTo(HaveOccurred())

				frrk8sPods, err := metallb.FRRK8SPods(cs, k8s.FRRK8sNamespace)
				Expect(err).NotTo(HaveOccurred())

				for _, pod := range frrk8sPods {
					Eventually(func() string {
						logs, err := e2ek8s.PodLogsSinceTime(cs, pod, frrK8SContainerName, &beforeUpdateTime)
						Expect(err).NotTo(HaveOccurred())

						return logs
					}, 2*time.Minute, 1*time.Second).Should(
						ContainSubstring(`more than a single configuration object found in namespace`),
					)
				}

			})
		})
	})

	ginkgo.Context("Configures correct operator logging levels", func() {
		ginkgo.When("no FRROperatorConfiguration is present", func() {
			ginkgo.It("logs with debug log level", func() {
				config := frrk8sv1beta2.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRRConfigurationSpec{
						Raw: frrk8sv1beta2.RawConfig{
							Config: dummyRawConfig,
						},
					},
				}

				// We need to wait 1 second for PodLogsSinceTime not to include previous output (as granularity is 1s).
				time.Sleep(1 * time.Second)
				beforeUpdateTime := metav1.Now()
				err = updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())

				frrk8sPods, err := metallb.FRRK8SPods(cs, k8s.FRRK8sNamespace)
				Expect(err).NotTo(HaveOccurred())

				for _, pod := range frrk8sPods {
					Eventually(func() string {
						logs, err := e2ek8s.PodLogsSinceTime(cs, pod, frrK8SContainerName, &beforeUpdateTime)
						Expect(err).NotTo(HaveOccurred())

						return logs
					}, 2*time.Minute, 1*time.Second).Should(
						And(
							ContainSubstring(`"controller":"FRRConfigurationReconciler","level":"info","start reconcile"`),
							ContainSubstring(`"controller":"FRRConfigurationReconciler","end reconcile"`),
							ContainSubstring(`"controller":"FRRConfigurationReconciler","k8s config"`),
						),
					)
				}
			})
		})

		ginkgo.When("FRROperatorConfiguration with info is added", func() {
			ginkgo.It("logs with info log level", func() {
				operatorConfig := frrk8sv1beta2.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRROperatorConfigurationSpec{
						LogLevel: "info",
					},
				}

				config := frrk8sv1beta2.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRRConfigurationSpec{
						Raw: frrk8sv1beta2.RawConfig{
							Config: dummyRawConfig,
						},
					},
				}

				err = updater.UpdateFrrOperatorConfiguration(operatorConfig)
				Expect(err).NotTo(HaveOccurred())

				// We need to wait 1 second for PodLogsSinceTime not to include previous output (as granularity is 1s).
				time.Sleep(1 * time.Second)
				beforeUpdateTime := metav1.Now()
				err = updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())

				frrk8sPods, err := metallb.FRRK8SPods(cs, k8s.FRRK8sNamespace)
				Expect(err).NotTo(HaveOccurred())

				for _, pod := range frrk8sPods {
					Eventually(func() string {
						logs, err := e2ek8s.PodLogsSinceTime(cs, pod, frrK8SContainerName, &beforeUpdateTime)
						Expect(err).NotTo(HaveOccurred())

						return logs
					}, 2*time.Minute, 1*time.Second).Should(
						And(
							ContainSubstring(`"controller":"FRRConfigurationReconciler","level":"info","start reconcile"`),
							ContainSubstring(`"controller":"FRRConfigurationReconciler","end reconcile"`),
							Not(ContainSubstring(`"controller":"FRRConfigurationReconciler","k8s config"`)),
						),
					)
				}
			})
		})

		ginkgo.When("FRROperatorConfiguration with error is added", func() {
			ginkgo.It("doesn't log", func() {
				operatorConfig := frrk8sv1beta2.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRROperatorConfigurationSpec{
						LogLevel: "error",
					},
				}

				config := frrk8sv1beta2.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRRConfigurationSpec{
						Raw: frrk8sv1beta2.RawConfig{
							Config: dummyRawConfig,
						},
					},
				}

				err = updater.UpdateFrrOperatorConfiguration(operatorConfig)
				Expect(err).NotTo(HaveOccurred())

				// We need to wait 1 second for PodLogsSinceTime not to include previous output (as granularity is 1s).
				time.Sleep(1 * time.Second)
				beforeUpdateTime := metav1.Now()

				err = updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())

				frrk8sPods, err := metallb.FRRK8SPods(cs, k8s.FRRK8sNamespace)
				Expect(err).NotTo(HaveOccurred())

				for _, pod := range frrk8sPods {
					Eventually(func() string {
						logs, err := e2ek8s.PodLogsSinceTime(cs, pod, frrK8SContainerName, &beforeUpdateTime)
						Expect(err).NotTo(HaveOccurred())

						return logs
					}, 2*time.Minute, 1*time.Second).Should(
						And(
							// We're transitioning from info to error, so this first `start reconcile` message should
							// still show up, but not the `end reconcile`.
							Not(ContainSubstring(`"controller":"FRRConfigurationReconciler","level":"info","start reconcile"`)),
							Not(ContainSubstring(`"controller":"FRRConfigurationReconciler","end reconcile"`)),
							Not(ContainSubstring(`"controller":"FRRConfigurationReconciler","k8s config"`)),
						),
					)
				}
			})
		})

		ginkgo.When("FRROperatorConfiguration is removed", func() {
			ginkgo.It("logs with debug log level", func() {
				operatorConfig := frrk8sv1beta2.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRROperatorConfigurationSpec{
						LogLevel: "info",
					},
				}

				config := frrk8sv1beta2.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRRConfigurationSpec{
						Raw: frrk8sv1beta2.RawConfig{
							Config: dummyRawConfig,
						},
					},
				}

				ginkgo.By("Creating a configuration that makes it log with info log level")
				err = updater.UpdateFrrOperatorConfiguration(operatorConfig)
				Expect(err).NotTo(HaveOccurred())

				// We need to wait 1 second for PodLogsSinceTime not to include previous output (as granularity is 1s).
				time.Sleep(1 * time.Second)
				beforeUpdateTime := metav1.Now()
				err = updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())

				frrk8sPods, err := metallb.FRRK8SPods(cs, k8s.FRRK8sNamespace)
				Expect(err).NotTo(HaveOccurred())

				for _, pod := range frrk8sPods {
					Eventually(func() string {
						logs, err := e2ek8s.PodLogsSinceTime(cs, pod, frrK8SContainerName, &beforeUpdateTime)
						Expect(err).NotTo(HaveOccurred())

						return logs
					}, 2*time.Minute, 1*time.Second).Should(
						And(
							ContainSubstring(`"controller":"FRRConfigurationReconciler","level":"info","start reconcile"`),
							ContainSubstring(`"controller":"FRRConfigurationReconciler","end reconcile"`),
							Not(ContainSubstring(`"controller":"FRRConfigurationReconciler","k8s config"`)),
						),
					)
				}

				ginkgo.By("removing all resources. We should be back to debug log level")
				err = updater.Clean()
				Expect(err).NotTo(HaveOccurred())

				// We need to make sure that the reconciliation happened.
				for _, pod := range frrk8sPods {
					Eventually(func() string {
						logs, err := e2ek8s.PodLogsSinceTime(cs, pod, frrK8SContainerName, &beforeUpdateTime)
						Expect(err).NotTo(HaveOccurred())

						return logs
					}, 2*time.Minute, 1*time.Second).Should(
						ContainSubstring(`"controller":"FRROperatorConfigurationReconciler","level":"info","log level controller":"debug"`),
					)
				}
				// Now, recreate the resource and check logs again.
				// We need to wait 1 second for PodLogsSinceTime not to include previous output (as granularity is 1s).
				time.Sleep(1 * time.Second)
				beforeUpdateTime = metav1.Now()
				err = updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())

				frrk8sPods, err = metallb.FRRK8SPods(cs, k8s.FRRK8sNamespace)
				Expect(err).NotTo(HaveOccurred())

				for _, pod := range frrk8sPods {
					Eventually(func() string {
						logs, err := e2ek8s.PodLogsSinceTime(cs, pod, frrK8SContainerName, &beforeUpdateTime)
						Expect(err).NotTo(HaveOccurred())

						return logs
					}, 2*time.Minute, 1*time.Second).Should(
						And(
							ContainSubstring(`"controller":"FRRConfigurationReconciler","level":"info","start reconcile"`),
							ContainSubstring(`"controller":"FRRConfigurationReconciler","end reconcile"`),
							ContainSubstring(`"controller":"FRRConfigurationReconciler","k8s config"`),
						),
					)
				}
			})
		})
	})
})

var _ = ginkgo.Describe("Verifyng dynamic logging levels for FRR configuration", func() {
	var cs clientset.Interface

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

		err := updater.Clean()
		Expect(err).NotTo(HaveOccurred())
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

	ginkgo.Context("Configures correct FRR logging levels", func() {
		ginkgo.When("no logLevel configuration is present", func() {
			ginkgo.It("logs with default log level", func() {

				// Get the default log level. Requires a single FRR DaemonSet to match the label selector.
				ds, err := cs.AppsV1().DaemonSets(k8s.FRRK8sNamespace).List(context.Background(), metav1.ListOptions{
					LabelSelector: k8s.FRRK8sDaemonsetLS,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(len(ds.Items)).To(Equal(1))

				// Get the default log level. Falls back to `info` if `--log-level` isn't found in the args list.
				re := regexp.MustCompile(`^--log-level=(.*)$`)
				defaultLogLevel := "info"
				for _, container := range ds.Items[0].Spec.Template.Spec.Containers {
					if container.Name == frrK8SContainerName {
						for _, arg := range container.Args {
							m := re.FindStringSubmatch(arg)
							if len(m) == 2 {
								defaultLogLevel = m[1]
							}
						}
					}
				}

				// Create an object without logging configuration.
				config := frrk8sv1beta2.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRRConfigurationSpec{
						Raw: frrk8sv1beta2.RawConfig{
							Config: dummyRawConfig,
						},
					},
				}
				err = updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())

				// Expect to find default log level configuration in FRR config.
				expectedLogString := "log stdout"
				if defaultLogLevel != "debug" {
					expectedLogString = fmt.Sprintf("log stdout %s", logLevelToFRR(defaultLogLevel))
				}
				ginkgo.By(fmt.Sprintf("Comparing to default log level %q, expecting to find string %q",
					defaultLogLevel, expectedLogString))
				pods, err := k8s.FRRK8sPods(cs)
				Expect(err).NotTo(HaveOccurred())
				for _, pod := range pods {
					exec := executor.ForPod(pod.Namespace, pod.Name, frrContainerName)
					Eventually(func() string {
						output, err := exec.Exec("vtysh", "-c", "show run")
						Expect(err).NotTo(HaveOccurred())
						return output
					}, 2*time.Minute, 1*time.Second).Should(ContainSubstring(expectedLogString))
				}
			})
		})

		ginkgo.When("logLevel configuration is present", func() {
			ginkgo.It("logs with most restrictive configured log level", func() {
				config := frrk8sv1beta2.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test1",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRRConfigurationSpec{
						LogLevel: "debug",
						Raw: frrk8sv1beta2.RawConfig{
							Config: dummyRawConfig,
						},
					},
				}
				err = updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())

				config2 := frrk8sv1beta2.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test2",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta2.FRRConfigurationSpec{
						LogLevel: "error",
						Raw: frrk8sv1beta2.RawConfig{
							Config: dummyRawConfig,
						},
					},
				}
				err = updater.Update(nil, config2)
				Expect(err).NotTo(HaveOccurred())

				expectedLogString := fmt.Sprintf("log stdout %s", logLevelToFRR("error"))
				ginkgo.By(fmt.Sprintf("Comparing to log level error, expecting to find string %q", expectedLogString))
				pods, err := k8s.FRRK8sPods(cs)
				Expect(err).NotTo(HaveOccurred())
				for _, pod := range pods {
					exec := executor.ForPod(pod.Namespace, pod.Name, frrContainerName)
					Eventually(func() string {
						output, err := exec.Exec("vtysh", "-c", "show run")
						Expect(err).NotTo(HaveOccurred())
						return output
					}, 2*time.Minute, 1*time.Second).Should(ContainSubstring(expectedLogString))
				}
			})
		})
	})
})

// logLevelToFRR converts the provided level to a valid log level for frr. Defaults to "" if parsing fails.
func logLevelToFRR(level string) string {
	// Allowed frr log levels are: emergencies, alerts, critical,
	// 		errors, warnings, notifications, informational, or debugging
	switch level {
	case "all", "debug":
		return "debugging"
	case "info":
		return "informational"
	case "warn":
		return "warnings"
	case "error":
		return "errors"
	case "none":
		return "emergencies"
	}

	return ""
}

const dummyRawConfig = `router bgp 64512
        neighbor 172.18.0.5 remote-as 4200000000
        neighbor 172.18.0.5 timers 0 0
        neighbor 172.18.0.5 port 179`
