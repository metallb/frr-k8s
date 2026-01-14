// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

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

var _ = ginkgo.Describe("Logging", func() {
	var cs clientset.Interface

	defer ginkgo.GinkgoRecover()
	updater, err := config.NewUpdater()
	Expect(err).NotTo(HaveOccurred())
	reporter := dump.NewK8sReporter(k8s.FRRK8sNamespace)

	var frrk8sPods []*corev1.Pod

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

		frrk8sPods, err = metallb.FRRK8SPods(cs, k8s.FRRK8sNamespace)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.Context("for status cleaner", func() {
		var frrk8sStatusCleanerPods []*corev1.Pod

		ginkgo.BeforeEach(func() {
			frrk8sStatusCleanerPods, err = frrK8SStatusCleanerPods(cs, k8s.FRRK8sNamespace)
			Expect(err).NotTo(HaveOccurred())
		})

		ginkgo.When("FRROperatorConfiguration changes", func() {
			ginkgo.It("logs with the correct log levels", func() {
				ginkgo.By("getting the default log level")
				defaultLogLevel := getDefaultLogLevelStatusCleaner(cs)

				ginkgo.By("creating an empty FRROperatorConfiguration")
				operatorConfig := frrk8sv1beta1.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.FRROperatorConfigurationName,
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRROperatorConfigurationSpec{},
				}
				updateAndCheckStatusCleanerLogs(cs, frrk8sStatusCleanerPods, updater, &operatorConfig, defaultLogLevel)

				ginkgo.By("updating the log level to info")
				operatorConfig = frrk8sv1beta1.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.FRROperatorConfigurationName,
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRROperatorConfigurationSpec{
						LogLevel: "info",
					},
				}
				updateAndCheckStatusCleanerLogs(cs, frrk8sStatusCleanerPods, updater, &operatorConfig, defaultLogLevel)

				ginkgo.By("deleting the FRROperatorConfiguration")
				updateAndCheckStatusCleanerLogs(cs, frrk8sStatusCleanerPods, updater, nil, defaultLogLevel)

				ginkgo.By("creating an FRROperatorConfiguration with log level all")
				operatorConfig = frrk8sv1beta1.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.FRROperatorConfigurationName,
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRROperatorConfigurationSpec{
						LogLevel: "debug",
					},
				}
				updateAndCheckStatusCleanerLogs(cs, frrk8sStatusCleanerPods, updater, &operatorConfig, defaultLogLevel)
			})
		})
	})

	ginkgo.Context("for frr-k8s", func() {
		ginkgo.When("no FRROperatorConfiguration is present", func() {
			ginkgo.It("logs with default log level", func() {
				config := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRConfigurationSpec{},
				}

				// Determine what logs we expect to find.
				defaultLogLevel := getDefaultLogLevel(cs, k8s.FRRK8SContainerName)
				defaultLogLevelBGPSessionState := getDefaultLogLevel(cs, k8s.FRRK8SStatusContainerName)
				expected := expectedLogsByLogLevel(defaultLogLevel)
				expectedBGPSessionState := expectedLogsByLogLevelBGPSessionState(defaultLogLevelBGPSessionState)

				checkLogs(cs, frrk8sPods, updater, config, expected, expectedBGPSessionState)
			})
		})

		ginkgo.When("FRROperatorConfiguration with empty spec is added", func() {
			ginkgo.It("logs with default log level", func() {
				operatorConfig := frrk8sv1beta1.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.FRROperatorConfigurationName,
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRROperatorConfigurationSpec{},
				}

				config := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRConfigurationSpec{},
				}

				// Determine what logs we expect to find.
				defaultLogLevel := getDefaultLogLevel(cs, k8s.FRRK8SContainerName)
				defaultLogLevelBGPSessionState := getDefaultLogLevel(cs, k8s.FRRK8SStatusContainerName)
				expected := expectedLogsByLogLevel(defaultLogLevel)
				expectedBGPSessionState := expectedLogsByLogLevelBGPSessionState(defaultLogLevelBGPSessionState)

				err = updater.UpdateFrrOperatorConfiguration(operatorConfig)
				Expect(err).NotTo(HaveOccurred())

				checkLogs(cs, frrk8sPods, updater, config, expected, expectedBGPSessionState)
			})
		})

		ginkgo.DescribeTable("when FRRConfiguration is added", func(logLevel string) {
			operatorConfig := frrk8sv1beta1.FRROperatorConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      k8s.FRROperatorConfigurationName,
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRROperatorConfigurationSpec{
					LogLevel: logLevel,
				},
			}

			config := frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: k8s.FRRK8sNamespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{},
			}

			// Determine what logs we expect to find.
			expected := expectedLogsByLogLevel(logLevel)
			expectedBGPSessionState := expectedLogsByLogLevelBGPSessionState(logLevel)

			err = updater.UpdateFrrOperatorConfiguration(operatorConfig)
			Expect(err).NotTo(HaveOccurred())

			checkLogs(cs, frrk8sPods, updater, config, expected, expectedBGPSessionState)
		},
			ginkgo.Entry("logs with info level when logLevel is set to info", "info"),
			ginkgo.Entry("logs with error level when logLevel is set to error", "error"),
		)

		ginkgo.When("FRROperatorConfiguration is removed", func() {
			ginkgo.It("logs with default log level", func() {
				operatorConfig := frrk8sv1beta1.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.FRROperatorConfigurationName,
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRROperatorConfigurationSpec{
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

				// Determine what logs we expect to find.
				defaultLogLevel := getDefaultLogLevel(cs, k8s.FRRK8SContainerName)
				defaultLogLevelBGPSessionState := getDefaultLogLevel(cs, k8s.FRRK8SStatusContainerName)
				expectedBefore := expectedLogsByLogLevel("info")
				expectedAfter := expectedLogsByLogLevel(defaultLogLevel)
				expectedBGPSessionStateBefore := expectedLogsByLogLevelBGPSessionState("info")
				expectedBGPSessionStateAfter := expectedLogsByLogLevelBGPSessionState(defaultLogLevelBGPSessionState)

				ginkgo.By("Creating a configuration that makes it log with info log level")
				err = updater.UpdateFrrOperatorConfiguration(operatorConfig)
				Expect(err).NotTo(HaveOccurred())

				checkLogs(cs, frrk8sPods, updater, config, expectedBefore, expectedBGPSessionStateBefore)

				ginkgo.By("removing all resources. We should be back to default log level")
				err = updater.Clean()
				Expect(err).NotTo(HaveOccurred())

				checkLogs(cs, frrk8sPods, updater, config, expectedAfter, expectedBGPSessionStateAfter)
			})
		})
	})

	ginkgo.Context("for frr", func() {
		ginkgo.When("no logLevel configuration is present", func() {
			ginkgo.It("logs with default log level", func() {

				defaultLogLevel := getDefaultLogLevel(cs, k8s.FRRK8SContainerName)

				// Create an object without logging configuration.
				config := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRConfigurationSpec{},
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
					exec := executor.ForPod(pod.Namespace, pod.Name, k8s.FRRContainerName)
					Eventually(func() string {
						output, err := exec.Exec("vtysh", "-c", "show run")
						Expect(err).NotTo(HaveOccurred())
						return output
					}, 2*time.Minute, 1*time.Second).Should(ContainSubstring(expectedLogString))
				}
			})
		})

		ginkgo.When("empty logLevel configuration is present", func() {
			ginkgo.It("logs with default log level", func() {

				defaultLogLevel := getDefaultLogLevel(cs, k8s.FRRK8SContainerName)

				operatorConfig := frrk8sv1beta1.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.FRROperatorConfigurationName,
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRROperatorConfigurationSpec{},
				}
				err = updater.UpdateFrrOperatorConfiguration(operatorConfig)
				Expect(err).NotTo(HaveOccurred())

				// Create an object without logging configuration.
				config := frrk8sv1beta1.FRRConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRRConfigurationSpec{},
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
					exec := executor.ForPod(pod.Namespace, pod.Name, k8s.FRRContainerName)
					Eventually(func() string {
						output, err := exec.Exec("vtysh", "-c", "show run")
						Expect(err).NotTo(HaveOccurred())
						return output
					}, 2*time.Minute, 1*time.Second).Should(ContainSubstring(expectedLogString))
				}
			})
		})

		ginkgo.When("logLevel configuration with logLevel is present", func() {
			ginkgo.It("logs with configured log level", func() {
				// In this test, we also test that the operator reconciles FRR config when only FRROperatorConfiguration
				// changes, without a change to FRRConfiguration.
				config := frrk8sv1beta1.FRROperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.FRROperatorConfigurationName,
						Namespace: k8s.FRRK8sNamespace,
					},
					Spec: frrk8sv1beta1.FRROperatorConfigurationSpec{
						LogLevel: "error",
					},
				}
				err = updater.UpdateFrrOperatorConfiguration(config)
				Expect(err).NotTo(HaveOccurred())

				expectedLogString := fmt.Sprintf("log stdout %s", logLevelToFRR("error"))
				ginkgo.By(fmt.Sprintf("Comparing to log level error, expecting to find string %q", expectedLogString))
				pods, err := k8s.FRRK8sPods(cs)
				Expect(err).NotTo(HaveOccurred())
				for _, pod := range pods {
					exec := executor.ForPod(pod.Namespace, pod.Name, k8s.FRRContainerName)
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

// getDefaultLogLevel retrieves the default log level from the FRR DaemonSet for the specified container.
func getDefaultLogLevel(cs clientset.Interface, containerName string) string {
	// Get the default log level. Requires a single FRR DaemonSet to match the label selector.
	ds, err := cs.AppsV1().DaemonSets(k8s.FRRK8sNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: k8s.FRRK8sDaemonsetLS,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(len(ds.Items)).To(Equal(1), "could not find FRR-K8S DaemonSet")

	// Get the default log level. Falls back to `info` if `--log-level` isn't found in the args list.
	re := regexp.MustCompile(`^--log-level=(.*)$`)
	defaultLogLevel := "info"
	for _, container := range ds.Items[0].Spec.Template.Spec.Containers {
		if container.Name == containerName {
			for _, arg := range container.Args {
				m := re.FindStringSubmatch(arg)
				if len(m) == 2 {
					defaultLogLevel = m[1]
				}
			}
		}
	}
	return defaultLogLevel
}

// getDefaultLogLevelStatusCleaner retrieves the default log level from the FRR status cleaner deployment.
func getDefaultLogLevelStatusCleaner(cs clientset.Interface) string {
	// Get the default log level. Requires a single FRR DaemonSet to match the label selector.
	dep, err := cs.AppsV1().Deployments(k8s.FRRK8sNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: k8s.FRRK8sStatusCleanerApp,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(len(dep.Items)).To(Equal(1), "could not find statuscleaner deployment")

	// Get the default log level. Falls back to `info` if `--log-level` isn't found in the args list.
	re := regexp.MustCompile(`^--log-level=(.*)$`)
	defaultLogLevel := "info"
	for _, container := range dep.Items[0].Spec.Template.Spec.Containers {
		if container.Name == k8s.FRRK8SStatusCleanerContainerName {
			for _, arg := range container.Args {
				m := re.FindStringSubmatch(arg)
				if len(m) == 2 {
					defaultLogLevel = m[1]
				}
			}
		}
	}
	return defaultLogLevel
}

// expectedLogsByLogLevel takes a log level and generates a GomegaMatcher for expected log lines. Note that verification
// for the FRRStateReconciler is a bit more complex as the FRRState may change later, and depending on timing may not
// change at all (if updated to FRRConfiguration happen quickly in a row). It's also not strictly necessary to verify
// that the logging of the FRRState controller works correctly, as all the update logic is driven via the
// FRRConfigurationReconciler and therefore the FRRState reconciler's log level changes will work exactly the same.
func expectedLogsByLogLevel(logLevel string) types.GomegaMatcher {
	switch logLevel {
	case "all", "debug":
		return And(
			ContainSubstring(`"controller":"FRRConfigurationReconciler","level":"info","start reconcile"`),
			ContainSubstring(`"controller":"FRRConfigurationReconciler","end reconcile"`),
			ContainSubstring(`"controller":"FRRConfigurationReconciler","k8s config"`),
		)
	case "info":
		return And(
			ContainSubstring(`"controller":"FRRConfigurationReconciler","level":"info","start reconcile"`),
			ContainSubstring(`"controller":"FRRConfigurationReconciler","end reconcile"`),
			Not(ContainSubstring(`"controller":"FRRConfigurationReconciler","k8s config"`)),
		)
	case "warn", "error", "none":
		return And(
			Not(ContainSubstring(`"controller":"FRRConfigurationReconciler","level":"info","start reconcile"`)),
			Not(ContainSubstring(`"controller":"FRRConfigurationReconciler","end reconcile"`)),
			Not(ContainSubstring(`"controller":"FRRConfigurationReconciler","k8s config"`)),
		)
	default:
		ginkgo.Fail("Invalid logLevel hit in expectedLogsByLogLevel")
	}
	return nil
}

// expectedLogsByLogLevelBGPSessionState returns a matcher for expected BGP session state logs.
func expectedLogsByLogLevelBGPSessionState(logLevel string) types.GomegaMatcher {
	if logLevel == "debug" {
		return ContainSubstring(`"controller":"BGPSessionState","level":"debug","log level controller"`)
	}
	return Not(ContainSubstring(`"controller":"BGPSessionState","level":"debug","log level controller":"%s"`, logLevel))
}

// expectedLogsByLogLevelStatusCleaner returns a matcher for expected status cleaner logs.
func expectedLogsByLogLevelStatusCleaner(logLevel string) types.GomegaMatcher {
	if logLevel == "debug" {
		return ContainSubstring(`"controller":"NodeStateCleaner","level":"debug","log level controller"`)
	}
	return Not(ContainSubstring(`"controller":"NodeStateCleaner","level":"debug","log level controller":"%s"`, logLevel))
}

// checkLogs updates the FRR configuration and verifies that controller logs match expected patterns.
// It checks logs from both the FRRConfigurationReconciler (in the controller container) and the
// BGPSessionState controller (in the frr-status container) against the provided matchers.
func checkLogs(cs clientset.Interface, frrk8sPods []*corev1.Pod, updater *config.Updater, config frrk8sv1beta1.FRRConfiguration,
	expected, expectedBGPSessionState types.GomegaMatcher) {
	// We need to wait 1 second to avoid pollution of logs, as the granularity of metav1.Now() is 1 full second,
	// see https://github.com/kubernetes/kubernetes/issues/124200.
	time.Sleep(1 * time.Second)
	beforeUpdateTime := metav1.Now()
	err := updater.Update(nil, config)
	Expect(err).NotTo(HaveOccurred())

	if expected != nil {
		for _, pod := range frrk8sPods {
			Eventually(func() string {
				logs, err := e2ek8s.PodLogsSinceTime(cs, pod, k8s.FRRK8SContainerName, &beforeUpdateTime)
				Expect(err).NotTo(HaveOccurred())

				return logs
			}, 2*time.Minute, 1*time.Second).Should(expected,
				fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, k8s.FRRK8SContainerName))
		}
	}

	if expectedBGPSessionState != nil {
		for _, pod := range frrk8sPods {
			Eventually(func() string {
				logs, err := e2ek8s.PodLogsSinceTime(cs, pod, k8s.FRRK8SStatusContainerName, &beforeUpdateTime)
				Expect(err).NotTo(HaveOccurred())

				return logs
			}, 2*time.Minute, 1*time.Second).Should(expectedBGPSessionState,
				fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, k8s.FRRK8SStatusContainerName))
		}
	}
}

// updateAndCheckStatusCleanerLogs updates or deletes the FRROperatorConfiguration and verifies that the
// NodeStateCleaner controller logs match expected patterns. The function:
// 1. Sleeps 1 second (due to metav1.Now() granularity, see https://github.com/kubernetes/kubernetes/issues/124200)
// 2. Takes the current timestamp (of the current second)
// 3. Provokes a change by updating (if frrOperatorConfig is non-nil) or deleting (if nil) the FRROperatorConfiguration
// 4. Verifies that log messages in the frr-k8s-status-cleaner container are in the expected state based on the configured or default log level
func updateAndCheckStatusCleanerLogs(cs clientset.Interface, frrk8sStatusCleanerPods []*corev1.Pod, updater *config.Updater,
	frrOperatorConfig *frrk8sv1beta1.FRROperatorConfiguration, defaultLogLevel string) {
	// We need to wait 1 second to avoid pollution of logs, as the granularity of metav1.Now() is 1 full second,
	// see https://github.com/kubernetes/kubernetes/issues/124200.
	time.Sleep(1 * time.Second)

	// Now that logs are clean, take the current timestamp.
	beforeUpdateTime := metav1.Now()

	// Next, provoke a change to generate log messages: delete (if nil) or create the provided FRROperatorConfiguration.
	// Also, determine what logs we're expected to see.
	expectedStatusCleanerState := expectedLogsByLogLevelStatusCleaner(defaultLogLevel)
	if frrOperatorConfig == nil {
		err := updater.CleanFRROperatorConfiguration()
		Expect(err).NotTo(HaveOccurred())
	} else {
		err := updater.UpdateFrrOperatorConfiguration(*frrOperatorConfig)
		Expect(err).NotTo(HaveOccurred())

		if frrOperatorConfig.Spec.LogLevel != "" {
			expectedStatusCleanerState = expectedLogsByLogLevelStatusCleaner(frrOperatorConfig.Spec.LogLevel)
		}
	}

	// Now, connect to the status cleaner and make sure that the log messages are in the expected state.
	for _, pod := range frrk8sStatusCleanerPods {
		Eventually(func() string {
			logs, err := e2ek8s.PodLogsSinceTime(cs, pod, k8s.FRRK8SStatusCleanerContainerName, &beforeUpdateTime)
			Expect(err).NotTo(HaveOccurred())

			return logs
		}, 2*time.Minute, 1*time.Second).Should(expectedStatusCleanerState,
			fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, k8s.FRRK8SStatusCleanerContainerName))
	}
}

// frrK8SStatusCleanerPods returns the set of pods related to FRR-K8s StatusCleaner / the webhook-server.
func frrK8SStatusCleanerPods(cs clientset.Interface, namespace string) ([]*corev1.Pod, error) {
	pods, err := cs.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: k8s.FRRK8sStatusCleanerApp,
	})
	if err != nil {
		return nil, errors.Join(err, errors.New("Failed to fetch frr-k8s pods"))
	}
	if len(pods.Items) == 0 {
		return nil, errors.New("No frr-k8s pods found")
	}
	res := make([]*corev1.Pod, 0)
	for _, item := range pods.Items {
		i := item
		res = append(res, &i)
	}
	return res, nil
}
