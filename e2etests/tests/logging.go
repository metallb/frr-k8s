// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/openshift-kni/k8sreporter"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/config"
	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	"go.universe.tf/e2etest/pkg/executor"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	e2ek8s "go.universe.tf/e2etest/pkg/k8s"
	"go.universe.tf/e2etest/pkg/metallb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	dummyNodeName = "non-existing-node-for-logging-test"

	pollingTimeout  = 30 * time.Second
	pollingInterval = 1 * time.Second
)

var _ = ginkgo.Describe("Logging", func() {
	var cs clientset.Interface
	var updater *config.Updater
	var reporter *k8sreporter.KubernetesReporter
	var frrk8sPods []*corev1.Pod

	ginkgo.BeforeEach(func() {
		var err error
		updater, err = config.NewUpdater()
		Expect(err).NotTo(HaveOccurred())
		reporter = dump.NewK8sReporter(k8s.FRRK8sNamespace)

		ginkgo.By("Clearing any previous configuration")
		for _, c := range infra.FRRContainers {
			err := c.UpdateBGPConfigFile(frrconfig.Empty)
			Expect(err).NotTo(HaveOccurred())
		}
		err = updater.Clean()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()

		frrk8sPods, err = metallb.FRRK8SPods(cs, k8s.FRRK8sNamespace)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		if ginkgo.CurrentSpecReport().Failed() {
			testName := ginkgo.CurrentSpecReport().LeafNodeText
			dump.K8sInfo(testName, reporter)
			dump.BGPInfo(testName, infra.FRRContainers, cs)
		}

		err := updater.Clean()
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.Context("for status cleaner", func() {
		var frrk8sStatusCleanerPods []*corev1.Pod

		ginkgo.BeforeEach(func() {
			var err error
			frrk8sStatusCleanerPods, err = k8s.FRRK8SStatusCleanerPods(cs, k8s.FRRK8sNamespace)
			Expect(err).NotTo(HaveOccurred())
		})

		ginkgo.When("FRRK8sConfiguration changes", func() {
			ginkgo.It("logs with the correct log levels", func() {
				ginkgo.By("Creating an empty FRRK8sConfiguration")
				operatorConfig := frrk8sv1beta1.FRRK8sConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.FRRK8sConfigurationName,
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRK8sConfigurationSpec{},
				}
				err := updater.UpdateFRRK8sConfiguration(operatorConfig)
				Expect(err).NotTo(HaveOccurred())
				sleepForMetav1Granularity()
				beforeUpdateTime := metav1.Now()
				createDummyNodeState(updater)
				checkStatusCleanerLogs(cs, frrk8sStatusCleanerPods, k8s.FRRK8sDefaultLogLevel, beforeUpdateTime)

				ginkgo.By("Updating the log level to info")
				operatorConfig = frrk8sv1beta1.FRRK8sConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.FRRK8sConfigurationName,
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRK8sConfigurationSpec{
						LogLevel: "info",
					},
				}
				err = updater.UpdateFRRK8sConfiguration(operatorConfig)
				Expect(err).NotTo(HaveOccurred())
				sleepForMetav1Granularity()
				beforeUpdateTime = metav1.Now()
				createDummyNodeState(updater)
				checkStatusCleanerLogs(cs, frrk8sStatusCleanerPods, "info", beforeUpdateTime)

				ginkgo.By("Deleting the FRRK8sConfiguration")
				err = updater.CleanFRRK8sConfiguration()
				Expect(err).NotTo(HaveOccurred())
				sleepForMetav1Granularity()
				beforeUpdateTime = metav1.Now()
				createDummyNodeState(updater)
				checkStatusCleanerLogs(cs, frrk8sStatusCleanerPods, k8s.FRRK8sDefaultLogLevel, beforeUpdateTime)

				ginkgo.By("Creating an FRRK8sConfiguration with log level all")
				operatorConfig = frrk8sv1beta1.FRRK8sConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.FRRK8sConfigurationName,
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRK8sConfigurationSpec{
						LogLevel: "all",
					},
				}
				err = updater.UpdateFRRK8sConfiguration(operatorConfig)
				Expect(err).NotTo(HaveOccurred())
				sleepForMetav1Granularity()
				beforeUpdateTime = metav1.Now()
				createDummyNodeState(updater)
				checkStatusCleanerLogs(cs, frrk8sStatusCleanerPods, "debug", beforeUpdateTime)
			})
		})
	})

	ginkgo.Context("for frr-k8s", func() {
		ginkgo.When("no FRRK8sConfiguration is present", func() {
			ginkgo.It("logs with default log level", func() {
				config := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRConfigurationSpec{},
				}

				sleepForMetav1Granularity()
				beforeUpdateTime := metav1.Now()
				ginkgo.By("Creating FRRConfiguration")
				err := updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())
				checkFRRK8sLogs(cs, frrk8sPods, beforeUpdateTime, k8s.FRRK8sDefaultLogLevel)
				checkBGPStateLogs(cs, frrk8sPods, beforeUpdateTime, k8s.FRRK8sDefaultLogLevel)
			})
		})

		ginkgo.DescribeTable("when FRRConfiguration is added", func(logLevel string) {
			operatorConfig := frrk8sv1beta1.FRRK8sConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      k8s.FRRK8sConfigurationName,
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRK8sConfigurationSpec{},
			}
			wantLogLevel := k8s.FRRK8sDefaultLogLevel
			if logLevel != "" {
				wantLogLevel = logLevel
				operatorConfig.Spec.LogLevel = logLevel
			}

			config := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{},
			}

			ginkgo.By("Creating FRRK8sConfiguration")
			err := updater.UpdateFRRK8sConfiguration(operatorConfig)
			Expect(err).NotTo(HaveOccurred())

			sleepForMetav1Granularity()
			beforeUpdateTime := metav1.Now()
			ginkgo.By("Creating FRRConfiguration")
			err = updater.Update(nil, config)
			Expect(err).NotTo(HaveOccurred())
			checkFRRK8sLogs(cs, frrk8sPods, beforeUpdateTime, wantLogLevel)
			checkBGPStateLogs(cs, frrk8sPods, beforeUpdateTime, wantLogLevel)
		},
			ginkgo.Entry("logs with default level when logLevel is unconfigured", ""),
			ginkgo.Entry("logs with debug level when logLevel is set to debug", "debug"),
			ginkgo.Entry("logs with info level when logLevel is set to info", "info"),
		)

		ginkgo.When("FRRK8sConfiguration is removed", func() {
			ginkgo.It("logs with default log level", func() {
				operatorConfig := frrk8sv1beta1.FRRK8sConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.FRRK8sConfigurationName,
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRK8sConfigurationSpec{
						LogLevel: "info",
					},
				}

				config := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRConfigurationSpec{},
				}

				ginkgo.By("Creating a configuration with info log level")
				err := updater.UpdateFRRK8sConfiguration(operatorConfig)
				Expect(err).NotTo(HaveOccurred())

				sleepForMetav1Granularity()
				beforeUpdateTime := metav1.Now()
				ginkgo.By("Creating FRRConfiguration")
				err = updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())
				checkFRRK8sLogs(cs, frrk8sPods, beforeUpdateTime, "info")
				checkBGPStateLogs(cs, frrk8sPods, beforeUpdateTime, "info")

				ginkgo.By("Removing all resources. We should be back to default log level")
				err = updater.Clean()
				Expect(err).NotTo(HaveOccurred())

				sleepForMetav1Granularity()
				beforeUpdateTime = metav1.Now()
				err = updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())
				checkFRRK8sLogs(cs, frrk8sPods, beforeUpdateTime, k8s.FRRK8sDefaultLogLevel)
				checkBGPStateLogs(cs, frrk8sPods, beforeUpdateTime, k8s.FRRK8sDefaultLogLevel)
			})
		})
	})

	ginkgo.Context("for frr", func() {
		ginkgo.When("no logLevel configuration is present", func() {
			ginkgo.It("logs with default log level", func() {
				config := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRConfigurationSpec{},
				}
				err := updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())

				checkFRRConfigLogLevel(cs, k8s.FRRK8sDefaultLogLevel, frrk8sPods)
			})
		})

		ginkgo.DescribeTable("when FRRK8sConfiguration is present", func(logLevel string) {
			// In this test, we also test that the operator reconciles FRR config when only FRRK8sConfiguration
			// changes, without a change to FRRConfiguration.
			config := frrk8sv1beta1.FRRK8sConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      k8s.FRRK8sConfigurationName,
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRK8sConfigurationSpec{},
			}
			wantLogLevel := k8s.FRRK8sDefaultLogLevel
			if logLevel != "" {
				wantLogLevel = logLevel
				config.Spec.LogLevel = logLevel
			}
			err := updater.UpdateFRRK8sConfiguration(config)
			Expect(err).NotTo(HaveOccurred())

			checkFRRConfigLogLevel(cs, wantLogLevel, frrk8sPods)
		},
			ginkgo.Entry("logs with default level when logLevel is unconfigured", ""),
			ginkgo.Entry("logs with debug level when logLevel is set to debug", "debug"),
			ginkgo.Entry("logs with info level when logLevel is set to info", "info"),
		)
	})
})

// sleepForMetav1Granularity waits to avoid log timestamp pollution due to metav1.Now() granularity.
// We need to wait 1 second to avoid pollution of logs, as the granularity of metav1.Now() is 1 full second,
// see https://github.com/kubernetes/kubernetes/issues/124200.
func sleepForMetav1Granularity() {
	time.Sleep(1 * time.Second)
}

// logLevelToFRR converts the provided level to a valid FRR log level. Returns "" for unknown levels.
func logLevelToFRR(level string) string {
	// Allowed FRR log levels are: emergencies, alerts, critical,
	// errors, warnings, notifications, informational, or debugging
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

// checkFRRK8sLogs verifies FRRConfigurationReconciler logs match the expected log level.
func checkFRRK8sLogs(cs clientset.Interface, frrk8sPods []*corev1.Pod, beforeUpdateTime metav1.Time, logLevel string) {
	ginkgo.By("Checking FRR K8s Logs")
	var expected types.GomegaMatcher
	switch logLevel {
	case "all", "debug":
		expected = And(
			ContainSubstring(`"controller":"FRRConfigurationReconciler","level":"info","start reconcile"`),
			ContainSubstring(`"controller":"FRRConfigurationReconciler","k8s config"`),
		)
	case "info":
		expected = And(
			ContainSubstring(`"controller":"FRRConfigurationReconciler","level":"info","start reconcile"`),
			Not(ContainSubstring(`"controller":"FRRConfigurationReconciler","k8s config"`)),
		)
	default:
		ginkgo.Fail(fmt.Sprintf("Invalid log level provided to checkFRRK8sLogs, %q not supported", logLevel))
	}
	for _, pod := range frrk8sPods {
		Eventually(func() string {
			logs, err := e2ek8s.PodLogsSinceTime(cs, pod, k8s.FRRK8SContainerName, &beforeUpdateTime)
			Expect(err).NotTo(HaveOccurred())

			return logs
		}, pollingTimeout, pollingInterval).Should(expected,
			fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, k8s.FRRK8SContainerName))
	}
}

// checkBGPStateLogs verifies BGPSessionState controller logs match the expected log level.
func checkBGPStateLogs(cs clientset.Interface, frrk8sPods []*corev1.Pod, beforeUpdateTime metav1.Time, logLevel string) {
	ginkgo.By("Checking BGP State Logs")
	var assertion func(actual any, intervals ...any) types.AsyncAssertion
	var expected types.GomegaMatcher
	switch logLevel {
	case "all", "debug":
		assertion = Eventually
		expected = ContainSubstring(`"controller":"BGPSessionState","level":"debug","log level controller"`)
	case "info":
		assertion = Consistently
		expected = Not(ContainSubstring(`"controller":"BGPSessionState","level":"debug","log level controller":"%s"`, logLevel))
	default:
		ginkgo.Fail(fmt.Sprintf("Invalid log level provided to checkBGPStateLogs, %q not supported", logLevel))
	}
	for _, pod := range frrk8sPods {
		assertion(func() string {
			logs, err := e2ek8s.PodLogsSinceTime(cs, pod, k8s.FRRK8SStatusContainerName, &beforeUpdateTime)
			Expect(err).NotTo(HaveOccurred())
			return logs
		}, pollingTimeout, pollingInterval).Should(expected,
			fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, k8s.FRRK8SStatusContainerName))
	}
}

// checkStatusCleanerLogs verifies NodeStateCleaner logs match the expected log level.
func checkStatusCleanerLogs(cs clientset.Interface, frrk8sStatusCleanerPods []*corev1.Pod, logLevel string,
	beforeUpdateTime metav1.Time) {
	ginkgo.By("Checking Status Cleaner logs")
	var assertion func(actual any, intervals ...any) types.AsyncAssertion
	var expected types.GomegaMatcher
	switch logLevel {
	case "all", "debug":
		assertion = Eventually
		expected = ContainSubstring(`"controller":"NodeStateCleaner","level":"debug","log level controller"`)
	case "info":
		assertion = Consistently
		expected = Not(ContainSubstring(`"controller":"NodeStateCleaner","level":"debug","log level controller":"%s"`, logLevel))
	default:
		ginkgo.Fail(fmt.Sprintf("Invalid log level provided to checkStatusCleanerLogs, %q not supported", logLevel))
	}
	for _, pod := range frrk8sStatusCleanerPods {
		assertion(func() string {
			logs, err := e2ek8s.PodLogsSinceTime(cs, pod, k8s.FRRK8SStatusCleanerContainerName, &beforeUpdateTime)
			Expect(err).NotTo(HaveOccurred())

			return logs
		}, pollingTimeout, pollingInterval).Should(expected,
			fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, k8s.FRRK8SStatusCleanerContainerName))
	}
}

// createDummyNodeState creates a FRRNodeState for a non-existent node to trigger the status cleaner.
func createDummyNodeState(updater *config.Updater) {
	dummyNodeState := frrk8sv1beta1.FRRNodeState{
		ObjectMeta: metav1.ObjectMeta{
			Name: dummyNodeName,
		},
	}
	err := updater.UpdateFRRNodeState(dummyNodeState)
	Expect(err).NotTo(HaveOccurred())
}

// getExpectedLogString returns the expected FRR log configuration string for the given log level.
func getExpectedLogString(logLevel string) string {
	if logLevel == "all" || logLevel == "debug" {
		return "log stdout"
	}
	return fmt.Sprintf("log stdout %s", logLevelToFRR(logLevel))
}

// checkFRRConfigLogLevel verifies the FRR daemon configuration contains the expected log level setting.
func checkFRRConfigLogLevel(cs clientset.Interface, logLevel string, frrk8sPods []*corev1.Pod) {
	expectedLogString := getExpectedLogString(logLevel)
	ginkgo.By(fmt.Sprintf("Comparing to log level %q, expecting to find string %q", logLevel, expectedLogString))
	for _, pod := range frrk8sPods {
		exec := executor.ForPod(pod.Namespace, pod.Name, k8s.FRRContainerName)
		Eventually(func() string {
			output, err := exec.Exec("vtysh", "-c", "show run")
			Expect(err).NotTo(HaveOccurred())
			return output
		}, pollingTimeout, pollingInterval).Should(ContainSubstring(expectedLogString))
	}
}
