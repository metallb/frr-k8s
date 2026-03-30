// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/logging"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
	daemonPod *corev1.Pod
)

const (
	testNodeName  = "testnode"
	testNamespace = "testnamespace"
)

func TestAPIs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.TODO())
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = frrk8sv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme.Scheme,
		HealthProbeBindAddress: "",
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).ToNot(HaveOccurred())

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testNodeName,
			Labels: map[string]string{"test": "e2e"},
		},
	}
	err = k8sClient.Create(ctx, node)
	Expect(err).ToNot(HaveOccurred())
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	err = k8sClient.Create(ctx, namespace)
	Expect(err).ToNot(HaveOccurred())

	daemonPod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "frr-k8s-daemon", Namespace: testNamespace},
		Spec: corev1.PodSpec{Containers: []corev1.Container{
			{Name: "frr-k8s", Image: "frr-k8s"},
		}},
	}
	err = k8sClient.Create(ctx, daemonPod)
	Expect(err).ToNot(HaveOccurred())

	nodeState := &frrk8sv1beta1.FRRNodeState{ // for the periodic reconciliation
		ObjectMeta: metav1.ObjectMeta{
			Name: testNodeName,
		},
	}
	err = k8sClient.Create(ctx, nodeState)
	Expect(err).ToNot(HaveOccurred())

	defaultLogLevel := logging.LevelDebug
	err = logging.InitWithWriter(os.Stdout)
	Expect(err).ToNot(HaveOccurred())
	logging.GetLogger().SetLogLevel(defaultLogLevel)

	err = (&BGPSessionStateReconciler{
		Client:          k8sManager.GetClient(),
		BGPPeersFetcher: fakeBGP.GetBGPNeighbors,
		NodeName:        testNodeName,
		Namespace:       testNamespace,
		DaemonPod:       daemonPod,
		ResyncPeriod:    1 * time.Second,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
