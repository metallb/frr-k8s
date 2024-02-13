// SPDX-License-Identifier:Apache-2.0

package e2e

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/tests"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/test/e2e/framework"
	e2econfig "k8s.io/kubernetes/test/e2e/framework/config"
)

var (
	skipDockerCmd       bool
	reportPath          string
	prometheusNamespace string
	localNics           string
	externalContainers  string
	runOnHost           bool
	bgpNativeMode       bool
	frrImage            string
)

// handleFlags sets up all flags and parses the command line.
func handleFlags() {
	e2econfig.CopyFlags(e2econfig.Flags, flag.CommandLine)
	framework.RegisterCommonFlags(flag.CommandLine)
	/*
		Using framework.RegisterClusterFlags(flag.CommandLine) results in a panic:
		"flag redefined: kubeconfig".
		This happens because controller-runtime registers the kubeconfig flag as well.
		To solve this we set the framework's kubeconfig directly via the KUBECONFIG env var
		instead of letting it call the flag. Since we also use the provider flag it is handled manually.
	*/
	flag.StringVar(&framework.TestContext.Provider, "provider", "", "The name of the Kubernetes provider (gce, gke, local, skeleton (the fallback if not set), etc.)")
	framework.TestContext.KubeConfig = os.Getenv(clientcmd.RecommendedConfigPathEnvVar)

	flag.BoolVar(&skipDockerCmd, "skip-docker", false, "set this to true if the BGP daemon is running on the host instead of in a container")
	flag.StringVar(&reportPath, "report-path", "/tmp/report", "the path to be used to dump test failure information")
	flag.StringVar(&prometheusNamespace, "prometheus-namespace", "monitoring", "the namespace prometheus is running in (if running)")
	flag.StringVar(&externalContainers, "external-containers", "", "a comma separated list of external containers names to use for the test. (valid parameters are: ibgp-single-hop / ibgp-multi-hop / ebgp-single-hop / ebgp-multi-hop)")
	flag.StringVar(&frrImage, "frr-image", "quay.io/frrouting/frr:9.0.2", "the image to use for the external frr containers")

	flag.Parse()

	if _, res := os.LookupEnv("RUN_FRR_CONTAINER_ON_HOST_NETWORK"); res {
		runOnHost = true
	}
	dump.ReportPath = reportPath
}

func TestMain(m *testing.M) {
	// Register test flags, then parse flags.
	handleFlags()
	if testing.Short() {
		return
	}

	framework.AfterReadingAllFlags(&framework.TestContext)

	os.Exit(m.Run())
}

func TestE2E(t *testing.T) {
	if testing.Short() {
		return
	}

	gomega.RegisterFailHandler(framework.Fail)
	ginkgo.RunSpecs(t, "E2E Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	// Make sure the framework's kubeconfig is set.
	framework.ExpectNotEqual(framework.TestContext.KubeConfig, "", fmt.Sprintf("%s env var not set", clientcmd.RecommendedConfigPathEnvVar))

	cs, err := framework.LoadClientset()
	framework.ExpectNoError(err)

	switch {
	case externalContainers != "":
		infra.FRRContainers, err = infra.ExternalContainersSetup(externalContainers, cs)
		framework.ExpectNoError(err)
	case runOnHost:
		infra.FRRContainers, err = infra.HostContainerSetup(frrImage)
		framework.ExpectNoError(err)
	default:
		infra.FRRContainers, err = infra.KindnetContainersSetup(cs, frrImage)
		framework.ExpectNoError(err)
		vrfFRRContainers, err := infra.VRFContainersSetup(cs, frrImage)
		framework.ExpectNoError(err)
		infra.FRRContainers = append(infra.FRRContainers, vrfFRRContainers...)
	}

	tests.PrometheusNamespace = prometheusNamespace
})

var _ = ginkgo.AfterSuite(func() {
	cs, err := framework.LoadClientset()
	framework.ExpectNoError(err)

	err = infra.InfraTearDown(cs)
	framework.ExpectNoError(err)
	err = infra.InfraTearDownVRF(cs)
	framework.ExpectNoError(err)
})
