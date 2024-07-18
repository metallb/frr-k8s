// SPDX-License-Identifier:Apache-2.0

package e2e

import (
	"flag"
	"os"
	"testing"

	"github.com/metallb/frrk8stests/pkg/dump"
	"github.com/metallb/frrk8stests/pkg/infra"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	"github.com/metallb/frrk8stests/tests"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	"go.universe.tf/e2etest/pkg/executor"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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
	flag.BoolVar(&skipDockerCmd, "skip-docker", false, "set this to true if the BGP daemon is running on the host instead of in a container")
	flag.StringVar(&reportPath, "report-path", "/tmp/report", "the path to be used to dump test failure information")
	flag.StringVar(&prometheusNamespace, "prometheus-namespace", "monitoring", "the namespace prometheus is running in (if running)")
	flag.StringVar(&externalContainers, "external-containers", "", "a comma separated list of external containers names to use for the test. (valid parameters are: ibgp-single-hop / ibgp-multi-hop / ebgp-single-hop / ebgp-multi-hop)")
	flag.StringVar(&executor.Kubectl, "kubectl", "kubectl", "the path for the kubectl binary")
	flag.StringVar(&frrImage, "frr-image", "quay.io/frrouting/frr:9.1.0", "the image to use for the external frr containers")

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

	os.Exit(m.Run())
}

func TestE2E(t *testing.T) {
	if testing.Short() {
		return
	}

	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	log.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	cs := k8sclient.New()
	var err error
	switch {
	case externalContainers != "":
		infra.FRRContainers, err = infra.ExternalContainersSetup(externalContainers, cs)
		Expect(err).NotTo(HaveOccurred())
	case runOnHost:
		infra.FRRContainers, err = infra.HostContainerSetup(frrImage)
		Expect(err).NotTo(HaveOccurred())
	default:
		infra.FRRContainers, err = infra.KindnetContainersSetup(cs, frrImage)
		Expect(err).NotTo(HaveOccurred())
		vrfFRRContainers, err := infra.VRFContainersSetup(cs, frrImage)
		Expect(err).NotTo(HaveOccurred())
		infra.FRRContainers = append(infra.FRRContainers, vrfFRRContainers...)
	}

	tests.PrometheusNamespace = prometheusNamespace

	h, err := k8s.FRRK8isDaemonSetReady(cs)
	Expect(err).NotTo(HaveOccurred())
	Expect(h).Should(BeTrue(), "frr-k8s daemonset should be ready before test")
})

var _ = ginkgo.AfterSuite(func() {
	cs := k8sclient.New()

	err := infra.InfraTearDown(cs)
	Expect(err).NotTo(HaveOccurred())
	err = infra.InfraTearDownVRF(cs)
	Expect(err).NotTo(HaveOccurred())
})
