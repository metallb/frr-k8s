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

var _ = ginkgo.Describe("Logging", ginkgo.Serial, func() {
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
				beforeLogs := capturePodLogs(cs, frrk8sStatusCleanerPods, k8s.FRRK8SStatusCleanerContainerName)
				createDummyNodeState(updater)
				checkStatusCleanerLogs(cs, frrk8sStatusCleanerPods, k8s.FRRK8sDefaultLogLevel, beforeLogs)

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
				beforeLogs = capturePodLogs(cs, frrk8sStatusCleanerPods, k8s.FRRK8SStatusCleanerContainerName)
				createDummyNodeState(updater)
				checkStatusCleanerLogs(cs, frrk8sStatusCleanerPods, "info", beforeLogs)

				ginkgo.By("Deleting the FRRK8sConfiguration")
				err = updater.CleanFRRK8sConfiguration()
				Expect(err).NotTo(HaveOccurred())
				beforeLogs = capturePodLogs(cs, frrk8sStatusCleanerPods, k8s.FRRK8SStatusCleanerContainerName)
				createDummyNodeState(updater)
				checkStatusCleanerLogs(cs, frrk8sStatusCleanerPods, k8s.FRRK8sDefaultLogLevel, beforeLogs)

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
				beforeLogs = capturePodLogs(cs, frrk8sStatusCleanerPods, k8s.FRRK8SStatusCleanerContainerName)
				createDummyNodeState(updater)
				checkStatusCleanerLogs(cs, frrk8sStatusCleanerPods, "debug", beforeLogs)
			})
		})
	})

	ginkgo.Context("for frr-k8s", func() {
		ginkgo.When("FRRConfiguration is added with no FRRK8sConfiguration present", func() {
			ginkgo.It("logs with default log level", func() {
				config := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRConfigurationSpec{},
				}

				beforeLogsFRR := capturePodLogs(cs, frrk8sPods, k8s.FRRK8SContainerName)
				beforeLogsBGP := capturePodLogs(cs, frrk8sPods, k8s.FRRK8SStatusContainerName)
				ginkgo.By("Creating FRRConfiguration")
				err := updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())
				checkFRRK8sLogs(cs, frrk8sPods, beforeLogsFRR, k8s.FRRK8sDefaultLogLevel)
				checkBGPStateLogs(cs, frrk8sPods, beforeLogsBGP, k8s.FRRK8sDefaultLogLevel)
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

			beforeLogsFRR := capturePodLogs(cs, frrk8sPods, k8s.FRRK8SContainerName)
			beforeLogsBGP := capturePodLogs(cs, frrk8sPods, k8s.FRRK8SStatusContainerName)
			ginkgo.By("Creating FRRConfiguration")
			err = updater.Update(nil, config)
			Expect(err).NotTo(HaveOccurred())
			checkFRRK8sLogs(cs, frrk8sPods, beforeLogsFRR, wantLogLevel)
			checkBGPStateLogs(cs, frrk8sPods, beforeLogsBGP, wantLogLevel)
		},
			ginkgo.Entry("logs with default level when logLevel is unconfigured", ""),
			ginkgo.Entry("logs with debug level when logLevel is set to debug", "debug"),
			ginkgo.Entry("logs with info level when logLevel is set to info", "info"),
		)

		ginkgo.When("FRRConfiguration is added after FRRK8sConfiguration was removed", func() {
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

				beforeLogsFRR := capturePodLogs(cs, frrk8sPods, k8s.FRRK8SContainerName)
				beforeLogsBGP := capturePodLogs(cs, frrk8sPods, k8s.FRRK8SStatusContainerName)
				ginkgo.By("Creating FRRConfiguration")
				err = updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())
				checkFRRK8sLogs(cs, frrk8sPods, beforeLogsFRR, "info")
				checkBGPStateLogs(cs, frrk8sPods, beforeLogsBGP, "info")

				ginkgo.By("Removing all resources. We should be back to default log level")
				err = updater.Clean()
				Expect(err).NotTo(HaveOccurred())

				beforeLogsFRR = capturePodLogs(cs, frrk8sPods, k8s.FRRK8SContainerName)
				beforeLogsBGP = capturePodLogs(cs, frrk8sPods, k8s.FRRK8SStatusContainerName)
				err = updater.Update(nil, config)
				Expect(err).NotTo(HaveOccurred())
				checkFRRK8sLogs(cs, frrk8sPods, beforeLogsFRR, k8s.FRRK8sDefaultLogLevel)
				checkBGPStateLogs(cs, frrk8sPods, beforeLogsBGP, k8s.FRRK8sDefaultLogLevel)
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

				checkFRRConfigLogLevel(k8s.FRRK8sDefaultLogLevel, frrk8sPods)
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

			checkFRRConfigLogLevel(wantLogLevel, frrk8sPods)
		},
			ginkgo.Entry("logs with default level when logLevel is unconfigured", ""),
			ginkgo.Entry("logs with debug level when logLevel is set to debug", "debug"),
			ginkgo.Entry("logs with info level when logLevel is set to info", "info"),
		)
	})
})

// getLogDiff returns the difference between beforeLogs and afterLogs.
// It returns the portion of afterLogs that comes after the end of beforeLogs.
func getLogDiff(beforeLogs, afterLogs string) string {
	if len(beforeLogs) >= len(afterLogs) {
		return ""
	}
	// If beforeLogs is a prefix of afterLogs, return the new content.
	if len(beforeLogs) > 0 && afterLogs[:len(beforeLogs)] == beforeLogs {
		return afterLogs[len(beforeLogs):]
	}
	// Otherwise return all of afterLogs (safer fallback).
	return afterLogs
}

// capturePodLogs captures logs from all specified pods and containers.
// Returns a map with key format: "namespace/podname/containername".
//
// This is used to work around metav1.Now() timestamp granularity issues when filtering logs.
// Instead of relying on timestamps (which have 1-second granularity per
// https://github.com/kubernetes/kubernetes/issues/124200), we capture the full logs before
// an action, then capture them again after, and diff the two to find only new log entries.
func capturePodLogs(cs clientset.Interface, pods []*corev1.Pod, containerName string) map[string]string {
	logs := make(map[string]string)
	for _, pod := range pods {
		containerKey := fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, containerName)
		podLogs, err := e2ek8s.PodLogs(cs, pod, corev1.PodLogOptions{Container: containerName})
		Expect(err).NotTo(HaveOccurred())
		logs[containerKey] = podLogs
	}
	return logs
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
func checkFRRK8sLogs(cs clientset.Interface, frrk8sPods []*corev1.Pod, beforeLogs map[string]string, logLevel string) {
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
		containerKey := fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, k8s.FRRK8SContainerName)
		Eventually(func() string {
			afterLogs, err := e2ek8s.PodLogs(cs, pod, corev1.PodLogOptions{Container: k8s.FRRK8SContainerName})
			Expect(err).NotTo(HaveOccurred())

			return getLogDiff(beforeLogs[containerKey], afterLogs)
		}, pollingTimeout, pollingInterval).Should(expected, containerKey)
	}
}

// checkBGPStateLogs verifies BGPSessionState controller logs match the expected log level.
func checkBGPStateLogs(cs clientset.Interface, frrk8sPods []*corev1.Pod, beforeLogs map[string]string, logLevel string) {
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
		containerKey := fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, k8s.FRRK8SStatusContainerName)
		assertion(func() string {
			afterLogs, err := e2ek8s.PodLogs(cs, pod, corev1.PodLogOptions{Container: k8s.FRRK8SStatusContainerName})
			Expect(err).NotTo(HaveOccurred())
			return getLogDiff(beforeLogs[containerKey], afterLogs)
		}, pollingTimeout, pollingInterval).Should(expected, containerKey)
	}
}

// checkStatusCleanerLogs verifies NodeStateCleaner logs match the expected log level.
func checkStatusCleanerLogs(cs clientset.Interface, frrk8sStatusCleanerPods []*corev1.Pod, logLevel string,
	beforeLogs map[string]string) {
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
		containerKey := fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, k8s.FRRK8SStatusCleanerContainerName)
		assertion(func() string {
			afterLogs, err := e2ek8s.PodLogs(cs, pod, corev1.PodLogOptions{Container: k8s.FRRK8SStatusCleanerContainerName})
			Expect(err).NotTo(HaveOccurred())

			return getLogDiff(beforeLogs[containerKey], afterLogs)
		}, pollingTimeout, pollingInterval).Should(expected, containerKey)
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
func checkFRRConfigLogLevel(logLevel string, frrk8sPods []*corev1.Pod) {
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
