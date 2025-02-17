// SPDX-License-Identifier:Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/frr-tools/metrics/vtysh"
	"github.com/metallb/frr-k8s/frr-tools/status/controller"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/metallb/frr-k8s/internal/logging"
	"github.com/metallb/frr-k8s/internal/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(frrk8sv1beta1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		logLevel     string
		nodeName     string
		namespace    string
		podName      string
		pollInterval string
	)

	flag.StringVar(&logLevel, "log-level", "info", fmt.Sprintf("log level. must be one of: [%s]", logging.Levels.String()))
	flag.StringVar(&nodeName, "node-name", "", "The node this daemon is running on.")
	flag.StringVar(&namespace, "namespace", "", "The namespace this daemon is deployed in")
	flag.StringVar(&podName, "pod-name", "", "The pod name of this daemon")
	flag.StringVar(&pollInterval, "poll-interval", "2m", "The maximum duration between FRR polls")

	flag.Parse()

	resyncPeriod, err := time.ParseDuration(pollInterval)
	if err != nil {
		fmt.Printf("failed to parse poll interval as duration: %v\n", err)
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	logger, err := logging.Init(logLevel)
	if err != nil {
		fmt.Printf("failed to initialize logging: %v\n", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: "",
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	cl, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "unable to create client")
		os.Exit(1)
	}
	daemonPod := &corev1.Pod{}
	err = cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podName}, daemonPod)
	if err != nil {
		setupLog.Error(err, "could not fetch the daemon pod")
		os.Exit(1)
	}

	if err = (&controller.BGPSessionStateReconciler{
		Client:          mgr.GetClient(),
		Logger:          logger,
		BGPPeersFetcher: func() (map[string][]*frr.Neighbor, error) { return vtysh.GetBGPNeighbors(vtysh.Run) },
		NodeName:        nodeName,
		Namespace:       namespace,
		DaemonPod:       daemonPod.DeepCopy(),
		ResyncPeriod:    resyncPeriod,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BGPSessionState")
		os.Exit(1)
	}

	setupLog.Info("starting status exporter", "version", version.String())
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
